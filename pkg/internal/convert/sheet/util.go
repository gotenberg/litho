package sheet

import (
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// strconvFloat writes v compactly.
func strconvFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}

// positive reports v > 0: false for zero, a negative number and NaN, so
// that a length the reader leaves unset or not a number takes its
// default.
func positive(v float64) bool { return v > 0 }

// ordered reports a <= b: false when either is NaN.
func ordered(a, b float64) bool { return a <= b }

// colorOf returns c as a composition colour.
func colorOf(c css.Color) compose.Color {
	return compose.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// rectOps returns the path of the rectangle x, y, w, h.
func rectOps(x, y, w, h float64) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x, Y: y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x + w, Y: y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x + w, Y: y + h}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x, Y: y + h}}},
		{Kind: compose.ClosePath},
	}
}

// lineOps returns the path of the segment from (x1, y1) to (x2, y2).
func lineOps(x1, y1, x2, y2 float64) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x1, Y: y1}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x2, Y: y2}}},
	}
}
