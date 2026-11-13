package officefx

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestPlanGrid checks the grid of an effect: the engine's
// resolution, lowered so a morphology's distance is whole pixels, the box
// widened to whole pixels, and reported when the surface cap reduces it.
func TestPlanGrid(t *testing.T) {
	g, reduced, ok := PlanGrid(typeset.Rect{X: 10, Y: 20, Width: 100.3, Height: 50}, 3, 7.5)
	if !ok || reduced {
		t.Fatalf("PlanGrid = %v, %v, want a grid unreduced", ok, reduced)
	}
	if n := 7.5 * g.Unit; math.Abs(n-math.Round(n)) > 1e-9 || g.Unit > 5.0/3 {
		t.Errorf("unit %v puts 7.5 pt on %v pixels, want a whole number at most the rule's", g.Unit, n)
	}
	if math.Abs(g.Box.Width*g.Unit-float64(g.W)) > 1e-9 || g.Box.Width < 100.3 || g.Box.X != 10 {
		t.Errorf("box %+v of %d pixels, want the box widened to whole pixels", g.Box, g.W)
	}
	g, reduced, ok = PlanGrid(typeset.Rect{Width: 1200, Height: 1200}, 0.1, 0)
	if !ok || !reduced || int64(g.W)*int64(g.H) > effect.MaxPixels {
		t.Errorf("PlanGrid of a large sharp effect = %d by %d, %v, %v, want reduced within the cap", g.W, g.H, reduced, ok)
	}
}

// TestPlanGrid_LastBitsTakeTheSamePixels checks that boxes whose sides
// differ in their last bits, as two machines compute one box, take the
// same pixels: a 99.6 point side at 300 pixels an inch is 415 pixels,
// which one bit more must not round up to 416.
//
// # Corpus evidence
//
//   - litho/docx: shapes-effects-3d's blurred rounded rectangle, 99.6 pt
//     wide with its blur, drew a bitmap 415 pixels wide on linux/amd64 and
//     416 on linux/arm64 and darwin/arm64.
func TestPlanGrid_LastBitsTakeTheSamePixels(t *testing.T) {
	for _, w := range []float64{99.6, math.Nextafter(99.6, 100), math.Nextafter(math.Nextafter(99.6, 100), 100), math.Nextafter(99.6, 99)} {
		g, _, ok := PlanGrid(typeset.Rect{Width: w, Height: 70.8}, 1, 0)
		if !ok || g.W != 415 {
			t.Errorf("PlanGrid of a side of %v points = %d pixels, %v, want 415", w, g.W, ok)
		}
	}
}
