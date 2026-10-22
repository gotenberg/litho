package page

import (
	"fmt"
	"image"
	"log/slog"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/function"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
	"github.com/gotenberg/litho/pkg/internal/render/colorconv"
)

// fromRGBMemoBits sizes the table a [blendConverter] remembers the colours
// it converted from sRGB in: 4096 slots. Anti-DoS guard, not spec-mandated:
// the table is direct-mapped and never grows, so an image of a million
// colours costs a million conversions and no memory, while the colours of a
// flat-coloured page all fit.
const fromRGBMemoBits = 12

// maxBlendSpaces bounds the blending colour spaces and the tint transforms
// one render keeps. Anti-DoS guard, not spec-mandated: a page names a
// handful of group spaces and spot colours, and past the bound a space or a
// transform is simply read again on each use.
const maxBlendSpaces = 1024

// blendConverter connects a grey or CMYK blending colour space with sRGB.
// To sRGB it reads the space as [colorconv] reads a colour in it, so a
// colour composited in the space shows as the same colour painted in sRGB
// does. From sRGB it converts a CMYK space through an ICC link from sRGB to
// the profile the space is read through, and a grey space, or a CMYK space
// whose profile holds no tables back from the connection space, by the
// device formulas of ISO 32000-1, 10.3.
//
// It is safe for concurrent use.
type blendConverter struct {
	kind blend.Kind
	to   colorconv.Converter
	// link converts sRGB to the CMYK space, nil for the device formula.
	link *icc.Link
	// formula is the space converted by the device formulas.
	formula *blend.Space

	mu   sync.Mutex
	in   [4]float64
	keys []uint32
	memo []blend.Components
}

func (c *blendConverter) ToRGB(comps blend.Components) [3]float32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.kind.Channels()
	for i := range n {
		c.in[i] = float64(comps[i])
	}
	r, g, b := c.to.RGB(c.in[:n])
	return [3]float32{r, g, b}
}

