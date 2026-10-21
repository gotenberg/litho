package page

import (
	"errors"
	"fmt"
	"image"
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

// errOverBudget reports a soft mask whose rasters would pass the render
// pixel budget, which the painting then goes without.
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
		for _, release := range c.release {
			release()
		}
		clear(c.masks)
		c.release, c.dicts, c.pixels = nil, nil, 0
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
// skipped and the painting goes unmasked, as a viewer paints it; one past
// the render pixel budget is counted in [TransparencyOverBudget] and goes
// unmasked too.
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
	var bc [3]float32
	backdrop := blend.Backdrop{}
	if kind == "Luminosity" {
		bc = r.backdropColor(sm.Dict["BC"], cs, resources)
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
	g, release, err := r.newGroup(bounds, backdrop, attrs)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, errOverBudget
	}
	defer release()

	sub := r.sub(r.width, r.height, r.device, g)
	sub.masks = r.masks
	sub.m.Current().CTM = sm.CTM
	parent := &scope{resources: resources, depth: r.depth}
	err = sub.formXObject(parent, ref, stream, false)
	r.ops = sub.ops
	if err != nil {
		return nil, err
	}

	if kind == "Luminosity" {
		m, err := blend.LuminosityMask(r.ctx, g, bc, tr, r.budget)
		if err != nil {
			return nil, fmt.Errorf("deriving a luminosity mask: %w", err)
		}
		return m, nil
	}
	m, err := blend.AlphaMask(r.ctx, g, tr, r.budget)
	if err != nil {
		return nil, fmt.Errorf("deriving an alpha mask: %w", err)
	}
	return m, nil
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
// components of a colour in the group's colour space cs, converted to sRGB.
// Absent, it is black. A group with no /CS reads the components as the
// device space of their count. A colour that does not convert is skipped
// and black applies.
func (r *renderer) backdropColor(obj, cs ast.PdfObject, resources ast.PdfDict) [3]float32 {
	if obj == nil {
		return [3]float32{}
	}
	arr, err := document.ResolveArray(r.ctx, r.doc, obj)
	if err != nil {
		r.skip("soft mask backdrop", err)
		return [3]float32{}
	}
	comps := make([]float64, 0, len(arr))
	for _, e := range arr {
		f, ok := document.Number(e)
		if !ok {
			r.skip("soft mask backdrop", errors.New("a /BC entry is no number"))
			return [3]float32{}
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
			return [3]float32{}
		}
	}
	conv, err := colorconv.ForObject(r.ctx, r.doc, space, resources, r.colorOptions())
	if err != nil {
		r.skip("soft mask backdrop", err)
		return [3]float32{}
	}
	cr, cg, cb := conv.RGB(comps)
	return [3]float32{cr, cg, cb}
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
	return &s
}
