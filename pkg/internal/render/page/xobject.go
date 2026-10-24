package page

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
	"github.com/gotenberg/litho/pkg/internal/render/colorconv"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/imgpaint"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// inlineImageName labels an inline image, which has no resource name.
const inlineImageName = graphics.InlineImageName

// do runs Do (ISO 32000-1, 8.8): it paints the named image or form XObject.
// A PostScript XObject paints nothing (8.8.2).
func (r *renderer) do(s *scope, op content.Operation) error {
	if len(op.Operands) != 1 || r.m.Current().SkipExtraction {
		return nil
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return nil
	}
	obj := r.resource(s, "XObject", name)
	if obj == nil {
		r.skip("XObject", fmt.Errorf("no /XObject resource %s", name))
		return nil
	}
	stream, err := document.ResolveStream(r.ctx, r.doc, obj)
	if err != nil || stream.Dict == nil {
		r.skip("XObject", fmt.Errorf("XObject %s is not a stream: %w", name, err))
		return nil
	}
	if !r.visible(stream.Dict["OC"]) {
		return nil
	}
	subtype, err := document.ResolveName(r.ctx, r.doc, stream.Dict["Subtype"])
	if err != nil {
		r.skip("XObject", fmt.Errorf("XObject %s has no /Subtype: %w", name, err))
		return nil
	}
	switch subtype {
	case "Image":
		if r.omits(Images) {
			return nil
		}
		return r.image(s, name, stream)
	case "Form":
		ref, _ := obj.(ast.PdfIndirectRef)
		return r.form(s, ref, stream)
	}
	return nil
}

// form paints a form XObject named by Do.
func (r *renderer) form(s *scope, ref ast.PdfIndirectRef, stream ast.PdfStream) error {
	return r.formXObject(s, ref, stream, true)
}

// formGeometry reads a form's /Matrix, the identity when it is absent or
// does not read as six numbers, and its /BBox (ISO 32000-1, 8.10.2, Table
// 95).
func (r *renderer) formGeometry(dict ast.PdfDict) (geom.Matrix, [4]float64, error) {
	matrix := geom.Identity
	if v, err := document.ResolveArray(r.ctx, r.doc, dict["Matrix"]); err == nil && len(v) == 6 {
		for i, e := range v {
			f, ok := document.Number(e)
			if !ok {
				matrix = geom.Identity
				break
			}
			matrix[i] = f
		}
	}
	var box [4]float64
	bbox, err := document.ResolveArray(r.ctx, r.doc, dict["BBox"])
	if err != nil || len(bbox) != 4 {
		// Table 95 requires /BBox; poppler skips a form without a usable one.
		return matrix, box, fmt.Errorf("no usable /BBox: %w", err)
	}
	for i, e := range bbox {
		f, ok := document.Number(e)
		if !ok {
			return matrix, box, errors.New("a /BBox entry is no number")
		}
		box[i] = max(-maxBBoxCoordinate, min(f, maxBBoxCoordinate))
	}
	return matrix, box, nil
}

// maxBBoxCoordinate bounds each /BBox coordinate a form is clipped to.
// Anti-overflow guard, not spec-mandated: ISO 32000-1, Annex C, Table C.1
// expects readers to hold reals near ±3.4×10^38, and a /BBox of ±9×10^307
// mapped to device space past 72 dpi overflows to infinity, leaving a clip
// that holds nothing, so the form drew at 72 dpi and vanished at 300. A
// box clamped to a billion units clips nothing a page shows: the largest
// page, 14,400 units by a /UserUnit of 75,000 (Annex C.2), is about a
// billion units wide.
//
// # Corpus evidence
//
//   - pdfjs issue20872.pdf: a form of /BBox ±8.99×10^307 drew the whole page
//     at 72 dpi and nothing at 240 or 300.
const maxBBoxCoordinate = 1e9

// boxPath returns the rectangle box, [x0 y0 x1 y1], through m.
func boxPath(m geom.Matrix, box [4]float64) geom.Path {
	var p geom.Path
	p.MoveTo(m.Apply(geom.Point{X: box[0], Y: box[1]}))
	p.LineTo(m.Apply(geom.Point{X: box[2], Y: box[1]}))
	p.LineTo(m.Apply(geom.Point{X: box[2], Y: box[3]}))
	p.LineTo(m.Apply(geom.Point{X: box[0], Y: box[3]}))
	p.Close()
	return p
}

