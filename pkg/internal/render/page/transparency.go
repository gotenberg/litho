package page

import (
	"fmt"
	"image"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// maxBlendScanDepth bounds how deeply [renderer.normalOnly] follows forms
// nested in forms. Anti-DoS guard, not spec-mandated: the scan only decides
// whether an isolated group may be painted without its own raster, so a
// form nested deeper than this is taken to blend, which paints the group
// with a raster, the exact way; real files nest a handful of forms.
const maxBlendScanDepth = 16

// frame is one transparency group being painted (ISO 32000-1, 11.4): the
// page group at the root, then the group XObjects and the knockout groups of
// text objects being painted inside it.
type frame struct {
	group *blend.Group
	// direct is the group's colour plane when an element with no blending to
	// do may be painted straight onto it ([blend.Group.Direct]), or nil.
	direct *surface.Canvas
}

// view is the raster a painting draws into, Normal source over: a group's
// colour plane, or the layer of one element. Its pixel (0, 0) is device
// pixel (x, y).
type view struct {
	canvas *surface.Canvas
	x, y   int
}

// matrix returns m, a matrix into device space, followed by the shift into
// the view.
func (v view) matrix(m geom.Matrix) geom.Matrix {
	return m.Mul(geom.Matrix{1, 0, 0, 1, -float64(v.x), -float64(v.y)})
}

// coverage returns cov, a coverage of device pixels, shifted into the view.
// It shares cov's values.
func (v view) coverage(cov *raster.Coverage) *raster.Coverage {
	out := *cov
	out.X -= v.x
	out.Y -= v.y
	return &out
}

// effects are the compositing inputs of the graphics state to one painting
// (ISO 32000-1, 11.6.4): the blend mode, the constant alpha of the
// operation, the soft mask and the alpha source flag.
type effects struct {
	mode    blend.Mode
	opacity surface.Opacity
	mask    *graphics.SoftMask
	ais     bool
}

// effectsOf returns the effects of st on a painting at constant alpha alpha,
// CA for a stroke and ca for everything else (11.6.4.4). The blend mode
// follows the edition of ISO 32000 the document's version names, which part
// on ColorDodge and ColorBurn ([blend.Mode.In]). A mode the machine does not
// know is Normal, as 11.3.5 makes an unrecognised name.
func (r *renderer) effectsOf(st *graphics.State, alpha float64) effects {
	mode := blend.Normal
	if st.BlendMode != "Normal" {
		if m, ok := blend.ParseMode(st.BlendMode); ok {
			mode = m
		}
	}
	return effects{mode: mode.In(r.edition), opacity: surface.NewOpacity(alpha), mask: st.SoftMask, ais: st.AlphaIsShape}
}

// blends reports whether a painting needs its group's compositing: a blend
// mode other than Normal, or a soft mask. Anything else composites as Normal
// source over at its constant alpha, which painting onto a direct plane does.
func (e effects) blends() bool {
	return e.mode != blend.Normal || e.mask != nil
}

// params returns the compositing parameters of e with mask, the soft mask
// e.mask stands for.
func (e effects) params(mask *blend.Mask) blend.Params {
	return blend.Params{Mode: e.mode, Opacity: e.opacity, SoftMask: mask, AlphaIsShape: e.ais}
}

// top returns the group being painted.
func (r *renderer) top() *frame {
	return &r.groups[len(r.groups)-1]
}

// directView returns the view of the top group's direct plane, false when
// the group has none.
func (r *renderer) directView() (view, bool) {
	top := r.top()
	if top.direct == nil {
		return view{}, false
	}
	b := top.group.Bounds()
	return view{canvas: top.direct, x: b.Min.X, y: b.Min.Y}, true
}

// paint draws one element (ISO 32000-1, 11.4.4) into the group being
// painted, the one sink every painting reaches. draw paints the element into
// a view at the opacity given; bounds holds the device pixels it may touch.
//
// An element with no blending to do, in a group with a direct plane, is
// drawn onto it at its constant alpha, as fast as an opaque page. Any other
// is drawn opaque into a layer over its bounds, which is then composited into
// the group with the blend mode, constant alpha and soft mask of fx.
func (r *renderer) paint(bounds image.Rectangle, fx effects, draw func(v view, op surface.Opacity) error) error {
	if fx.opacity.Transparent() {
		return nil
	}
	if v, ok := r.directView(); ok && !fx.blends() {
		return draw(v, fx.opacity)
	}
	top := r.top()
	area := bounds.Intersect(top.group.Bounds()).Intersect(r.clipRect())
	if area.Empty() {
		return nil
	}
	mask, err := r.softMask(fx.mask)
	if err != nil {
		return err
	}
	release, err := r.reserve(int64(area.Dx()) * int64(area.Dy()))
	if err != nil {
		return err
	}
	defer release()
	layer, err := surface.NewCanvas(area.Dx(), area.Dy(), r.budget)
	if err != nil {
		return fmt.Errorf("%w: element layer: %w", ErrTooLarge, err)
	}
	if err := draw(view{canvas: layer, x: area.Min.X, y: area.Min.Y}, surface.Opaque); err != nil {
		return err
	}
	if err := top.group.PaintCanvas(r.ctx, layer, area.Min, fx.params(mask)); err != nil {
		return fmt.Errorf("compositing: %w", err)
	}
	return nil
}

// paintSolid paints coverage cov in the sRGB colour c, as [renderer.paint]
// does, without a layer: a solid colour's coverage is its shape and alpha.
func (r *renderer) paintSolid(cov *raster.Coverage, c [3]float32, fx effects) error {
	if cov == nil || cov.Empty() || fx.opacity.Transparent() {
		return nil
	}
	if v, ok := r.directView(); ok && !fx.blends() {
		raster.PaintSolid(v.canvas, v.coverage(cov), c[0], c[1], c[2], fx.opacity.Alpha())
		return nil
	}
	mask, err := r.softMask(fx.mask)
	if err != nil {
		return err
	}
	rect := image.Rect(cov.X, cov.Y, cov.X+cov.Width, cov.Y+cov.Height)
	if err := r.top().group.PaintCoverage(r.ctx, rect, cov.A, c, fx.params(mask)); err != nil {
		return fmt.Errorf("compositing: %w", err)
	}
	return nil
}

// clipRect returns the device pixels the current clip's rectangle touches.
func (r *renderer) clipRect() image.Rectangle {
	x0, x1, y0, y1 := r.clip().span(r.width, r.height)
	return image.Rect(x0, y0, x1, y1)
}

// clipIn returns the current clip as a mask of v's size, nil when it clips
// nothing.
func (r *renderer) clipIn(v view) (*surface.Mask, error) {
	full, err := r.clip().mask1(r.width, r.height, r.budget)
	if err != nil || full == nil {
		return full, err
	}
	if v.x == 0 && v.y == 0 && v.canvas.Width == r.width && v.canvas.Height == r.height {
		return full, nil
	}
	m, err := surface.NewMask(v.canvas.Width, v.canvas.Height, r.budget)
	if err != nil {
		return nil, fmt.Errorf("%w: clip mask: %w", ErrTooLarge, err)
	}
	area := image.Rect(v.x, v.y, v.x+v.canvas.Width, v.y+v.canvas.Height).Intersect(image.Rect(0, 0, r.width, r.height))
	for y := area.Min.Y; y < area.Max.Y; y++ {
		dst := (y-v.y)*m.Width + area.Min.X - v.x
		copy(m.A[dst:dst+area.Dx()], full.A[y*r.width+area.Min.X:y*r.width+area.Max.X])
	}
	return m, nil
}

// pixelRect returns the device pixels b touches, clamped to the raster.
func (r *renderer) pixelRect(b geom.Rect) image.Rectangle {
	clampTo := func(v float64, hi int) int {
		switch {
		case math.IsNaN(v) || v < 0:
			return 0
		case v > float64(hi):
			return hi
		}
		return int(v)
	}
	return image.Rect(clampTo(math.Floor(b.Min.X), r.width), clampTo(math.Floor(b.Min.Y), r.height),
		clampTo(math.Ceil(b.Max.X), r.width), clampTo(math.Ceil(b.Max.Y), r.height))
}

// unitSquareRect returns the device pixels the unit square covers under m,
// the place of an image (ISO 32000-1, 8.9.4).
func (r *renderer) unitSquareRect(m geom.Matrix) image.Rectangle {
	var p geom.Path
	p.MoveTo(m.Apply(geom.Point{}))
	p.LineTo(m.Apply(geom.Point{X: 1}))
	p.LineTo(m.Apply(geom.Point{X: 1, Y: 1}))
	p.LineTo(m.Apply(geom.Point{Y: 1}))
	b, ok := p.Bounds()
	if !ok {
		return image.Rectangle{}
	}
	return r.pixelRect(b)
}

// offscreen counts the pixels of the rasters a render holds besides its
// page: groups, layers, soft masks and pattern tiles.
type offscreen struct{ live int64 }

// reserve counts n pixels of offscreen raster against the render pixel
// budget, refusing them past it, and returns their release.
func (r *renderer) reserve(n int64) (func(), error) {
	if n > r.budget-r.offscreen.live {
		return nil, fmt.Errorf("%w: offscreen rasters of %d pixels exceed the render pixel budget", ErrTooLarge, r.offscreen.live+n)
	}
	r.offscreen.live += n
	return func() { r.offscreen.live -= n }, nil
}

// groupPlanes is how many planes of its size a group holds: the colour and
// the shape, and for a non-isolated group also its alpha and its initial
// backdrop ([blend.NewGroup]).
func groupPlanes(attrs blend.Attributes) int64 {
	if attrs.Isolated {
		return 2
	}
	return 4
}

// newGroup starts a group over bounds, a backdrop and attributes, and
// returns it with the release of its pixels. It returns nil, without error,
// when its planes and the layer of one element as large as itself would
// pass the render pixel budget: the caller then paints without the group and
// counts [TransparencyOverBudget].
func (r *renderer) newGroup(bounds image.Rectangle, backdrop blend.Backdrop, attrs blend.Attributes) (*blend.Group, func(), error) {
	px := int64(bounds.Dx()) * int64(bounds.Dy())
	need := px * groupPlanes(attrs)
	if px > r.budget || need+px > r.budget-r.offscreen.live {
		return nil, nil, nil
	}
	release, err := r.reserve(need)
	if err != nil {
		return nil, nil, err
	}
	g, err := blend.NewGroup(bounds, backdrop, attrs, r.budget)
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("%w: transparency group: %w", ErrTooLarge, err)
	}
	return g, release, nil
}

