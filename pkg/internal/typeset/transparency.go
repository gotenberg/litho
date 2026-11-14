package typeset

import (
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// blendNames are the names of the blend modes, by [BlendMode] (ISO
// 32000-1, 11.3.5, Tables 136 and 137).
var blendNames = [...]string{
	BlendNormal:     "Normal",
	BlendMultiply:   "Multiply",
	BlendScreen:     "Screen",
	BlendOverlay:    "Overlay",
	BlendDarken:     "Darken",
	BlendLighten:    "Lighten",
	BlendColorDodge: "ColorDodge",
	BlendColorBurn:  "ColorBurn",
	BlendHardLight:  "HardLight",
	BlendSoftLight:  "SoftLight",
	BlendDifference: "Difference",
	BlendExclusion:  "Exclusion",
	BlendHue:        "Hue",
	BlendSaturation: "Saturation",
	BlendColor:      "Color",
	BlendLuminosity: "Luminosity",
}

// transparencyGroup paints the content of g as one transparency group
// (ISO 32000-1, 11.4): a form XObject with a /Group of subtype
// /Transparency (8.10.3, 11.6.6), painted by Do under an ExtGState that
// sets the group's constant alpha opacity, its blend mode blend and its
// soft mask (11.6.4.4: these apply to the group as a whole, and are reset
// to their initial values inside it). The q of [pageWriter.group] holds
// that state, and its transform and clip are in force.
//
// A group that paints nothing on the page, whose content or mask covers
// nothing of what its clip and the page show, writes no form.
func (p *pageWriter) transparencyGroup(ctx context.Context, g *Group, opacity float64, blend BlendMode) error {
	tg := g.Transparency
	box, ok, err := p.groupBox(ctx, g.Content, g.Clip)
	if err != nil || !ok {
		return err
	}
	var smask ast.PdfIndirectRef
	if tg.Mask != nil {
		// Outside what the mask's content covers, the mask is its
		// backdrop's: black, which masks out by luminosity, and no alpha
		// (11.6.5.2), so the group shows nothing there.
		mbox, ok, err := p.groupBox(ctx, tg.Mask.Content, g.Clip)
		if err != nil || !ok {
			return err
		}
		box = [4]float64{max(box[0], mbox[0]), max(box[1], mbox[1]), min(box[2], mbox[2]), min(box[3], mbox[3])}
		if box[2] <= box[0] || box[3] <= box[1] {
			return nil
		}
		if smask, err = p.softMask(ctx, tg.Mask, mbox); err != nil {
			return err
		}
	}
	attrs := ast.PdfDict{
		"Type": ast.PdfName("Group"),
		"S":    ast.PdfName("Transparency"),
	}
	switch tg.Space {
	case SpaceRGB:
		attrs["CS"] = ast.PdfName("DeviceRGB")
	case SpaceGray:
		attrs["CS"] = ast.PdfName("DeviceGray")
	case SpaceInherit:
	}
	if tg.Isolated {
		attrs["I"] = ast.PdfBool(true)
	}
	if tg.Knockout {
		attrs["K"] = ast.PdfBool(true)
	}
	ref, err := p.groupForm(ctx, g.Content, box, attrs, false)
	if err != nil {
		return err
	}
	p.forms++
	name := fmt.Sprintf("Fm%d", p.forms)
	p.xobjs = append(p.xobjs, xobjUse{name: name, ref: ref})
	a := alphaState{fill: round(opacity), stroke: round(opacity)}
	if smask.ObjectNum != 0 {
		mask := fmt.Sprintf("GM%d", len(p.masks)+1)
		p.masks = append(p.masks, maskUse{name: mask, alpha: a, smask: smask, blend: blend, withBlend: blend != p.blend})
		fmt.Fprintf(&p.buf, "/%s gs\n", mask)
		p.alpha[len(p.alpha)-1] = a
		p.blend = blend
	} else {
		p.setState(a, blend)
	}
	fmt.Fprintf(&p.buf, "/%s Do\n", name)
	return nil
}

// softMask writes the soft mask dictionary of m (ISO 32000-1, 11.6.5.2,
// Table 144) over box and returns its reference: its group paints m's
// content, in DeviceRGB for a luminosity mask, whose group Table 144 asks
// a colour space of. The backdrop and the transfer function are left to
// their defaults, black and the identity.
func (p *pageWriter) softMask(ctx context.Context, m *SoftMask, box [4]float64) (ast.PdfIndirectRef, error) {
	attrs := ast.PdfDict{
		"Type": ast.PdfName("Group"),
		"S":    ast.PdfName("Transparency"),
	}
	kind := ast.PdfName("Alpha")
	if m.Kind == MaskLuminosity {
		attrs["CS"] = ast.PdfName("DeviceRGB")
		kind = "Luminosity"
	}
	ref, err := p.groupForm(ctx, m.Content, box, attrs, true)
	if err != nil {
		return ast.PdfIndirectRef{}, err
	}
	return p.w.alloc.Alloc(ast.PdfDict{
		"Type": ast.PdfName("Mask"),
		"S":    kind,
		"G":    ref,
	}), nil
}

// groupForm writes content as a form XObject with the group attributes attrs
// and the bounding box box, in the coordinates written at the current
// level (ISO 32000-1, 8.10.2, Table 95), and returns its reference. The
// form's content stream starts with the alpha, blend mode and soft mask
// at their initial values, which 11.6.6 makes the state a transparency
// group's content starts in, and has resources of its own.
//
// In a tagged document the content of a group is tagged inside the form:
// its marked-content sequences belong to the form's stream, which the
// structure tree names through /StructParents and marked-content
// references with /Stm (14.7.4.3 and 14.7.4.4), and the Do that paints the
// form stands outside every sequence of the stream that paints it, so the
// sequences nest (14.6). A mask's content is never shown and is marked
// nothing.
func (p *pageWriter) groupForm(ctx context.Context, content []Fragment, box [4]float64, attrs ast.PdfDict, mask bool) (ast.PdfIndirectRef, error) {
	fw := newPageWriter(p.w, p.index, &Page{Width: p.width, Height: p.height})
	fw.off, fw.ctm, fw.steps = p.off, p.ctm, p.steps
	fw.bare = p.bare || mask
	st := p.w.st
	if st != nil && !fw.bare {
		fw.form = st.openForm(p.index, p.form, p.nextMCID)
	}
	if err := fw.fragments(ctx, content, 1); err != nil {
		return ast.PdfIndirectRef{}, err
	}
	fw.endMark()
	p.steps = fw.steps
	data := fw.buf.Bytes()
	p.w.hashBytes(data)
	stream, err := p.w.flateStream(ast.PdfDict{
		"Type":      ast.PdfName("XObject"),
		"Subtype":   ast.PdfName("Form"),
		"BBox":      ast.PdfArray{fine(box[0]), fine(box[1]), fine(box[2]), fine(box[3])},
		"Group":     attrs,
		"Resources": fw.resources(),
	}, data)
	if err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("writing page %d: %w", p.index+1, err)
	}
	ref := p.w.alloc.Alloc(stream)
	if fw.form >= 0 {
		st.closeForm(fw.form, ref, stream.Dict)
	}
	return ref, nil
}

