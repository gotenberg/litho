package sheet

import (
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// strconvFloat writes v compactly.
func strconvFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}

// ordered reports a <= b: false when either is NaN.
func ordered(a, b float64) bool { return a <= b }

// colorOf returns c as a composition colour.
func colorOf(c css.Color) typeset.Color {
	return typeset.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// rectOps returns the path of the rectangle x, y, w, h.
func rectOps(x, y, w, h float64) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x, Y: y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x + w, Y: y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x + w, Y: y + h}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x, Y: y + h}}},
		{Kind: typeset.ClosePath},
	}
}

// lineOps returns the path of the segment from (x1, y1) to (x2, y2).
func lineOps(x1, y1, x2, y2 float64) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x1, Y: y1}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x2, Y: y2}}},
	}
}
