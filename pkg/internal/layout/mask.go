package layout

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maskLayer is one mask layer drawn: its fragments in flow coordinates
// and whether they mask by luminance rather than by alpha.
type maskLayer struct {
	frags     []typeset.Fragment
	luminance bool
	clip      typeset.Rect
	op        css.MaskComposite
	// raster reports a layer of a raster image or of one that cannot be
	// read, which Chromium does not apply.
	raster bool
}

// maskOf returns the soft mask of b's mask in flow coordinates, nil for
// none (CSS Masking 1, 6). Each layer is laid out as a background image
// is, over its origin box and clipped to its clip box. Layers that all
// mask by alpha and add up are one alpha mask: their source-over
// composite is the mask the layers add to. Otherwise each layer becomes
// its mask value, a grey from black to white, and the layers composite
// bottom up in a luminosity mask by blend modes that compute the
// operators on those values (ISO 32000-1, 11.3.5): add by screen,
// s + d - s·d; intersect by multiply, s·d; exclude by exclusion,
// s + d - 2·s·d; subtract, s·(1 - d), by multiplying s with the
// difference of white and what lies beneath.
//
// # Corpus evidence
//
//   - probe, html: Chromium writes a gradient or an SVG mask element as a
//     soft mask of an isolated transparency group, of subtype Alpha for a
//     gradient and Luminosity for a mask element or mask-mode luminance;
//     several layers that add as the content drawn once per layer under
//     each layer's soft mask, and the other operators through soft masks
//     with a transfer function. Its alpha intersect, subtract and exclude
//     depart from CSS Masking 1, 6.11, which litho keeps: two linear
//     gradient layers over orange (green channel 136 where the mask keeps,
//     255 where it masks out) read, at (10, 10), (10, 90) and (90, 90) of a
//     100 pixel box, 160, 153 and 243 for intersect where the clause gives
//     about 136, 248 and 255; its luminance intersect matches litho's
//     within 2 levels. It writes an element whose mask image is
//     a PNG, waiting 3 seconds or not, as an isolated group with no soft
//     mask: unmasked; so it draws an element whose mask names no element
//     or an image that does not exist.
func (p *painter) maskOf(b *box) *typeset.SoftMask {
	layers := b.st.MaskLayers()
	if len(layers) == 0 {
		return nil
	}
	drawn := make([]maskLayer, 0, len(layers))
	for _, l := range layers {
		ml := p.maskLayer(b, l)
		if ml.raster {
			// CSS Masking 1, 6.1 masks by the image, and by transparent
			// black for an image that cannot be read; Chromium prints the
			// element as if the layer were not there.
			p.e.warnOnce(warn.ContentUnsupported, warn.Omitted, "a raster mask image, or one that cannot be read, is not applied, as Chromium prints it", b.y)
			continue
		}
		drawn = append(drawn, ml)
	}
	if len(drawn) == 0 {
		return nil
	}
	alphaOnly := true
	for i, ml := range drawn {
		alphaOnly = alphaOnly && !ml.luminance && (ml.op == css.CompositeAdd || i == len(drawn)-1)
	}
	if alphaOnly {
		var content []typeset.Fragment
		for _, l := range slices.Backward(drawn) {
			content = append(content, l.frags...)
		}
		return &typeset.SoftMask{Kind: typeset.MaskAlpha, Content: content}
	}
	if len(drawn) == 1 {
		return &typeset.SoftMask{Kind: typeset.MaskLuminosity, Content: drawn[0].frags}
	}
	all := drawn[0].clip
	for _, l := range drawn[1:] {
		all = union(all, l.clip)
	}
	var acc []typeset.Fragment
	for i, l := range slices.Backward(drawn) {
		v := maskValue(l)
		if i == len(drawn)-1 {
			acc = v
			continue
		}
		switch l.op {
		case css.CompositeAdd:
			acc = append(acc, &typeset.Group{Opacity: 1, Blend: typeset.BlendScreen, Content: v})
		case css.CompositeIntersect:
			acc = append(acc, &typeset.Group{Opacity: 1, Blend: typeset.BlendMultiply, Content: v})
		case css.CompositeExclude:
			acc = append(acc, &typeset.Group{Opacity: 1, Blend: typeset.BlendExclusion, Content: v})
		case css.CompositeSubtract:
			acc = []typeset.Fragment{
				&typeset.Path{Ops: rectOps(all), Fill: &typeset.Fill{Color: typeset.Color{R: 1, G: 1, B: 1, A: 1}}},
				&typeset.Group{Opacity: 1, Blend: typeset.BlendDifference, Transparency: &typeset.TransparencyGroup{Isolated: true}, Content: acc},
				&typeset.Group{Opacity: 1, Blend: typeset.BlendMultiply, Content: v},
			}
		}
	}
	return &typeset.SoftMask{Kind: typeset.MaskLuminosity, Content: acc}
}