// groupAttrs reads the group attributes dictionary of a form XObject or a
// page (ISO 32000-1, 11.6.6, Table 147): it reports false when /Group is
// absent or is not a transparency group (/S /Transparency, the only subtype
// 8.10.3 defines), and otherwise returns /I and /K and the /CS object.
func (r *renderer) groupAttrs(dict ast.PdfDict) (blend.Attributes, ast.PdfObject, bool) {
	g, err := document.ResolveDict(r.ctx, r.doc, dict["Group"])
	if err != nil || g == nil {
		return blend.Attributes{}, nil, false
	}
	if s, err := document.ResolveName(r.ctx, r.doc, g["S"]); err != nil || s != "Transparency" {
		return blend.Attributes{}, nil, false
	}
	isolated, _ := document.ResolveBool(r.ctx, r.doc, g["I"])
	knockout, _ := document.ResolveBool(r.ctx, r.doc, g["K"])
	return blend.Attributes{Isolated: bool(isolated), Knockout: bool(knockout)}, g["CS"], true
}

// elide reports whether a transparency group XObject paints exactly as its
// content painted into the parent, so it needs no raster of its own. A
// non-knockout group composited with Normal, opaque and with no soft mask
// does when it is non-isolated, as ISO 32000-1, 11.4.5 makes its elements
// composite with the parent's stack anyway, or when its content blends with
// Normal only: Normal source over is associative, so the group of such
// elements over transparency, composited over the parent, is the elements
// composited over the parent one by one.
func (r *renderer) elide(attrs blend.Attributes, fx effects, ref ast.PdfIndirectRef, resources ast.PdfDict) bool {
	if fx.blends() || fx.opacity != surface.Opaque || attrs.Knockout {
		return false
	}
	return !attrs.Isolated || r.normalOnly(ref, resources)
}