// formXObject paints a form XObject (ISO 32000-1, 8.10.1): its content runs
// in a saved graphics state with /Matrix concatenated to the CTM and the
// clip narrowed to /BBox, against its own /Resources, or those of the stream
// that paints it when it has none (7.8.3 keeps that reading for older
// files). A name its own /Resources lacks resolves in the page's
// ([renderer.withPageNames]).
//
// When honourGroup is set, a transparency group XObject (11.6.6) paints
// into a group of its own ([renderer.group]): its content starts from the
// Normal blend mode, alphas of 1 and no soft mask, and those in force at the
// Do composite the group as a whole, ca for its alpha. A soft mask's group
// is painted with honourGroup unset, into the group its caller made.
func (r *renderer) formXObject(s *scope, ref ast.PdfIndirectRef, stream ast.PdfStream, honourGroup bool) error {
	if lim := r.doc.Limits().WithDefaults().MaxFormDepth; s.depth+1 > lim {
		r.skip("form XObject", fmt.Errorf("nested deeper than %d forms", lim))
		return nil
	}
	if ref.ObjectNum != 0 && slices.Contains(r.chain, ref.ObjectNum) {
		r.skip("form XObject", fmt.Errorf("form %d paints itself", ref.ObjectNum))
		return nil
	}
	dict := stream.Dict
	matrix, box, err := r.formGeometry(dict)
	if err != nil {
		r.skip("form XObject", err)
		return nil
	}
	attrs, cs, isGroup := r.groupAttrs(dict)
	isGroup = isGroup && honourGroup
	resources, err := document.ResolveDict(r.ctx, r.doc, dict["Resources"])
	if err != nil || resources == nil {
		resources = s.resources
	} else {
		resources = r.withPageNames(ref, resources)
	}
	ops, err := r.formContent(ref, stream)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("form XObject", err)
		return nil
	}

	atDo := r.effectsOf(r.m.Current(), r.m.Current().FillAlpha)
	savedStack, savedMarked := len(r.m.Stack), len(r.m.MarkedContent)
	if err := r.m.ExecuteOne(r.ctx, content.Operation{Operator: "q"}); err != nil {
		return fmt.Errorf("saving state for a form: %w", err)
	}
	r.syncClips()
	defer func() {
		r.m.Stack = r.m.Stack[:savedStack]
		if len(r.m.MarkedContent) > savedMarked {
			r.m.MarkedContent = r.m.MarkedContent[:savedMarked]
		}
		r.syncClips()
	}()
	st := r.m.Current()
	st.CTM = [6]float64(matrix.Mul(geom.Matrix(st.CTM)))
	if isGroup {
		// 11.6.6: before the group's content runs, the blend mode is
		// initialised to Normal, both alpha constants to 1 and the soft mask
		// to None.
		st.BlendMode, st.SoftMask = "Normal", nil
		st.StrokeAlpha, st.FillAlpha = 1, 1
	}
	formToDevice := r.toDevice()

	bp := boxPath(formToDevice, box)
	c, err := r.clip().withPath(bp, raster.NonZero, true, r.width, r.height, r.budget)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("form XObject", err)
		return nil
	}
	if isGroup {
		// A group's content reaches the page through the clip at whole
		// pixels, elided or not, as poppler composites every group.
		c = c.wholePixels()
	}
	if err := r.setClip(c); err != nil {
		return err
	}
	if !r.anyWithin(r.clipRect()) {
		// The form paints within its box alone.
		return nil
	}

	r.chain = append(r.chain, ref.ObjectNum)
	defer func() { r.chain = r.chain[:len(r.chain)-1] }()
	if !isGroup || r.elide(attrs, atDo, ref, resources) {
		return r.run(ops, resources, formToDevice, s.depth+1)
	}
	attrs.Space = r.formSpace(attrs, cs, ref, resources)
	return r.group(ops, resources, formToDevice, s.depth+1, bp, attrs, atDo)
}

