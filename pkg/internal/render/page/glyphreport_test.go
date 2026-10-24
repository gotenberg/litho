package page

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

func TestGlyphReportsOriginAndPlaceholderBounds(t *testing.T) {
	t.Parallel()
	// Helvetica is not embedded and no substitute is served: A and B draw as
	// placeholder boxes from 0.25 em below the baseline to 0.75 em above it,
	// as wide as their advances, 667 thousandths of 20 points each.
	f := fixture{
		page:    textPage,
		content: "0 g BT /F 20 Tf 10 50 Td (A B) Tj ET",
		objs:    []string{"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"},
	}
	doc, pg := open(t, f.bytes(), limits.Limits{})
	type shown struct {
		origin geom.Point
		bounds geom.Rect
	}
	var got []shown
	opts := f.options()
	opts.Glyph = func(origin geom.Point, bounds geom.Rect) {
		got = append(got, shown{origin, bounds})
	}
	if _, _, err := Render(context.Background(), doc, pg, opts); err != nil {
		t.Fatalf("Render: %v", err)
	}
	// The space draws nothing, so it is not reported.
	if len(got) != 2 {
		t.Fatalf("got %d glyphs, want 2: %v", len(got), got)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	// The page is 100 points high: user y 50 is device row 50. B follows A
	// and the space, 278 thousandths wide.
	wantX := [2]float64{10, 10 + 13.34 + 0.278*20}
	for i, g := range got {
		if !near(g.origin.Y, 50) || !near(g.bounds.Min.Y, 35) || !near(g.bounds.Max.Y, 55) {
			t.Errorf("glyph %d: origin %v bounds %v, want the baseline at 50 and the box from 35 to 55", i, g.origin, g.bounds)
		}
		if !near(g.bounds.Max.X-g.bounds.Min.X, 13.34) || !near(g.bounds.Min.X, g.origin.X) {
			t.Errorf("glyph %d: bounds %v, want 13.34 wide from the origin %v", i, g.bounds, g.origin)
		}
	}
	if !near(got[0].origin.X, wantX[0]) || !near(got[1].origin.X, wantX[1]) {
		t.Errorf("origins %v and %v, want x %v", got[0].origin, got[1].origin, wantX)
	}
}

func TestNoTransferLeavesTransferFunctionsOut(t *testing.T) {
	t.Parallel()
	f := fixture{
		page:    "/Resources << /ExtGState << /Inv << /TR 5 0 R >> >> >>",
		content: "q /Inv gs 0 g 0 0 20 20 re f Q",
		objs:    []string{invert},
	}
	doc, pg := open(t, f.bytes(), limits.Limits{})
	opts := f.options()
	opts.NoTransfer = true
	c, _, err := Render(context.Background(), doc, pg, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Without the option the inverting transfer paints the square white
	// (TestTransferFunctions).
	expect(t, c.Image(), 1, map[[2]int]rgb{{10, 90}: black})
}