// groupBox returns the box [x0 y0 x1 y1] a form painting content needs, in
// the coordinates written at the current level: what of the page the CTM
// shows, within clip when it is set, within what content paints when
// [pageWriter.bounds] measures it, grown by one unit a side for the
// rounding of what is written. Outside a form's BBox nothing of it paints
// (ISO 32000-1, 8.10.2), so a box that holds all of this changes nothing
// a reader shows. It reports false when nothing of content can show.
func (p *pageWriter) groupBox(ctx context.Context, content []Fragment, clip *Path) ([4]float64, bool, error) {
	c := p.ctm
	det := float64(c.A*c.D) - float64(c.B*c.C)
	if det == 0 || !numeric.Finite(det) {
		return [4]float64{}, false, nil
	}
	inv := Matrix{A: c.D / det, B: -c.B / det, C: -c.C / det, D: c.A / det}
	inv.E, inv.F = -(float64(c.E*inv.A) + float64(c.F*inv.C)), -(float64(c.E*inv.B) + float64(c.F*inv.D))
	view := newBounds()
	view.box(inv, 0, 0, p.width, p.height)
	box := [4]float64{view.x0, view.y0, view.x1, view.y1}
	if clip != nil {
		cb := newBounds()
		cb.ops(Matrix{A: 1, D: 1, E: p.off.X, F: p.off.Y}, clip.Ops, 0)
		box = intersect(box, cb)
	}
	b := newBounds()
	known, err := p.bounds(ctx, &b, content, Matrix{A: 1, D: 1, E: p.off.X, F: p.off.Y})
	if err != nil {
		return box, false, err
	}
	if known {
		box = intersect(box, b)
	}
	if !(box[2] >= box[0] && box[3] >= box[1]) || !numeric.Finite(box[0]+box[1]+box[2]+box[3]) {
		return box, false, nil
	}
	return [4]float64{box[0] - 1, box[1] - 1, box[2] + 1, box[3] + 1}, true, nil
}

func newBounds() bounds {
	return bounds{x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)}
}

// intersect returns the part of box within b.
func intersect(box [4]float64, b bounds) [4]float64 {
	return [4]float64{max(box[0], b.x0), max(box[1], b.y0), min(box[2], b.x1), min(box[3], b.y1)}
}

