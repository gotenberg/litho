package page

import (
	"errors"
	"fmt"
	"image"
	"log/slog"
	"reflect"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/function"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
	"github.com/gotenberg/litho/pkg/internal/render/colorconv"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// softMaskCacheShare bounds the soft masks one render keeps for reuse: their
// pixels together may reach 1/softMaskCacheShare of the render pixel budget,
// past which the cache is emptied. Anti-DoS, not spec-mandated: a mask is
// rendered once per graphics state that installs it (ISO 32000-1, 11.6.5.2)
// and then serves every painting under it, so a page needs few at once,
// while a page of thousands of gs operators each installing a mask would
// otherwise hold thousands of page-sized masks.
const softMaskCacheShare = 4

// maxMaskReduction is the coarsest a soft mask's group renders at when it
// does not fit the render pixel budget at device resolution: one mask value
// for a square of at most maxMaskReduction device pixels a side. A quality
// floor, not spec-mandated nor a resource limit: past it the mask is too
// coarse to stand for the one the file draws, and what it masks is hidden
// instead. Sixteen pixels are 1.4 mm at 300 dpi, and a group needs that
// reduction only when its planes pass 256 times what the budget has left.
const maxMaskReduction = 16

// errOverBudget reports a soft mask whose rasters would pass the render
// pixel budget even at [maxMaskReduction], which then hides what it masks.
var errOverBudget = errors.New("soft mask exceeds the render pixel budget")

// maskKey names a soft mask as it renders: the dictionary the graphics
// state installed, by identity, the CTM it was installed under and, for a /G
// with no /Resources of its own, the resources it reads instead (zero
// otherwise). Two gs operators installing one dictionary under one CTM
// render one mask, which a page that sets the same state around every
// painting, or forms that each name it in resources of their own, do
// hundreds of times.
type maskKey struct {
	dict, resources uintptr
	ctm             [6]float64
}

func identity(d ast.PdfDict) uintptr {
	return reflect.ValueOf(d).Pointer()
}

// maskCache keeps the soft masks rendered so far ([maskKey]); nil records a
// mask that does not render. It holds a reference to each dictionary it
// keys, so an identity is never reused while it is in the cache.
type maskCache struct {
	masks   map[maskKey]*blend.Mask
	dicts   []ast.PdfDict
	pixels  int64
	release []func()
}

func newMaskCache() *maskCache {
	return &maskCache{masks: make(map[maskKey]*blend.Mask)}
}

// empty drops every mask c holds and releases their pixels.
func (c *maskCache) empty() {
	for _, release := range c.release {
		release()
	}
	clear(c.masks)
	c.release, c.dicts, c.pixels = nil, nil, 0
}

// keepMask caches m under key, emptying the cache first when m would pass
// its share of the budget. kept holds the dictionaries key names.
func (r *renderer) keepMask(key maskKey, m *blend.Mask, kept ...ast.PdfDict) {
	c := r.masks
	var px int64
	if m != nil {
		px = m.Pixels()
	}
	capacity := r.budget / softMaskCacheShare
	if c.pixels+px > capacity {
		c.empty()
	}
	if px > capacity {
		return
	}
	if px > 0 {
		release, err := r.reserve(px)
		if err != nil {
			return
		}
		c.release = append(c.release, release)
	}
	c.masks[key] = m
	c.dicts = append(c.dicts, kept...)
	c.pixels += px
}

// softMask returns the soft mask sm installs (ISO 32000-1, 11.6.5.2), nil
// for none. A mask that does not render, its /G missing or damaged, is
// skipped and the painting goes unmasked, as a viewer paints it. One past
// the render pixel budget is counted in [TransparencyOverBudget] and
// renders coarser ([renderer.maskGroup]); past [maxMaskReduction] it hides
// what it masks, rather than showing what the file masks out.
func (r *renderer) softMask(sm *graphics.SoftMask) (*blend.Mask, error) {
	if sm == nil {
		return nil, nil
	}
	own := maskKey{dict: identity(sm.Dict), ctm: sm.CTM}
	borrowed := own
	borrowed.resources = identity(r.m.Resources)
	if m, ok := r.masks.masks[own]; ok {
		return m, nil
	}
	if m, ok := r.masks.masks[borrowed]; ok {
		return m, nil
	}
	m, ownResources, err := r.renderSoftMask(sm)
	switch {
	case err == nil:
	case errors.Is(err, errOverBudget):
		r.report.count(TransparencyOverBudget)
		r.doc.Logger.Debug("render: hiding what a soft mask masks", slog.Any("err", err))
		m = blend.ConstantMask(0)
	case fatal(err):
		return nil, err
	default:
		r.skip("soft mask", err)
	}
	if ownResources {
		r.keepMask(own, m, sm.Dict)
	} else {
		r.keepMask(borrowed, m, sm.Dict, r.m.Resources)
	}
	return m, nil
}

// renderSoftMask renders the group of a soft mask dictionary (ISO 32000-1,
// 11.6.5.2, Table 144) and derives the mask from it. Per Table 144 the group
// /G is painted with the CTM in force when the dictionary was installed, and
// every other graphics state parameter at its initial value; a /Luminosity
// group is composited over the opaque backdrop /BC, in the group's colour
// space and black by default (11.5.3), an /Alpha group over a transparent
// one (11.5.2). The values pass through /TR.
//
// It reports whether /G holds /Resources of its own; one without reads the
// resources of the stream in force.
func (r *renderer) renderSoftMask(sm *graphics.SoftMask) (*blend.Mask, bool, error) {
	dict := sm.Dict
	kind, err := document.ResolveName(r.ctx, r.doc, dict["S"])
	if err != nil {
		return nil, true, fmt.Errorf("soft mask /S: %w", err)
	}
	if kind != "Alpha" && kind != "Luminosity" {
		return nil, true, fmt.Errorf("soft mask /S /%s is neither /Alpha nor /Luminosity", kind)
	}
	ref, _ := dict["G"].(ast.PdfIndirectRef)
	stream, err := document.ResolveStream(r.ctx, r.doc, dict["G"])
	if err != nil || stream.Dict == nil {
		return nil, true, fmt.Errorf("soft mask /G is not a stream: %w", err)
	}
	matrix, box, err := r.formGeometry(stream.Dict)
	if err != nil {
		return nil, true, err
	}
	// Table 144 requires /G to be a transparency group; deliberate leniency
	// toward one without /Group: it is painted as a non-isolated,
	// non-knockout group, the defaults of Table 147.
	attrs, cs, _ := r.groupAttrs(stream.Dict)
	own := true
	resources, err := document.ResolveDict(r.ctx, r.doc, stream.Dict["Resources"])
	if err != nil || resources == nil {
		resources, own = r.m.Resources, false
	} else {
		resources = r.withPageNames(ref, resources)
	}
	m, err := r.maskOf(sm, kind, ref, stream, matrix, box, attrs, cs, resources)
	return m, own, err
}

// maskOf renders a soft mask's group, the form stream with the geometry,
// attributes and colour space read from it, against resources.
func (r *renderer) maskOf(sm *graphics.SoftMask, kind ast.PdfName, ref ast.PdfIndirectRef, stream ast.PdfStream,
	matrix geom.Matrix, box [4]float64, attrs blend.Attributes, cs ast.PdfObject, resources ast.PdfDict,
) (*blend.Mask, error) {
	tr := r.maskTransfer(sm.Dict["TR"])
	// The group blends in its /CS, which also gives /BC its components
	// (Table 144) and the luminosity its reading (11.5.3).
	attrs.Space = r.groupSpace(cs, resources)
	var bc blend.Components
	backdrop := blend.Backdrop{}
	if kind == "Luminosity" {
		bc = r.backdropColor(sm.Dict["BC"], cs, resources, attrs.Space)
		backdrop = blend.Opaque(bc)
	}

	toDevice := matrix.Mul(geom.Matrix(sm.CTM)).Mul(r.device)
	b, _ := boxPath(toDevice, box).Bounds()
	bounds := r.pixelRect(b)
	if bounds.Empty() {
		// A group that covers no pixel derives the mask's outside value
		// everywhere (11.6.5.2), which an empty one-pixel group does too.
		bounds = image.Rect(0, 0, 1, 1)
	}
	g, release, scale, err := r.maskGroup(bounds, backdrop, attrs)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, errOverBudget
	}
	defer release()

	width, height, device := r.width, r.height, r.device
	if scale > 1 {
		r.report.count(TransparencyOverBudget)
		width, height = ceilDiv(width, scale), ceilDiv(height, scale)
		device = device.Mul(geom.Matrix{1 / float64(scale), 0, 0, 1 / float64(scale), 0, 0})
	}
	sub := r.sub(width, height, device, g)
	if scale == 1 {
		sub.masks = r.masks
	} else {
		// A coarse group's masks belong to its own device space, and their
		// pixels return to the budget with it.
		defer sub.masks.empty()
	}
	sub.m.Current().CTM = sm.CTM
	parent := &scope{resources: resources, depth: r.depth}
	err = sub.formXObject(parent, ref, stream, false)
	r.ops = sub.ops
	if err != nil {
		return nil, err
	}

	var m *blend.Mask
	if kind == "Luminosity" {
		if m, err = blend.LuminosityMask(r.ctx, g, bc, tr, r.budget); err != nil {
			return nil, fmt.Errorf("deriving a luminosity mask: %w", err)
		}
	} else if m, err = blend.AlphaMask(r.ctx, g, tr, r.budget); err != nil {
		return nil, fmt.Errorf("deriving an alpha mask: %w", err)
	}
	return m.Coarse(scale), nil
}