// group paints ops, a transparency group's content (ISO 32000-1, 11.4),
// into a group of its own over the device pixels of bp, the group's
// bounding box, under the clip, then composites the group into the one
// being painted with fx (11.6.6). A non-isolated group starts from the
// parent's stack (11.4.5), a knockout group composites each element with
// that start (11.4.6). The group blends in the space attrs.Space holds
// ([renderer.formSpace]) and reaches the parent converted to its space. A
// group past the render pixel budget paints its content into the parent
// instead, each element through the group's blend mode, alpha and soft
// mask, counted in [TransparencyOverBudget].
func (r *renderer) group(ops []content.Operation, resources ast.PdfDict, base geom.Matrix, depth int, bp geom.Path, attrs blend.Attributes, fx effects) error {
	parent := r.top().group
	b, _ := bp.Bounds()
	bounds := r.pixelRect(b).Intersect(r.clipRect()).Intersect(parent.Bounds())
	if bounds.Empty() {
		return nil
	}
	g, release, err := r.newGroup(bounds, parent.Backdrop(), attrs)
	if err != nil {
		return err
	}
	if g == nil {
		r.report.count(TransparencyOverBudget)
		// The content paints into the parent without the group's
		// compositing (11.4), each element carrying the group's blend mode,
		// constant alpha and soft mask in place of the initial values 11.6.6
		// gives it. That is exact for elements that do not overlap, and
		// never shows what the group's mask or alpha hides.
		st := r.m.Current()
		st.BlendMode, st.SoftMask = fx.mode.String(), fx.mask
		st.FillAlpha = float64(fx.opacity.Alpha())
		st.StrokeAlpha = st.FillAlpha
		return r.run(ops, resources, base, depth)
	}
	defer release()
	r.groups = append(r.groups, frame{group: g, direct: g.Direct()})
	err = r.run(ops, resources, base, depth)
	r.groups = r.groups[:len(r.groups)-1]
	if err != nil || fx.opacity.Transparent() {
		return err
	}
	mask, err := r.softMask(fx.mask)
	if err != nil {
		return err
	}
	if err := g.Into(r.ctx, parent, fx.params(mask)); err != nil {
		return fmt.Errorf("compositing a transparency group: %w", err)
	}
	return nil
}

// image paints an image XObject or an inline image (ISO 32000-1, 8.9): the
// unit square through the CTM, in the fill alpha (11.6.4.4), made
// transparent by its /SMask, or by its /Mask when it has none (11.6.5.3),
// and by the soft mask of the graphics state, the two multiplied (11.6.5.3,
// 11.6.4.3). An image mask (8.9.6.2) paints the fill colour through its
// samples.
func (r *renderer) image(s *scope, name ast.PdfName, stream ast.PdfStream) error {
	c := r.clip()
	if c.empty() {
		return nil
	}
	st := r.m.Current()
	dict := stream.Dict
	interpolate, _ := document.ResolveBool(r.ctx, r.doc, dict["Interpolate"])
	toDevice := r.toDevice()
	bounds := r.unitSquareRect(toDevice).Intersect(r.clipRect())
	if bounds.Empty() || !r.anyWithin(bounds) {
		return nil
	}
	fx := r.effectsOf(st, st.FillAlpha)
	params := func(v view, op surface.Opacity) (imgpaint.Params, error) {
		clipMask, clipRect, err := r.clipIn(v, bounds)
		return imgpaint.Params{Matrix: v.matrix(toDevice), Interpolate: bool(interpolate), Opacity: op, Clip: clipMask, ClipRect: clipRect}, err
	}

	if isMask, _ := document.ResolveBool(r.ctx, r.doc, dict["ImageMask"]); isMask {
		cols, rows := maskFootprint(toDevice)
		img, err := pdfimage.DecodeMaskAt(r.ctx, r.doc, name, stream, cols, rows)
		if err != nil {
			return r.imageFailed(err)
		}
		stencil := imgpaint.StencilMask(img, bool(interpolate))
		if st.Fill.Space.Family == graphics.FamilyPattern {
			clipMask, err := c.mask1(r.width, r.height, r.budget)
			if err != nil {
				return err
			}
			p := imgpaint.Params{Matrix: toDevice, Interpolate: bool(interpolate), Clip: clipMask}
			return r.imageFailed(r.patternStencil(s, stencil, p, st))
		}
		conv, paints := r.colorFor(s, st.Fill.Space)
		if !paints {
			return nil
		}
		cr, cg, cb := conv.RGB(st.Fill.Components)
		var native *blend.Components
		if r.top().group.Space() != nil {
			sol := r.solidOf(s, st.Fill, [3]float32{cr, cg, cb})
			fx.keep, fx.spots = r.overprintPaint(s, st, st.Fill, false, &sol, true)
			native = &sol.in
		}
		return r.imageFailed(r.paintAs(bounds, fx, native, func(v view, op surface.Opacity) error {
			p, err := params(v, op)
			if err != nil {
				return err
			}
			return imgpaint.DrawStencil(r.ctx, v.canvas, stencil, cr, cg, cb, p)
		}))
	}

	// An image in a Separation or DeviceN space over the colorant None only
	// produces no visible output (ISO 32000-1, 8.6.6.4 and 8.6.6.5), whatever
	// its samples and masks. The decoder reads it as transparent pixels, and
	// imgpaint takes transparency from the masks alone, so it is skipped here.
	if cs, ok := dict["ColorSpace"]; ok {
		if _, err := colorconv.ForObject(r.ctx, r.doc, cs, s.resources, r.colorOptions()); errors.Is(err, colorconv.ErrPaintsNothing) {
			return nil
		}
	}
	img, err := pdfimage.DecodePainted(r.ctx, r.doc, name, stream, s.resources, r.page.Dict)
	if err != nil {
		return r.imageFailed(err)
	}
	b := img.Bounds()
	var masks imgpaint.Masks
	if own, ok := img.(*image.NRGBA); ok {
		// The decoder returns an image's own opacity as its alpha: the soft
		// mask a JPXDecode image carries in its data (Table 89
		// /SMaskInData), which it applies only where no /SMask is named. It
		// is a soft mask, so it overrides /Mask as /SMask does (11.6.5.3).
		masks.SMask = imgpaint.OwnAlpha(own, bool(interpolate))
	} else {
		masks, err = r.imageMasks(name, stream, b.Dx(), b.Dy(), toDevice)
		if err != nil {
			// A mask that cannot be read leaves no telling what it hides, and
			// painting the image opaque would show it all (ISO 32000-1, 8.9.6
			// and 11.6.5.3): the image is skipped instead.
			return r.imageFailed(err)
		}
	}
	if img, err = r.transferImage(img); err != nil {
		return err
	}
	return r.imageFailed(r.paint(bounds, fx, func(v view, op surface.Opacity) error {
		p, err := params(v, op)
		if err != nil {
			return err
		}
		return imgpaint.Draw(r.ctx, v.canvas, img, masks, p)
	}))
}