// normalOnly reports whether the content of form ref, against resources,
// can blend only with Normal: no graphics state its resources name sets
// another blend mode, and no form they name, nor a Type 3 glyph, does or is
// a knockout group. The answer is kept per form. A blend mode set by a
// resource of the stream that paints the form, which a form with no
// /Resources reads, is caught there: such a form is scanned against the
// resources it uses.
func (r *renderer) normalOnly(ref ast.PdfIndirectRef, resources ast.PdfDict) bool {
	if v, ok := r.normalForms[ref.ObjectNum]; ok && ref.ObjectNum != 0 {
		return v
	}
	v := r.scanNormal(resources, 0, map[int]bool{ref.ObjectNum: true})
	if ref.ObjectNum != 0 {
		r.normalForms[ref.ObjectNum] = v
	}
	return v
}

func (r *renderer) scanNormal(resources ast.PdfDict, depth int, seen map[int]bool) bool {
	switch {
	case depth > maxBlendScanDepth:
		return false
	case resources == nil:
		return true
	}
	if states, err := document.ResolveDict(r.ctx, r.doc, resources["ExtGState"]); err == nil {
		for _, v := range states {
			gs, err := document.ResolveDict(r.ctx, r.doc, v)
			if err != nil || gs == nil {
				continue
			}
			if !r.normalBlend(gs["BM"]) {
				return false
			}
		}
	}
	for _, kind := range []ast.PdfName{"XObject", "Font"} {
		entries, err := document.ResolveDict(r.ctx, r.doc, resources[kind])
		if err != nil {
			continue
		}
		for _, v := range entries {
			if ref, isRef := v.(ast.PdfIndirectRef); isRef {
				if seen[ref.ObjectNum] {
					continue
				}
				seen[ref.ObjectNum] = true
			}
			var dict ast.PdfDict
			if kind == "XObject" {
				stream, err := document.ResolveStream(r.ctx, r.doc, v)
				if err != nil || stream.Dict == nil {
					continue
				}
				dict = stream.Dict
				if attrs, _, isGroup := r.groupAttrs(dict); isGroup && attrs.Knockout {
					return false
				}
			} else if d, err := document.ResolveDict(r.ctx, r.doc, v); err == nil {
				dict = d
			}
			sub, err := document.ResolveDict(r.ctx, r.doc, dict["Resources"])
			if err == nil && sub != nil && !r.scanNormal(sub, depth+1, seen) {
				return false
			}
		}
	}
	return true
}

// normalBlend reports whether a /BM entry blends with Normal: a name, or the
// first name of an array the renderer knows (ISO 32000-1, 11.3.5, Table 58),
// read as the machine reads it.
func (r *renderer) normalBlend(obj ast.PdfObject) bool {
	if obj == nil {
		return true
	}
	resolved, err := document.ResolveObject(r.ctx, r.doc, obj)
	if err != nil {
		return true
	}
	names := ast.PdfArray{resolved}
	if arr, isArr := resolved.(ast.PdfArray); isArr {
		names = arr
	}
	for _, e := range names {
		name, err := document.ResolveName(r.ctx, r.doc, e)
		if err != nil {
			continue
		}
		if mode, ok := blend.ParseMode(string(name)); ok {
			return mode == blend.Normal
		}
	}
	return true
}