// maskGroup starts the group a soft mask's group paints into over bounds,
// device pixels, and returns it with the release of its pixels and the
// scale it renders at: 1, device resolution, when it fits the render pixel
// budget there, and otherwise the smallest reduction up to
// [maxMaskReduction] it fits at, its bounds then counted in squares of scale
// device pixels. It returns a nil group, without error, when none fits.
//
// A soft mask only modulates the opacity of what it masks (ISO 32000-1,
// 11.6.5.2), so a coarse one is a bounded approximation: rendered at
// 1/scale, each value is close to the mean of the scale × scale values the
// device-resolution mask holds, exactly so where its content is flat
// colour, and reading them bilinearly ([blend.Mask.Coarse]) leaves the mask
// exact wherever it does not change within scale device pixels. Within that
// distance of an edge the value differs by at most the step the edge makes,
// so an edge spreads over at most 2 × scale pixels instead of one. Hiding
// what the mask covers, or showing it unmasked, errs by that step
// everywhere.
func (r *renderer) maskGroup(bounds image.Rectangle, backdrop blend.Backdrop, attrs blend.Attributes) (*blend.Group, func(), int, error) {
	for scale := 1; scale <= maxMaskReduction; scale++ {
		coarse := image.Rect(bounds.Min.X/scale, bounds.Min.Y/scale, ceilDiv(bounds.Max.X, scale), ceilDiv(bounds.Max.Y, scale))
		g, release, err := r.newGroup(coarse, backdrop, attrs)
		if err != nil || g != nil {
			return g, release, scale, err
		}
	}
	return nil, nil, 0, nil
}