// ops adds the points and control points of ops under t, each grown by
// half: a cubic Bézier lies in the hull of its control points (ISO
// 32000-1, 8.5.2.2).
func (b *bounds) ops(t Matrix, ops []PathOp, half float64) {
	for _, op := range ops {
		n := 0
		switch op.Kind {
		case MoveTo, LineTo:
			n = 1
		case CurveTo:
			n = 3
		case ClosePath:
		}
		for _, pt := range op.P[:n] {
			b.box(t, pt.X-half, pt.Y-half, pt.X+half, pt.Y+half)
		}
	}
}

// bounds adds to b what frags paint under t, a box that holds every mark
// they make, and reports whether it measured them all: a text run whose
// face gives no glyph box is not measured.
//
//   - A path spans its points and control points, grown by the farthest a
//     stroke reaches past them: half the line width times the miter
//     limit for a miter join (ISO 32000-1, 8.4.3.5), times the square
//     root of two for a projecting square cap (8.4.3.3), whichever is
//     larger.
//   - A text run spans its face's glyph box at each glyph, under its size,
//     skew and horizontal scale (9.4.4), grown by half its stroke.
//   - An image spans its rectangle.
//   - A group spans what its content spans under its transform, within its
//     clip.
//
// Validate has bounded the nesting of groups the recursion follows.
func (p *pageWriter) bounds(ctx context.Context, b *bounds, frags []Fragment, t Matrix) (bool, error) {
	known := true
	for _, f := range frags {
		if err := p.step(ctx); err != nil {
			return false, err
		}
		switch v := f.(type) {
		case *Path:
			if len(v.Ops) == 0 || (v.Fill == nil && v.Stroke == nil) {
				continue
			}
			half := 0.0
			if s := v.Stroke; s != nil {
				reach := math.Sqrt2
				if s.Join == MiterJoin {
					miter := s.Miter
					if miter == 0 {
						miter = 10
					}
					reach = max(reach, miter)
				}
				half = s.Width / 2 * reach
			}
			b.ops(t, v.Ops, half)
		case *Image:
			b.box(t, v.Rect.X, v.Rect.Y, v.Rect.X+v.Rect.Width, v.Rect.Y+v.Rect.Height)
		case *Text:
			ok, err := p.textBounds(ctx, b, v, t)
			if err != nil {
				return false, err
			}
			known = known && ok
		case *Group:
			m := v.Transform
			if m == (Matrix{}) {
				m = Identity
			}
			inner := multiply(m, t)
			gb := newBounds()
			ok, err := p.bounds(ctx, &gb, v.Content, inner)
			if err != nil {
				return false, err
			}
			if v.Clip != nil {
				cb := newBounds()
				cb.ops(inner, v.Clip.Ops, 0)
				if ok {
					box := intersect([4]float64{gb.x0, gb.y0, gb.x1, gb.y1}, cb)
					gb = bounds{x0: box[0], y0: box[1], x1: box[2], y1: box[3]}
				} else {
					gb, ok = cb, true
				}
			}
			known = known && ok
			if gb.x1 >= gb.x0 && gb.y1 >= gb.y0 {
				b.point(Identity, gb.x0, gb.y0)
				b.point(Identity, gb.x1, gb.y1)
			}
		}
	}
	return known, nil
}

// textBounds adds the glyph boxes of v under t to b, false when its face
// gives no glyph box. The text matrix [size*hs 0 size*skew -size x y]
// maps a glyph's space (9.4.4), so the box's corners map to a
// parallelogram whose own box is added.
func (p *pageWriter) textBounds(ctx context.Context, b *bounds, v *Text, t Matrix) (bool, error) {
	if len(v.Glyphs) == 0 {
		return true, nil
	}
	fe, err := p.w.font(ctx, v.Face)
	if err != nil {
		return false, err
	}
	gb, ok := fe.glyphBox()
	if !ok {
		return false, nil
	}
	hs := v.HScale
	if hs == 0 {
		hs = 1
	}
	size, half := v.Size, float64(v.Stroke/2)
	gx0, gx1 := math.Inf(1), math.Inf(-1)
	for _, gy := range [2]float64{gb[1], gb[3]} {
		for _, gx := range [2]float64{gb[0], gb[2]} {
			x := float64(size*hs*gx) + float64(size*v.Skew*gy)
			gx0, gx1 = min(gx0, x), max(gx1, x)
		}
	}
	gy0, gy1 := float64(-size*gb[3]), float64(-size*gb[1])
	if v.Upright {
		// The text matrix [0 -size -size 0 x y] maps (gx, gy) to (x -
		// size*gy, y - size*gx).
		gx0, gx1 = gy0, gy1
		gy0, gy1 = float64(-size*gb[2]), float64(-size*gb[0])
	}
	for _, g := range v.Glyphs {
		x, y := v.Origin.X+float64(g.X*hs), v.Origin.Y+g.Y
		b.box(t, x+gx0-half, y+gy0-half, x+gx1+half, y+gy1+half)
	}
	return true, nil
}
