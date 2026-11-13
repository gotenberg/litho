package effect

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func pt3(x, y float64) [3]typeset.Point { return [3]typeset.Point{{X: x, Y: y}} }

// rect returns the ops of a rectangle, wound clockwise on screen.
func rect(x0, y0, x1, y1 float64) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: pt3(x0, y0)},
		{Kind: typeset.LineTo, P: pt3(x1, y0)},
		{Kind: typeset.LineTo, P: pt3(x1, y1)},
		{Kind: typeset.LineTo, P: pt3(x0, y1)},
		{Kind: typeset.ClosePath},
	}
}

// reverse returns rect's ops wound the other way.
func reverse(ops []typeset.PathOp) []typeset.PathOp {
	return []typeset.PathOp{ops[0], ops[3], ops[2], ops[1], ops[4]}
}

// ellipse returns the ops of an ellipse of centre (cx, cy) and radii rx,
// ry by four cubics. Each product is rounded on its own, so the points
// are the same on every machine and the determinism tests may draw it.
func ellipse(cx, cy, rx, ry float64) []typeset.PathOp {
	const k = 0.5522847498307936
	kx, ky := float64(k*rx), float64(k*ry)
	c := func(x1, y1, x2, y2, x3, y3 float64) typeset.PathOp {
		return typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{{X: x1, Y: y1}, {X: x2, Y: y2}, {X: x3, Y: y3}}}
	}
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: pt3(cx+rx, cy)},
		c(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry),
		c(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy),
		c(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry),
		c(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy),
		{Kind: typeset.ClosePath},
	}
}

// star returns the ops of a five-pointed star of centre (cx, cy) and
// outer radius r, its points joined in drawing order so its centre
// winds twice; the same on every machine, as [ellipse].
func star(cx, cy, r float64) []typeset.PathOp {
	ops := make([]typeset.PathOp, 0, 6)
	for i := range 5 {
		s, c := detmath.SincosDegrees(float64(i*144 - 90))
		k := typeset.LineTo
		if i == 0 {
			k = typeset.MoveTo
		}
		ops = append(ops, typeset.PathOp{Kind: k, P: pt3(cx+float64(r*c), cy+float64(r*s))})
	}
	return append(ops, typeset.PathOp{Kind: typeset.ClosePath})
}

// newSurface returns a surface of format f, failing the test on error.
func newSurface(t testing.TB, f Format, w, h int) *Surface {
	t.Helper()
	s, err := New(context.Background(), f, w, h, Grid{Scale: 1})
	if err != nil {
		t.Fatalf("New(%d, %d, %d): %v", f, w, h, err)
	}
	return s
}

// filled returns a coverage surface of w by h pixels covered by paths.
func filled(t testing.TB, w, h int, tr typeset.Matrix, paths ...Path) *Surface {
	t.Helper()
	s := newSurface(t, Coverage, w, h)
	if err := Fill(context.Background(), s, paths, tr); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	return s
}