// maskValue returns the fragments that paint the mask value of l as an
// opaque grey over its clip box: black, then the layer itself for
// luminance, or white through the layer's alpha. Outside its clip box a
// layer's value is 0 (CSS Masking 1, 6.4).
func maskValue(l maskLayer) []typeset.Fragment {
	black := &typeset.Path{Ops: rectOps(l.clip), Fill: &typeset.Fill{Color: typeset.Color{A: 1}}}
	value := l.frags
	if !l.luminance {
		white := &typeset.Path{Ops: rectOps(l.clip), Fill: &typeset.Fill{Color: typeset.Color{R: 1, G: 1, B: 1, A: 1}}}
		value = []typeset.Fragment{&typeset.Group{Opacity: 1, Transparency: &typeset.TransparencyGroup{
			Isolated: true, Mask: &typeset.SoftMask{Kind: typeset.MaskAlpha, Content: l.frags},
		}, Content: []typeset.Fragment{white}}}
	}
	return []typeset.Fragment{&typeset.Group{Opacity: 1, Transparency: &typeset.TransparencyGroup{Isolated: true}, Content: append([]typeset.Fragment{black}, value...)}}
}

// maskLayer draws the layer l of b's mask.
func (p *painter) maskLayer(b *box, l css.MaskLayer) maskLayer {
	origin, _ := p.referenceBox(b, l.Origin)
	clip, _ := p.referenceBox(b, l.Clip)
	if m := p.maskRef; m != nil {
		origin, _ = fragReference(m.origin, l.Origin)
		clip, _ = fragReference(m.clip, l.Clip)
	}
	if l.NoClip {
		// The mask painting area is unrestricted (CSS Masking 1, 6.8):
		// it reaches every box b paints, its descendants' included.
		clip, _ = p.referenceBox(b, css.GeoBorder)
		if m := p.maskRef; m != nil {
			clip = m.clip.r
		}
		if d, ok := p.descendantBounds(b, maxMaskBoundsBoxes); ok {
			clip = union(clip, d)
		}
	}
	out := maskLayer{clip: clip, op: l.Composite, luminance: l.Mode == css.MaskLuminance}
	if l.Image != "" && p.e.opts.MaskSource != nil {
		if frags, alpha, found := p.e.opts.MaskSource(p.e.ctx, l.Image, p.borderRect(b)); found {
			// A mask element is laid out by its own attributes; its
			// mask-type decides match-source (CSS Masking 1, 6.3).
			out.luminance = l.Mode == css.MaskLuminance || l.Mode == css.MaskMatchSource && !alpha
			out.frags = []typeset.Fragment{&typeset.Group{Opacity: 1, Clip: &typeset.Path{Ops: rectOps(clip), Fill: &typeset.Fill{}}, Content: frags}}
			return out
		}
	}
	st := &css.BackgroundLayer{Image: l.Image, Gradient: l.Gradient, Repeat: l.Repeat, Position: l.Position, Size: l.Size}
	var frags []typeset.Fragment
	if l.Gradient != nil {
		for _, pa := range p.e.gradientTiles(st, origin, clip, origin.Y) {
			frags = append(frags, pa)
		}
	} else if pic := zoomedPicture(p.e.picture(l.Image, origin.Y), b.st.UsedZoom()); pic != nil {
		if pic.Vector == nil {
			out.raster = true
			return out
		}
		pic = naturalPicture(pic, origin.Width, origin.Height)
		tiles := p.e.tiles(st, pic, origin, clip, origin.Y)
		for i, t := range tiles {
			f := p.e.pictureFrag(pic, t, nil)
			if f == nil {
				break
			}
			if i == 0 && pic.Vector != nil {
				tiles = p.e.vectorTiles(tiles, fragNodes(f), origin.Y)
			}
			frags = append(frags, f)
		}
	} else if l.Image != "" {
		out.raster = true
		return out
	}
	if len(frags) > 0 {
		frags = []typeset.Fragment{&typeset.Group{Opacity: 1, Clip: &typeset.Path{Ops: rectOps(clip), Fill: &typeset.Fill{}}, Content: frags}}
	}
	out.frags = frags
	return out
}

// maxMaskBoundsBoxes caps the boxes descendantBounds visits. Anti-DoS
// guard, not spec-mandated: a masked element holds a handful of boxes;
// past the cap the bounds of the boxes visited stand for all of them.
const maxMaskBoundsBoxes = 4096

// descendantBounds returns the union of the border boxes of b's
// descendants in flow coordinates, b's own relative offset in force,
// visiting at most n boxes; false for none.
func (p *painter) descendantBounds(b *box, n int) (typeset.Rect, bool) {
	var out typeset.Rect
	set := false
	stack := slices.Clone(b.kids)
	for len(stack) > 0 && n > 0 {
		k := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n--
		if k.kind != kindText && k.kind != kindBreak {
			ox, oy := offset(k)
			r := typeset.Rect{X: k.x + p.dx + ox, Y: k.y + p.dy + oy, Width: k.borderW(), Height: k.borderH()}
			if r.Width > 0 && r.Height > 0 {
				if set {
					out = union(out, r)
				} else {
					out, set = r, true
				}
			}
		}
		stack = append(stack, k.kids...)
	}
	return out, set
}