// imageFailed returns err when it ends the render, and otherwise skips the
// image and returns nil.
func (r *renderer) imageFailed(err error) error {
	if err == nil {
		return nil
	}
	if fatal(err) {
		return err
	}
	r.skip("image", err)
	return nil
}

// imageMasks reads the masks of an image (ISO 32000-1, 8.9.5, Table 89): a
// /SMask stream, else a /Mask stream (a stencil, 8.9.6.3) or array (a colour
// key, 8.9.6.4). toDevice maps the unit square the image and its masks fill
// onto the device, which sizes a mask past the image budget (see
// [pdfimage.DecodeMaskAt]). A mask that is present but does not decode is an
// error: the caller skips the image rather than paint what the mask hides.
// An /SMask that resolves to no stream, a /None or a reference to a missing
// object (7.3.10), masks nothing.
func (r *renderer) imageMasks(name ast.PdfName, stream ast.PdfStream, width, height int, toDevice geom.Matrix) (imgpaint.Masks, error) {
	dict := stream.Dict
	cols, rows := maskFootprint(toDevice)
	if obj, ok := dict["SMask"]; ok {
		if sm, err := document.ResolveStream(r.ctx, r.doc, obj); err == nil && sm.Dict != nil {
			sm.Dict = pdfimage.SoftMaskDict(r.ctx, r.doc, sm.Dict, width, height)
			img, err := pdfimage.DecodeMaskAt(r.ctx, r.doc, name+" soft mask", sm, cols, rows)
			if err != nil {
				return imgpaint.Masks{}, fmt.Errorf("soft mask: %w", err)
			}
			interp, _ := document.ResolveBool(r.ctx, r.doc, sm.Dict["Interpolate"])
			return imgpaint.Masks{SMask: imgpaint.SoftMask(img, bool(interp))}, nil
		}
	}
	obj, ok := dict["Mask"]
	if !ok {
		return imgpaint.Masks{}, nil
	}
	resolved, err := document.ResolveObject(r.ctx, r.doc, obj)
	if err != nil {
		return imgpaint.Masks{}, fmt.Errorf("mask: %w", err)
	}
	switch v := resolved.(type) {
	case ast.PdfStream:
		img, err := pdfimage.DecodeMaskAt(r.ctx, r.doc, name+" mask", v, cols, rows)
		if err != nil {
			return imgpaint.Masks{}, fmt.Errorf("stencil mask: %w", err)
		}
		interp, _ := document.ResolveBool(r.ctx, r.doc, v.Dict["Interpolate"])
		return imgpaint.Masks{Mask: imgpaint.StencilMask(img, bool(interp))}, nil
	case ast.PdfArray:
		key, err := r.colorKey(stream, v, width, height)
		if err != nil {
			return imgpaint.Masks{}, err
		}
		return imgpaint.Masks{Mask: key}, nil
	}
	return imgpaint.Masks{}, nil
}

// maskOversample is how many mask samples a mask past the image budget keeps
// per device pixel along each axis. The reduced mask is box-averaged, and
// twice the device resolution leaves the painter's sampling of it an edge
// within half a pixel of where the full mask puts it.
const maskOversample = 2