// ceilDiv returns n / d rounded up, for n >= 0 and d > 0.
func ceilDiv(n, d int) int {
	return (n + d - 1) / d
}

// maskTransfer compiles a soft mask's /TR (ISO 32000-1, Table 144): nil for
// /Identity, the default. A function that does not compile, or that is not
// one input to one output, is skipped and the identity applies.
func (r *renderer) maskTransfer(obj ast.PdfObject) *function.Function {
	if obj == nil {
		return nil
	}
	if name, err := document.ResolveName(r.ctx, r.doc, obj); err == nil && name == "Identity" {
		return nil
	}
	fn, err := function.New(r.ctx, r.doc, obj)
	if err != nil {
		r.skip("soft mask transfer function", err)
		return nil
	}
	if fn.Inputs() != 1 || fn.Outputs() != 1 {
		r.skip("soft mask transfer function", fmt.Errorf("%w: %d inputs, %d outputs", blend.ErrTransfer, fn.Inputs(), fn.Outputs()))
		return nil
	}
	return fn
}

// backdropColor reads a luminosity mask's /BC (ISO 32000-1, Table 144): the
// components of a colour in the group's colour space cs, returned as
// components of sp, the group's blending colour space: as they are when sp
// is the space cs names, converted to sRGB otherwise. Absent, it is black, the
// initial colour of the space (8.6.5). A group with no /CS reads the
// components as the device space of their count. A colour that does not
// convert is skipped and black applies.
func (r *renderer) backdropColor(obj, cs ast.PdfObject, resources ast.PdfDict, sp *blend.Space) blend.Components {
	black := blend.Components{}
	if sp.Kind() == blend.CMYK {
		black[3] = 1
	}
	if obj == nil {
		return black
	}
	arr, err := document.ResolveArray(r.ctx, r.doc, obj)
	if err != nil {
		r.skip("soft mask backdrop", err)
		return black
	}
	comps := make([]float64, 0, len(arr))
	for _, e := range arr {
		f, ok := document.Number(e)
		if !ok {
			r.skip("soft mask backdrop", errors.New("a /BC entry is no number"))
			return black
		}
		comps = append(comps, f)
	}
	space := cs
	if space == nil {
		switch len(comps) {
		case 1:
			space = ast.PdfName("DeviceGray")
		case 3:
			space = ast.PdfName("DeviceRGB")
		case 4:
			space = ast.PdfName("DeviceCMYK")
		default:
			r.skip("soft mask backdrop", fmt.Errorf("/BC of %d components and no group /CS", len(comps)))
			return black
		}
	}
	conv, err := colorconv.ForObject(r.ctx, r.doc, space, resources, r.colorOptions())
	if err != nil {
		r.skip("soft mask backdrop", err)
		return black
	}
	if sp != nil && cs != nil && len(comps) == sp.Kind().Channels() {
		return componentsOf(comps, len(comps))
	}
	cr, cg, cb := conv.RGB(comps)
	if sp != nil {
		return sp.FromRGB([3]float32{cr, cg, cb})
	}
	return blend.Components{cr, cg, cb}
}

// sub returns a renderer that paints into root, a group over a device raster
// of width × height pixels, device mapping default user space to those
// pixels, from the initial graphics state. It shares the render's budgets
// and caches but its own soft masks, which belong to its device space; the
// caller copies its operator count back.
func (r *renderer) sub(width, height int, device geom.Matrix, root *blend.Group) *renderer {
	s := *r
	s.width, s.height, s.device = width, height, device
	s.m = graphics.NewMachine([4]float64{})
	s.m.Doc = r.doc
	s.clips = []*clip{newClip(width, height)}
	s.groups = []frame{{group: root, direct: root.Direct()}}
	s.masks = newMaskCache()
	s.chain = slices.Clone(r.chain)
	// What a soft mask's group and a pattern's cells paint belongs to the
	// element they shape or fill, whose kind decided whether it is drawn.
	s.omit = 0
	// [Options.Within] marks pixels of the page, not of this raster.
	s.within, s.shared = nil, nil
	return &s
}
