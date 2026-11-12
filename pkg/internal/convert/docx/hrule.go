package docx

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// The edges Word draws along a shaded horizontal line, a measurement of
// Word: a 0.25-point line of A0A0A0 along its top and one of E3E3E3 along
// its bottom, both 0.28 points in from its left end.
//
// # Corpus evidence
//
//   - litho/docx shapes-horizontal-rules: Word prints the standard line
//     and the 4-point red one each as a bar of their colour with a 0.627
//     grey edge at the top and a 0.89 grey edge at the bottom, 0.25 point
//     each, and the line marked o:hrnoshade as its bar alone.
const (
	hrEdge      = 0.25
	hrEdgeInset = 0.28
)

var (
	hrTopEdge    = typeset.Color{R: 0xA0 / 255.0, G: 0xA0 / 255.0, B: 0xA0 / 255.0, A: 1}
	hrBottomEdge = typeset.Color{R: 0xE3 / 255.0, G: 0xE3 / 255.0, B: 0xE3 / 255.0, A: 1}
)

// vmlHorizontalRule builds a VML horizontal line (a shape marked o:hr,
// MS-OI29500 2.1.1394; the Office namespace of ECMA-376 Part 4, 19.1): a
// bar of the shape's fill colour, as tall as its height and as wide as
// its width, or as o:hrpct tenths of a percent of the column when its
// width is 0 (100% for none), shaded with Word's edges unless
// o:hrnoshade. nil when k is no horizontal line or draws a picture.
func (d *document) vmlHorizontalRule(k *ooxml.Element, st *css.Style, w, h float64) *layout.Node {
	if v, _ := k.AttrNS(ooxml.NSO, "hr"); !vmlTrue(v) || k.Child(ooxml.NSV, "imagedata") != nil {
		return nil
	}
	ps := inlineStyle(st)
	ps.Height = css.Pts(max(h, 0.1))
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	ps.Width = css.Pts(max(w, 0.1))
	if w <= 0 {
		pct := 1.0
		if v, ok := k.AttrNS(ooxml.NSO, "hrpct"); ok {
			if n, ok := ooxml.Int(v); ok && n > 0 {
				pct = float64(min(n, 1000)) / 1000
			}
		}
		ps.Width = css.Dimension{Kind: css.Percent, Value: pct}
	}
	fill := typeset.Color{R: 0xA0 / 255.0, G: 0xA0 / 255.0, B: 0xA0 / 255.0, A: 1}
	if ss := vmlShapeStyle(k); ss.Fill != nil {
		fill = typeset.Color{R: ss.Fill.R, G: ss.Fill.G, B: ss.Fill.B, A: ss.Fill.A}
	}
	noShade, _ := k.AttrNS(ooxml.NSO, "hrnoshade")
	shaded := !vmlTrue(noShade)
	pic := &layout.Picture{Width: max(w, 1), Height: max(h, 0.1), NoNaturalWidth: w <= 0}
	pic.Vector = func(_ context.Context, r typeset.Rect) []typeset.Fragment {
		bar := func(x, y, w, h float64, c typeset.Color) typeset.Fragment {
			return &typeset.Path{Ops: rectOps(x, y, w, h), Fill: &typeset.Fill{Color: c}}
		}
		out := []typeset.Fragment{bar(r.X, r.Y, r.Width, r.Height, fill)}
		if shaded && r.Width > 2*hrEdgeInset && r.Height > 2*hrEdge {
			out = append(out,
				bar(r.X+hrEdgeInset, r.Y, r.Width-hrEdgeInset, hrEdge, hrTopEdge),
				bar(r.X+hrEdgeInset, r.Y+r.Height-hrEdge, r.Width-hrEdgeInset, hrEdge, hrBottomEdge))
		}
		return out
	}
	return &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic}
}

// vmlTrue reads a VML boolean that is false when absent (Part 4,
// 14.1.2.19 ST_TrueFalse).
func vmlTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "t", "true", "1", "on":
		return true
	}
	return false
}

// rectOps returns the outline of the rectangle x, y, w, h.
func rectOps(x, y, w, h float64) []typeset.PathOp {
	pt := func(x, y float64) [3]typeset.Point { return [3]typeset.Point{{X: x, Y: y}} }
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: pt(x, y)}, {Kind: typeset.LineTo, P: pt(x+w, y)},
		{Kind: typeset.LineTo, P: pt(x+w, y+h)}, {Kind: typeset.LineTo, P: pt(x, y+h)}, {Kind: typeset.ClosePath},
	}
}