// maskFootprint returns the grid, in samples, a mask filling the unit square
// m maps onto the device is read at: maskOversample samples a device pixel
// along each side of the square.
func maskFootprint(m geom.Matrix) (cols, rows int) {
	side := func(x, y float64) int {
		v := math.Ceil(math.Hypot(x, y) * maskOversample)
		if !(v >= 1) {
			return 1
		}
		return int(min(v, math.MaxInt32))
	}
	return side(m[0], m[1]), side(m[2], m[3])
}

// colorKey reads the colour-key mask ranges of an image (ISO 32000-1,
// 8.9.6.4) against its samples as its filters leave them.
func (r *renderer) colorKey(stream ast.PdfStream, ranges ast.PdfArray, width, height int) (*imgpaint.Alpha, error) {
	dict := stream.Dict
	bpc, err := document.ResolveInt(r.ctx, r.doc, dict["BitsPerComponent"])
	if err != nil {
		return nil, fmt.Errorf("colour key: /BitsPerComponent: %w", err)
	}
	space, err := graphics.DescribeColorSpace(r.ctx, r.doc, dict["ColorSpace"])
	if err != nil {
		return nil, fmt.Errorf("colour key: %w", err)
	}
	keys := make([]int, len(ranges))
	for i, v := range ranges {
		n, err := document.ResolveInt(r.ctx, r.doc, v)
		if err != nil {
			return nil, fmt.Errorf("colour key: range %d: %w", i, err)
		}
		keys[i] = int(n)
	}
	maxPixels := r.doc.Limits().WithDefaults().MaxImagePixels
	if width <= 0 || height <= 0 || int64(width) > maxPixels/int64(height) || bpc < 1 || bpc > 16 || space.Components < 1 {
		return nil, fmt.Errorf("colour key over a %dx%d image of %d bits", width, height, bpc)
	}
	rowBytes := (int64(width)*int64(space.Components)*int64(bpc) + 7) / 8
	rc, err := r.doc.NewStreamReaderChecked(r.ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("colour key: opening samples: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(rc, rowBytes*int64(height)))
	if closeErr := rc.Close(); closeErr != nil {
		r.doc.Logger.Debug("render: closing colour-key samples", slog.Any("err", closeErr))
	}
	if err != nil {
		return nil, fmt.Errorf("colour key: reading samples: %w", err)
	}
	return imgpaint.ColorKey(data, width, height, space.Components, int(bpc), keys, maxPixels)
}

// visible reports whether optional content oc, an /OC entry, is shown (ISO
// 32000-1, 8.11.3.3): an optional content group the document's default
// configuration turns off is hidden. A membership dictionary is taken as
// shown for now.
func (r *renderer) visible(oc ast.PdfObject) bool {
	if oc == nil {
		return true
	}
	ref, isRef := oc.(ast.PdfIndirectRef)
	if !isRef {
		return true
	}
	dict, err := document.ResolveDict(r.ctx, r.doc, ref)
	if err != nil || dict == nil {
		return true
	}
	if t, _ := document.ResolveName(r.ctx, r.doc, dict["Type"]); t != "OCG" {
		return true
	}
	return ocgVisible(ref, r.doc.OCConfig)
}

// ocgVisible reads the visibility of the group ref under the document's
// default configuration (ISO 32000-1, 8.11.4.3): /OFF, then /ON, then
// /BaseState.
func ocgVisible(ref ast.PdfIndirectRef, cfg *document.OCConfig) bool {
	switch {
	case cfg == nil:
		return true
	case cfg.Off[ref.ObjectNum]:
		return false
	case cfg.On[ref.ObjectNum]:
		return true
	}
	return cfg.BaseState != "OFF"
}

// activeOCs maps the optional content groups the /Properties of resources
// names to their visibility, which the machine reads when BDC marks content
// /OC (8.11.3.2).
func activeOCs(ctx context.Context, doc *document.Document, resources ast.PdfDict) map[string]bool {
	out := make(map[string]bool)
	props, err := document.ResolveDict(ctx, doc, resources["Properties"])
	if err != nil || props == nil {
		return out
	}
	for k, v := range props {
		ref, isRef := v.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		dict, err := document.ResolveDict(ctx, doc, ref)
		if err != nil || dict == nil {
			continue
		}
		if t, _ := document.ResolveName(ctx, doc, dict["Type"]); t == "OCG" {
			out[string(k)] = ocgVisible(ref, doc.OCConfig)
		}
	}
	return out
}