func (c *blendConverter) FromRGB(rgb [3]float32) blend.Components {
	if c.link == nil {
		return c.formula.FromRGB(rgb)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// The renderer's rasters hold 8-bit colours at heart, so a colour is
	// quantised to 8 bits a channel and converted as such, whatever was
	// converted before; bit 24 keeps the empty slot unmatched.
	q := [3]uint8{to8(rgb[0]), to8(rgb[1]), to8(rgb[2])}
	key := uint32(1)<<24 | uint32(q[0])<<16 | uint32(q[1])<<8 | uint32(q[2])
	if c.keys == nil {
		c.keys = make([]uint32, 1<<fromRGBMemoBits)
		c.memo = make([]blend.Components, 1<<fromRGBMemoBits)
	}
	slot := (uint64(key) * 0x9E3779B97F4A7C15) >> (64 - fromRGBMemoBits)
	if c.keys[slot] == key {
		return c.memo[slot]
	}
	in := [3]float64{float64(q[0]) / 255, float64(q[1]) / 255, float64(q[2]) / 255}
	var out [4]float64
	c.link.Convert(in[:], out[:])
	comps := blend.Components{float32(out[0]), float32(out[1]), float32(out[2]), float32(out[3])}
	c.keys[slot], c.memo[slot] = key, comps
	return comps
}

// to8 rounds v, in [0, 1], to an 8-bit level.
func to8(v float32) uint8 {
	switch {
	case !(v > 0):
		return 0
	case v >= 1:
		return 255
	}
	return uint8(v*255 + 0.5)
}

// groupSpace returns the blending colour space a transparency group's /CS
// object cs names (ISO 32000-1, 11.3.4, Table 147), against resources, or nil
// for sRGB: when cs is absent or names an RGB space, which the renderer
// blends in sRGB, or a space it does not blend in. DeviceGray, CalGray and a
// one-component ICCBased space are grey; DeviceCMYK and a four-component
// ICCBased space are CMYK. An ICCBased space whose profile litho cannot use
// sorts as its /Alternate does ([renderer.readGroupSpace]). Spaces are kept
// per render by their definition.
//
// Lab and the special families are no blending space Table 147 admits, and
// a calibrated or ICC RGB space blends in sRGB rather than in its own
// primaries: the renderer's elements are all sRGB, so the round trip would
// only approximate them.
func (r *renderer) groupSpace(cs ast.PdfObject, resources ast.PdfDict) *blend.Space {
	if cs == nil {
		return nil
	}
	resolved, err := document.ResolveObject(r.ctx, r.doc, cs)
	if err != nil {
		return nil
	}
	read, substituted := r.readGroupSpace(resolved)
	kind, key, ok := r.blendKind(read)
	if !ok {
		return nil
	}
	if substituted {
		// The space is still the group's /CS: colours selected in it keep
		// their components ([renderer.nativeColor]), and it is told apart
		// from the alternate named on its own.
		key = fmt.Sprintf("%v", resolved)
	}
	if sp, ok := r.spaces[key]; ok {
		return sp
	}
	to, err := colorconv.ForObject(r.ctx, r.doc, cs, resources, colorconv.Options{Page: r.page.Dict, Cache: r.colorCache})
	if err != nil {
		r.skip("group colour space", err)
		return nil
	}
	conv := &blendConverter{kind: kind, to: to, formula: blend.NewSpace(kind, key, nil)}
	if kind == blend.CMYK {
		conv.link = r.linkFromRGB(read)
	}
	sp := blend.NewSpace(kind, key, conv)
	if len(r.spaces) < maxBlendSpaces {
		r.spaces[key] = sp
	}
	return sp
}

// readGroupSpace returns the resolved colour space a group whose resolved
// /CS is cs blends in: cs, or the /Alternate of an ICCBased space whose
// profile litho cannot use ([pdfimage.SubstituteICCAlternate]), which ISO
// 32000-1, 8.6.5.5, Table 66 has a reader use in its place, and which the
// group's colours are converted through ([colorconv.ForObject]). The intent
// is relative colorimetric, the one those colours are read under. An
// alternate that is a special space then blends in sRGB, as Table 147
// admits none. It reports whether the alternate replaced cs.
func (r *renderer) readGroupSpace(cs ast.PdfObject) (ast.PdfObject, bool) {
	alt, ok := pdfimage.SubstituteICCAlternate(r.ctx, r.doc, cs, icc.RelativeColorimetric, pdfimage.RoleSpace)
	if !ok {
		return cs, false
	}
	resolved, err := document.ResolveObject(r.ctx, r.doc, alt)
	if err != nil {
		return cs, false
	}
	return resolved, true
}

// blendKind returns the blending family of the resolved colour space object
// cs and the key its definition is told apart by, false when the renderer
// blends it in sRGB.
func (r *renderer) blendKind(cs ast.PdfObject) (blend.Kind, string, bool) {
	switch v := cs.(type) {
	case ast.PdfName:
		switch v {
		case graphics.FamilyDeviceGray:
			return blend.Gray, string(v), true
		case graphics.FamilyDeviceCMYK:
			return blend.CMYK, string(v), true
		}
	case ast.PdfArray:
		if len(v) < 2 {
			return 0, "", false
		}
		family, err := document.ResolveName(r.ctx, r.doc, v[0])
		if err != nil {
			return 0, "", false
		}
		key := fmt.Sprintf("%v", v)
		switch family {
		case graphics.FamilyCalGray:
			return blend.Gray, key, true
		case graphics.FamilyICCBased:
			space, err := graphics.DescribeColorSpace(r.ctx, r.doc, v)
			if err != nil {
				return 0, "", false
			}
			switch space.Components {
			case 1:
				return blend.Gray, key, true
			case 4:
				return blend.CMYK, key, true
			}
		}
	}
	return 0, "", false
}

// linkFromRGB returns the ICC link from sRGB to the CMYK profile the resolved
// space cs is read through: its own for an ICCBased space, the page's
// DeviceCMYK reading otherwise ([pdfimage.CMYKProfile]). It returns nil, and
// the device formula then applies, when the profile holds no table back from
// the connection space.
func (r *renderer) linkFromRGB(cs ast.PdfObject) *icc.Link {
	var dst *icc.Profile
	if _, isArray := cs.(ast.PdfArray); isArray {
		dst = pdfimage.ICCBasedProfile(r.ctx, r.doc, cs)
	} else {
		dst = pdfimage.CMYKProfile(r.ctx, r.doc, r.page.Dict)
	}
	if dst == nil {
		return nil
	}
	src, err := icc.Parse(icc.SRGBProfile())
	if err != nil {
		r.doc.Logger.Debug("render: reading the sRGB profile", slog.Any("err", err))
		return nil
	}
	link, err := icc.NewLink(src, dst, icc.TransformOptions{Intent: icc.RelativeColorimetric})
	if err != nil || link.OutputChannels() != 4 {
		r.doc.Logger.Debug("render: no link from sRGB to the group's CMYK profile; using the device formula", slog.Any("err", err))
		return nil
	}
	return link
}

// deviceCMYK returns the blending colour space of a page drawn for a CMYK
// device ([Options.CMYK]): DeviceCMYK, read in profile, or in the profile
// DeviceCMYK is read in for the page when profile is nil. Its key is
// DeviceCMYK's, so the device colours [renderer.nativeColor] passes through
// keep their components. A profile of another component count fails with
// [ErrInvalidOptions].
func (r *renderer) deviceCMYK(profile *icc.Profile) (*blend.Space, error) {
	name := ast.PdfName(graphics.FamilyDeviceCMYK)
	if profile == nil {
		if sp := r.groupSpace(name, nil); sp != nil {
			return sp, nil
		}
		// DeviceCMYK always converts; should it not, the device formulas
		// of ISO 32000-1, 10.3 connect it with sRGB.
		return blend.NewSpace(blend.CMYK, string(name), nil), nil
	}
	if profile.Components() != 4 {
		return nil, fmt.Errorf("%w: a CMYK device profile of %d components", ErrInvalidOptions, profile.Components())
	}
	srgb, err := icc.Parse(icc.SRGBProfile())
	if err != nil {
		return nil, fmt.Errorf("reading the sRGB profile: %w", err)
	}
	toRGB, err := icc.NewLink(profile, srgb, icc.TransformOptions{Intent: icc.RelativeColorimetric})
	if err != nil {
		return nil, fmt.Errorf("%w: linking the CMYK device profile to sRGB: %w", ErrInvalidOptions, err)
	}
	conv := &blendConverter{kind: blend.CMYK, to: linkConverter{toRGB}, formula: blend.NewSpace(blend.CMYK, string(name), nil)}
	link, err := icc.NewLink(srgb, profile, icc.TransformOptions{Intent: icc.RelativeColorimetric})
	if err == nil && link.OutputChannels() == 4 {
		conv.link = link
	} else {
		r.doc.Logger.Debug("render: no link from sRGB to the CMYK device profile; using the device formula", slog.Any("err", err))
	}
	return blend.NewSpace(blend.CMYK, string(name), conv), nil
}

// linkConverter reads colours through an ICC link to sRGB, a
// [colorconv.Converter]. It is safe for concurrent use, as [icc.Link] is.
type linkConverter struct{ link *icc.Link }

func (c linkConverter) RGB(components []float64) (r, g, b float32) {
	var out [3]float64
	c.link.Convert(components, out[:])
	return float32(out[0]), float32(out[1]), float32(out[2])
}

// solid is the colour of a painting of one colour: its sRGB, which a group
// blending in sRGB takes, and its components in the blending colour space of
// the group being painted when that is another.
type solid struct {
	rgb [3]float32
	in  blend.Components
}

// solidOf returns the solid colour of c, whose sRGB conv gave as rgb, for the
// group being painted. A colour in the group's own space, or in a device
// space the group's converts from by ISO 32000-1, 10.3, keeps its
// components ([renderer.nativeColor]); any other reaches the group through
// its sRGB (11.3.4 converts it into the blending colour space).
func (r *renderer) solidOf(s *scope, c graphics.Color, rgb [3]float32) solid {
	sp := r.top().group.Space()
	if sp == nil {
		return solid{rgb: rgb, in: blend.Components{rgb[0], rgb[1], rgb[2]}}
	}
	if comps, ok := r.nativeColor(s, c, sp); ok {
		return solid{rgb: rgb, in: comps}
	}
	return solid{rgb: rgb, in: sp.FromRGB(rgb)}
}

// nativeColor returns the components in sp of the colour c, painted against
// s's resources, when they need no trip through sRGB: c is in the space sp is,
// a DeviceGray colour enters a DeviceCMYK space as black alone (ISO 32000-1,
// 10.3), and a Separation or DeviceN colour whose alternate is sp's device
// space is its tint transform's output (8.6.6.4, 8.6.6.5). It reports false
// otherwise, under a transfer function, which applies to the device colour
// after conversion (10.5), and where a default colour space of the resources
// stands in for the device space (8.6.5.6).
func (r *renderer) nativeColor(s *scope, c graphics.Color, sp *blend.Space) (blend.Components, bool) {
	if r.currentTransfer() != nil {
		return blend.Components{}, false
	}
	key := sp.Key()
	switch c.Space.Family {
	case graphics.FamilyDeviceCMYK:
		if key == graphics.FamilyDeviceCMYK && !r.hasDefault(s, "DefaultCMYK") {
			return componentsOf(c.Components, 4), true
		}
	case graphics.FamilyDeviceGray:
		if r.hasDefault(s, "DefaultGray") {
			break
		}
		g := componentsOf(c.Components, 1)[0]
		switch key {
		case graphics.FamilyDeviceGray:
			return blend.Components{g}, true
		case graphics.FamilyDeviceCMYK:
			return blend.Components{0, 0, 0, 1 - g}, true
		}
	case graphics.FamilyICCBased, graphics.FamilyCalGray:
		if c.Space.Object != nil && fmt.Sprintf("%v", c.Space.Object) == key {
			return componentsOf(c.Components, sp.Kind().Channels()), true
		}
	case graphics.FamilySeparation, graphics.FamilyDeviceN:
		if (key == graphics.FamilyDeviceCMYK || key == graphics.FamilyDeviceGray) && !r.hasDefault(s, ast.PdfName("Default"+strings.TrimPrefix(key, "Device"))) {
			return r.tintIn(c, key, sp.Kind().Channels())
		}
	}
	return blend.Components{}, false
}

// componentsOf clips the first n of comps to [0, 1], a missing one 0.
func componentsOf(comps []float64, n int) blend.Components {
	var out blend.Components
	for i := range n {
		if i < len(comps) {
			out[i] = float32(min(max(comps[i], 0), 1))
		}
	}
	return out
}

// hasDefault reports whether s's resources name the default colour space
// name (ISO 32000-1, 8.6.5.6).
func (r *renderer) hasDefault(s *scope, name ast.PdfName) bool {
	if s == nil || s.resources == nil {
		return false
	}
	spaces, err := document.ResolveDict(r.ctx, r.doc, s.resources["ColorSpace"])
	if err != nil || spaces == nil {
		return false
	}
	_, ok := spaces[name]
	return ok
}

// tintIn evaluates the tint transform of c's Separation or DeviceN space when
// its alternate space is the device space family, of n components, and
// returns the result. Compiled transforms are kept per render by the space's
// definition, failures included.
func (r *renderer) tintIn(c graphics.Color, family string, n int) (blend.Components, bool) {
	arr, err := document.ResolveArray(r.ctx, r.doc, c.Space.Object)
	if err != nil || len(arr) < 4 {
		return blend.Components{}, false
	}
	alt, err := document.ResolveName(r.ctx, r.doc, arr[2])
	if err != nil || string(alt) != family {
		return blend.Components{}, false
	}
	key := fmt.Sprintf("%v", arr)
	fn, ok := r.tints[key]
	if !ok {
		fn, err = function.New(r.ctx, r.doc, arr[3])
		if err != nil || fn.Outputs() != n {
			fn = nil
		}
		if len(r.tints) < maxBlendSpaces {
			r.tints[key] = fn
		}
	}
	if fn == nil {
		return blend.Components{}, false
	}
	in := make([]float64, fn.Inputs())
	for i := range in {
		if i < len(c.Components) {
			in[i] = min(max(c.Components[i], 0), 1)
		}
	}
	out, err := fn.Eval(in)
	if err != nil || len(out) < n {
		return blend.Components{}, false
	}
	return componentsOf(out, n), true
}

// formSpace returns the blending colour space of a transparency group
// XObject painted into a raster of its own (ISO 32000-1, 11.6.6): the
// parent's when it is non-isolated, as it composites with the parent's stack
// (11.4.5), or states no /CS, the default Table 147 gives; the space its /CS
// names when its content paints with transparency ([renderer.paintsOpaque]).
//
// Deliberate leniency toward the rest, against Table 147, which converts
// every colour painted into a group into the group's /CS whether or not it
// composites with anything: a group whose content paints opaque, with
// Normal, stays in its parent's space. There the group's space changes no
// compositing, only the gamut of the trip into it and back, which poppler
// 26.09.0 and PDFium both skip, blending every group in their device RGB,
// while print producers such as InDesign state /CS /DeviceCMYK on groups
// that composite nothing, where honouring it would pull every RGB colour and
// image through the CMYK gamut.
func (r *renderer) formSpace(attrs blend.Attributes, cs ast.PdfObject, ref ast.PdfIndirectRef, resources ast.PdfDict) *blend.Space {
	parent := r.top().group.Space()
	if !attrs.Isolated || cs == nil || r.paintsOpaque(ref, resources) {
		return parent
	}
	return r.groupSpace(cs, resources)
}

// paintsOpaque reports whether the content of form ref, against resources,
// paints with no transparency at all, as [renderer.scanNormal] scans it with
// alpha set. The answer is kept per form.
func (r *renderer) paintsOpaque(ref ast.PdfIndirectRef, resources ast.PdfDict) bool {
	if v, ok := r.opaqueForms[ref.ObjectNum]; ok && ref.ObjectNum != 0 {
		return v
	}
	v := r.scanNormal(resources, true, 0, map[int]bool{ref.ObjectNum: true})
	if ref.ObjectNum != 0 {
		r.opaqueForms[ref.ObjectNum] = v
	}
	return v
}

// pageSpace returns the blending colour space of the page group (ISO
// 32000-1, 11.4.7), whose /Group /CS is cs: the space it names when force is
// set or the page's content paints with transparency, as
// [renderer.paintsOpaque] tells a form's, and sRGB, the space of the device
// the renderer draws for, otherwise. The leniency is [renderer.formSpace]'s.
func (r *renderer) pageSpace(cs ast.PdfObject, resources ast.PdfDict, force bool) *blend.Space {
	if cs == nil || !force && r.scanNormal(resources, true, 0, map[int]bool{}) {
		return nil
	}
	return r.groupSpace(cs, resources)
}

// maskBounds is the bounds of the pixels a clip lets through, kept for the
// last clip asked about: the elements painted under one clip share it, and
// keeping one entry holds no mask long past its clip.
type maskBounds struct {
	clip   *clip
	bounds image.Rectangle
}

// clipMaskBounds returns the device pixels the current clip's mask lets
// through, the whole raster when the clip is a rectangle alone.
func (r *renderer) clipMaskBounds() image.Rectangle {
	c := r.clip()
	if c.node == nil {
		return image.Rect(0, 0, r.width, r.height)
	}
	if r.lastMask.clip == c {
		return r.lastMask.bounds
	}
	// Only the clip's rectangle can let a pixel through, so only it is
	// scanned, and of it only the pixels the clip's shapes can cover.
	sx0, sx1, sy0, sy1 := c.span(r.width, r.height)
	scan := image.Rect(sx0, sy0, sx1, sy1)
	if c.node.values == nil {
		scan = scan.Intersect(c.node.bounds)
	}
	values, err := c.node.in(scan)
	if err != nil {
		// A clip whose coverage cannot be made bounds nothing; the
		// element's own painting meets the error.
		r.doc.Logger.Debug("render: clip bounds", slog.Any("err", err))
		return image.Rect(0, 0, r.width, r.height)
	}
	x0, y0, x1, y1 := scan.Max.X, scan.Max.Y, scan.Min.X, scan.Min.Y
	for y := scan.Min.Y; y < scan.Max.Y; y++ {
		row := values[(y-scan.Min.Y)*scan.Dx() : (y-scan.Min.Y+1)*scan.Dx()]
		for i, v := range row {
			if v > 0 {
				x := scan.Min.X + i
				x0, x1 = min(x0, x), max(x1, x+1)
				y0, y1 = min(y0, y), max(y1, y+1)
			}
		}
	}
	var b image.Rectangle
	if x0 < x1 {
		b = image.Rect(x0, y0, x1, y1)
	}
	r.lastMask = maskBounds{clip: c, bounds: b}
	return b
}

// translucentState reports whether the graphics state parameter dictionary
// gs paints with transparency besides its blend mode: a constant alpha below
// 1 (ISO 32000-1, 11.6.4.4) or a soft mask other than /None (11.6.5.2).
func (r *renderer) translucentState(gs ast.PdfDict) bool {
	for _, key := range []ast.PdfName{"CA", "ca"} {
		if v, ok := document.Number(gs[key]); ok && v < 1 {
			return true
		}
	}
	sm, ok := gs["SMask"]
	if !ok {
		return false
	}
	name, err := document.ResolveName(r.ctx, r.doc, sm)
	return err != nil || name != "None"
}

// maskedImage reports whether the XObject dictionary dict is an image with a
// soft mask of its own: /SMask (ISO 32000-1, 11.6.5.3), or /SMaskInData
// other than 0 (ISO 32000-1, Table 89).
func (r *renderer) maskedImage(dict ast.PdfDict) bool {
	if dict["SMask"] != nil {
		return true
	}
	v, ok := document.Number(dict["SMaskInData"])
	return ok && v != 0
}
