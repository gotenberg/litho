package docx

import (
	"bytes"
	"context"
	"math"
	"strings"

	gofont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// sptPlainText is the shape type of a VML text path drawn straight
// (ECMA-376 Part 4, 14.1.2.20 o:spt; MS-OI29500's msosptTextPlainText),
// the type of WordArt and of Word's text watermarks.
const sptPlainText = 136

// maxTextPathRunes caps the characters of a VML text path drawn. Anti-DoS
// guard, not spec-mandated: WordArt and watermarks are a few words, and
// each glyph becomes a path.
const maxTextPathRunes = 1024

// textPathBaseline is where the baseline of a VML text path that is not
// trimmed lies, as a share of the shape's height from its top: Word sets
// the text at the shape's height and stretches it to the shape's width.
//
// # Corpus evidence
//
//   - litho/docx doc-watermark-text: Word prints DRAFT in Times New Roman
//     in a 468 by 117 point shape with capitals 77.5 points tall, the
//     face's cap height at 117 points, their baseline 91.7 points below
//     the top (0.784), the advances filling the 468 points; it prints
//     CONFIDENTIAL in Calibri in a shape of the same size with capitals
//     74.4 points tall, the baseline 89.9 points below the top (0.768).
const textPathBaseline = 0.776

// vmlTextPath builds a VML shape whose text path draws its string (Part
// 4, 14.1.2.22 textpath; its shape type's textpath turns it on): the
// glyphs' outlines in a box of w by h points, filled and stroked as the
// shape says, over the copy its shadow draws (14.1.2.18). A trimmed text
// path (trim) stretches the glyphs' ink to the box; any other is set at
// the box's height and stretched to its width. nil when the shape draws
// no text path.
//
// # Corpus evidence
//
//   - litho/docx shapes-wordart: Word stretches the ink of a trimmed
//     "Classic WordArt" in Arial bold to its 300 by 60 point box, both
//     ways to within 0.1 point, fills it with its gradient, strokes it
//     with its stroke colour, and draws its shadow 3 points right and 3
//     points down in the shadow's colour.
func (d *document) vmlTextPath(k *ooxml.Element, st *css.Style, w, h float64) *layout.Node {
	tp := k.Child(ooxml.NSV, "textpath")
	typ := d.vmlTypeElems[strings.TrimPrefix(attrOr(k, "type"), "#")]
	ttp := typ.Child(ooxml.NSV, "textpath")
	on := func(name string) bool {
		if v, ok := tp.Attr(name); ok {
			return vmlOn(tp, name) && v != ""
		}
		if v, ok := ttp.Attr(name); ok {
			return vmlOn(ttp, name) && v != ""
		}
		return false
	}
	str := attrOr(tp, "string")
	if tp == nil || !on("on") || strings.TrimSpace(str) == "" || w <= 0 || h <= 0 {
		return nil
	}
	if r := []rune(str); len(r) > maxTextPathRunes {
		str = string(r[:maxTextPathRunes])
	}
	vs := ooxml.ParseVMLStyle(attrOr(tp, "style"))
	family := strings.Trim(strings.TrimSpace(vs["font-family"]), `"'`)
	ts := text.Style{
		Families: text.BrowserFamilies(d.families(runProps{fonts: fontSpec{ascii: family, hAnsi: family}})),
		Size:     h,
		Weight:   400,
		Italic:   vs["font-style"] == "italic",
	}
	if fw := vs["font-weight"]; fw == "bold" || fw == "bolder" || fw == "700" || fw == "800" || fw == "900" {
		ts.Weight = 700
	}
	trim := on("trim")
	ss := vmlShapeStyle(k)
	shadow, shadowDX, shadowDY := vmlShadow(k)
	fonts := d.fonts
	ps := inlineStyle(st)
	ps.Width, ps.Height = css.Pts(w), css.Pts(h)
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	pic := &layout.Picture{Width: w, Height: h}
	pic.Vector = func(ctx context.Context, r typeset.Rect) []typeset.Fragment {
		if fonts == nil {
			return nil
		}
		lines, _, err := fonts.Layout(ctx, []text.Run{{Text: str, Style: ts}}, math.MaxFloat32, text.Options{Whitespace: text.Preserve})
		if err != nil || len(lines) == 0 || !numeric.Positive(lines[0].Width) {
			return nil
		}
		ops := glyphOutlines(lines[0].Runs)
		x0, y0, x1, y1, ok := opsBounds(ops)
		if !ok {
			return nil
		}
		// The outlines are at the baseline origin, y downward.
		m := typeset.Matrix{A: r.Width / lines[0].Width, D: 1, E: r.X, F: r.Y + textPathBaseline*r.Height}
		if trim && x1 > x0 && y1 > y0 {
			m = typeset.Matrix{A: r.Width / (x1 - x0), D: r.Height / (y1 - y0)}
			m.E, m.F = r.X-x0*m.A, r.Y-y0*m.D
		}
		ops = transformOps(ops, m)
		var out []typeset.Fragment
		if shadow != nil {
			sh := transformOps(ops, typeset.Matrix{A: 1, D: 1, E: shadowDX, F: shadowDY})
			out = append(out, shape.Fill(sh, r, shape.Paint{Kind: shape.PaintSolid, Color: *shadow})...)
		}
		return append(out, ss.PaintOps(ops, r)...)
	}
	return &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic, Alt: str}
}

