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
