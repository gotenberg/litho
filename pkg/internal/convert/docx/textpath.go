package docx

import (
	"context"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/officefx"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
		Families: text.BrowserFamilies(d.families(runProps{fonts: fontSpec{ascii: family, hAnsi: family}}), ""),
		Size:     h,
		Weight:   400,
		Italic:   vs["font-style"] == "italic",
	}
	if fw := vs["font-weight"]; fw == "bold" || fw == "bolder" || fw == "700" || fw == "800" || fw == "900" {
		ts.Weight = 700
	}
	trim := on("trim")
	ss := vmlShapeStyle(k, typ)
	shadow, shadowDX, shadowDY := vmlShadow(k)
	fonts := d.fonts
	ps := inlineStyle(st)
	ps.Width, ps.Height = css.Pts(w), css.Pts(h)
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	pic := &layout.Picture{Width: w, Height: h}
	warp := d.vmlTextWarp(k, typ)
	pic.Vector = func(ctx context.Context, r typeset.Rect) []typeset.Fragment {
		if fonts == nil {
			return nil
		}
		lines, _, err := fonts.Layout(ctx, []text.Run{{Text: str, Style: ts}}, math.MaxFloat32, text.Options{Whitespace: text.Preserve})
		if err != nil || len(lines) == 0 || !numeric.Positive(lines[0].Width) {
			return nil
		}
		var outlines text.Outlines
		ops, warped := warpTextPath(ctx, lines, warp, r, &outlines)
		if !warped {
			for _, t := range lines[0].Runs {
				if t != nil {
					ops = outlines.Run(ops, t)
				}
			}
			x0, y0, x1, y1, ok := opsBounds(ops)
			if !ok {
				return nil
			}
			// The outlines are at the baseline origin, y downward.
			m := typeset.Matrix{A: r.Width / lines[0].Width, D: 1, E: r.X, F: r.Y + float64(textPathBaseline*r.Height)}
			if trim && x1 > x0 && y1 > y0 {
				m = typeset.Matrix{A: r.Width / (x1 - x0), D: r.Height / (y1 - y0)}
				m.E, m.F = r.X-float64(x0*m.A), r.Y-float64(y0*m.D)
			}
			ops = transformOps(ops, m)
		}
		var out []typeset.Fragment
		if shadow != nil {
			sh := transformOps(ops, typeset.Matrix{A: 1, D: 1, E: shadowDX, F: shadowDY})
			out = append(out, shape.Fill(sh, r, shape.Paint{Kind: shape.PaintSolid, Color: *shadow})...)
		}
		return append(out, ss.PaintOps(ops, r)...)
	}
	return &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic, Alt: str}
}

// vmlTextWarp returns the path a VML text path of a shape type other
// than plain text sets its string along (Part 4, 14.1.2.22 textpath), nil
// for plain text or a shape whose path does not read.
func (d *document) vmlTextWarp(k, typ *ooxml.Element) *shape.TextWarp {
	if d.vmlType(k) == sptPlainText {
		return nil
	}
	src, ok := ooxml.VMLTextPath(k, typ)
	if !ok {
		return nil
	}
	c, problems := shape.VML(src)
	if len(problems) > 0 {
		d.warn(warn.ContentUnsupported, warn.Approximated, "a VML text path is drawn along its path in part: "+strings.Join(problems, "; "))
	}
	return &shape.TextWarp{Custom: c}
}

// maxTextPathLines caps the lines of a VML text path's string set along
// its path. Anti-DoS guard, not spec-mandated: WordArt holds a line or
// two.
const maxTextPathLines = 16

// warpTextPath returns the outlines of the lines of a VML text path's
// string set along the path warp fitted to r, as officefx.WarpText sets
// a body converted from WordArt, the lines stacked at their line
// distance; false when warp is nil or draws nothing. The string is not
// kept as text: Word writes none for a VML text path, whose shape's
// alternative text names it.
//
// # Corpus evidence
//
//   - litho/docx shapes-wordart: Word sets "Arched VML text", a shape of
//     type 144 (Arch Up Curve) whose path is one arc, along the arc.
//   - libreoffice/docx ooxmlexport/fdo74110: Word stretches the three
//     centred lines of a type 164 text path between its two curves.
//   - libreoffice/docx ooxmlimport/WordArt and ooxmlexport/fdo74110,
//     litho/docx shapes-wordart: Word's PDFs hold no text for their VML
//     text paths.
func warpTextPath(ctx context.Context, lines []text.Line, warp *shape.TextWarp, r typeset.Rect, outlines *text.Outlines) ([]typeset.PathOp, bool) {
	lanes := shape.WarpLanes(warp, r)
	if lanes == nil {
		return nil, false
	}
	var frags []typeset.Fragment
	y, width := 0.0, 0.0
	for i := range lines {
		if i < maxTextPathLines {
			width = max(width, lines[i].Width)
		}
	}
	for i := range lines {
		if i >= maxTextPathLines {
			break
		}
		l := &lines[i]
		// Each line is centred on the widest, as Word sets the lines of
		// a text path.
		x := (width - l.Width) / 2
		y += l.Ascent
		for _, t := range l.Runs {
			if t != nil {
				// The paints are the shape's; the run is drawn as its
				// outline and kept as text, which takes a colour.
				c := *t
				c.Color = typeset.Black
				frags = append(frags, &typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, E: x, F: y}, Opacity: 1, Content: []typeset.Fragment{&c}})
			}
		}
		y += l.Descent + l.LineGap
	}
	out, _, err := officefx.WarpText(ctx, frags, typeset.Identity, &officefx.Warp{Lanes: lanes, Fit: true, Block: width}, outlines)
	if err != nil {
		return nil, false
	}
	var ops []typeset.PathOp
	var walk func(fs []typeset.Fragment, m typeset.Matrix)
	walk = func(fs []typeset.Fragment, m typeset.Matrix) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				ops = append(ops, transformOps(v.Ops, m)...)
			case *typeset.Text:
				if !v.Invisible {
					ops = append(ops, transformOps(outlines.Run(nil, v), m)...)
				}
			case *typeset.Group:
				gt := v.Transform
				if gt == (typeset.Matrix{}) {
					gt = typeset.Identity
				}
				walk(v.Content, compose(m, gt))
			}
		}
	}
	walk(out, typeset.Identity)
	if len(ops) == 0 {
		return nil, false
	}
	return ops, true
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
			out[i].P[j] = typeset.Point{X: float64(m.A*p.X) + float64(m.C*p.Y) + m.E, Y: float64(m.B*p.X) + float64(m.D*p.Y) + m.F}
		}
	}
	return out
}