// vmlShadow returns the colour and offset of a VML shape's shadow (Part 4,
// 14.1.2.18 shadow: on, color, gray by default, offset, 2 points right
// and down by default), nil when the shape has none.
func vmlShadow(k *ooxml.Element) (*typeset.Color, float64, float64) {
	s := k.Child(ooxml.NSV, "shadow")
	if s == nil || !vmlOn(s, "on") || attrOr(s, "on") == "" {
		return nil, 0, 0
	}
	c := css.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	if v, ok := ooxml.VMLColor(attrOr(s, "color")); ok {
		c = v
	}
	dx, dy := 2.0, 2.0
	if off := attrOr(s, "offset"); off != "" {
		a, b, _ := strings.Cut(off, ",")
		if v, ok := ooxml.Measure(strings.TrimSuffix(strings.TrimSpace(a), "px"), pxPt); ok {
			dx = v
		}
		if v, ok := ooxml.Measure(strings.TrimSuffix(strings.TrimSpace(b), "px"), pxPt); ok {
			dy = v
		}
	}
	return &typeset.Color{R: c.R, G: c.G, B: c.B, A: c.A}, dx, dy
}

// glyphOutlines returns the outlines of the glyphs of runs laid out on one
// line, in points from the line's left baseline point, y downward. A run
// whose face does not parse adds nothing.
func glyphOutlines(runs []*typeset.Text) []typeset.PathOp {
	var ops []typeset.PathOp
	faces := map[*typeset.Face]*gofont.Face{}
	for _, t := range runs {
		if t == nil || t.Face == nil {
			continue
		}
		f, ok := faces[t.Face]
		if !ok {
			// The text stack shaped the run with this program, so it
			// parses; one that does not adds no outline.
			f, _ = gofont.ParseTTF(bytes.NewReader(t.Face.Program)) //nolint:errcheck // a face without outlines draws nothing, see above.
			faces[t.Face] = f
		}
		if f == nil || f.Upem() == 0 {
			continue
		}
		k := t.Size / float64(f.Upem())
		hs := t.HScale
		if hs == 0 {
			hs = 1
		}
		for _, g := range t.Glyphs {
			ops = appendOutline(ops, f, g.GID, t.Origin.X+g.X*hs, t.Origin.Y+g.Y, k, hs)
		}
	}
	return ops
}

// appendOutline appends the outline of glyph gid of f at k points per font
// unit, its origin at x, y, scaled horizontally by hs, to ops. Quadratic
// segments become the cubic Béziers they equal.
func appendOutline(ops []typeset.PathOp, f *gofont.Face, gid uint16, x, y, k, hs float64) []typeset.PathOp {
	data, ok := f.GlyphData(gofont.GID(gid)).(gofont.GlyphOutline)
	if !ok {
		return ops
	}
	pt := func(s opentype.SegmentPoint) typeset.Point {
		return typeset.Point{X: x + hs*float64(s.X)*k, Y: y - float64(s.Y)*k}
	}
	var last typeset.Point
	open := false
	for _, s := range data.Segments {
		switch s.Op {
		case opentype.SegmentOpMoveTo:
			if open {
				ops = append(ops, typeset.PathOp{Kind: typeset.ClosePath})
			}
			last = pt(s.Args[0])
			ops = append(ops, typeset.PathOp{Kind: typeset.MoveTo, P: [3]typeset.Point{last}})
			open = true
		case opentype.SegmentOpLineTo:
			last = pt(s.Args[0])
			ops = append(ops, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{last}})
		case opentype.SegmentOpQuadTo:
			c, to := pt(s.Args[0]), pt(s.Args[1])
			c1 := typeset.Point{X: last.X + 2*(c.X-last.X)/3, Y: last.Y + 2*(c.Y-last.Y)/3}
			c2 := typeset.Point{X: to.X + 2*(c.X-to.X)/3, Y: to.Y + 2*(c.Y-to.Y)/3}
			ops = append(ops, typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{c1, c2, to}})
			last = to
		case opentype.SegmentOpCubeTo:
			last = pt(s.Args[2])
			ops = append(ops, typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(s.Args[0]), pt(s.Args[1]), last}})
		}
	}
	if open {
		ops = append(ops, typeset.PathOp{Kind: typeset.ClosePath})
	}
	return ops
}

// opsBounds returns the box of the points of ops, control points
// included; false when ops holds none.
func opsBounds(ops []typeset.PathOp) (x0, y0, x1, y1 float64, ok bool) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, op := range ops {
		n := 1
		switch op.Kind {
		case typeset.CurveTo:
			n = 3
		case typeset.ClosePath:
			n = 0
		case typeset.MoveTo, typeset.LineTo:
		}
		for _, p := range op.P[:n] {
			x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
			ok = true
		}
	}
	return x0, y0, x1, y1, ok
}

// transformOps returns ops mapped through m.
func transformOps(ops []typeset.PathOp, m typeset.Matrix) []typeset.PathOp {
	out := make([]typeset.PathOp, len(ops))
	for i, op := range ops {
		out[i].Kind = op.Kind
		for j, p := range op.P {
			out[i].P[j] = typeset.Point{X: m.A*p.X + m.C*p.Y + m.E, Y: m.B*p.X + m.D*p.Y + m.F}
		}
	}
	return out
}
