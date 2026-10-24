package blend

import (
	"context"
	"errors"
	"image"
	"math"
	"testing"
)

// spotPage returns a 1×1 CMYK page group with spot planes.
func spotPage(t *testing.T) *Group {
	t.Helper()
	g, err := NewPageIn(1, 1, NewSpace(CMYK, "cmyk", nil), testBudget)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.EnableSpots(testBudget); err != nil {
		t.Fatal(err)
	}
	return g
}

func paintSpot(t *testing.T, g *Group, cs Components, p Params) {
	t.Helper()
	if err := g.PaintCoverage(context.Background(), image.Rect(0, 0, 1, 1), []float32{1}, cs, p); err != nil {
		t.Fatal(err)
	}
}

func near(a, b Components) bool {
	for k := range a {
		if math.Abs(float64(a[k]-b[k])) > 1e-4 {
			return false
		}
	}
	return true
}

func TestSpotPlanesComposite(t *testing.T) {
	// A spot ink overprinting cyan (ISO 32000-1, 8.6.7) leaves it, and its
	// plane prints through its alternate once composited (8.6.6.4).
	gold := SpotTable{{0, 0, 0, 0}, {0, 0.2, 1, 0}}
	g := spotPage(t)
	paintSpot(t, g, Components{1, 0, 0, 0}, Params{})
	paintSpot(t, g, Components{}, Params{Keep: allProcessBits, Spots: Components{0.5}})
	if err := g.CompositeSpots(context.Background(), []SpotTable{gold}); err != nil {
		t.Fatal(err)
	}
	if g.HasSpots() {
		t.Error("planes kept after compositing")
	}
	if c, a := g.Components(0, 0); a != 1 || !near(c, Components{1, 0.1, 0.5, 0}) {
		t.Errorf("composited %v at %v, want cyan with half gold", c, a)
	}

	// Knocking out, the next painting clears the spot plane.
	g = spotPage(t)
	paintSpot(t, g, Components{}, Params{Keep: allProcessBits, Spots: Components{1}})
	paintSpot(t, g, Components{0, 1, 0, 0}, Params{})
	if err := g.CompositeSpots(context.Background(), []SpotTable{gold}); err != nil {
		t.Fatal(err)
	}
	if c, _ := g.Components(0, 0); !near(c, Components{0, 1, 0, 0}) {
		t.Errorf("knocked out: %v, want magenta", c)
	}

	// Ink sums clip to full ink.
	g = spotPage(t)
	paintSpot(t, g, Components{0, 1, 0, 0}, Params{})
	paintSpot(t, g, Components{}, Params{Keep: allProcessBits | SpotBit(1), Spots: Components{1}})
	paintSpot(t, g, Components{}, Params{Keep: allProcessBits | SpotBit(0), Spots: Components{0, 1}})
	if err := g.CompositeSpots(context.Background(), []SpotTable{gold, gold}); err != nil {
		t.Fatal(err)
	}
	if c, _ := g.Components(0, 0); !near(c, Components{0, 1, 1, 0}) {
		t.Errorf("clipped: %v", c)
	}
}

func TestEnableSpotsRefusesOtherGroups(t *testing.T) {
	rgb, err := NewPage(1, 1, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	if err := rgb.EnableSpots(testBudget); !errors.Is(err, ErrNotCMYK) {
		t.Errorf("sRGB group: %v, want ErrNotCMYK", err)
	}
	if rgb.CompositeSpots(context.Background(), nil) != nil || rgb.HasSpots() {
		t.Error("a group without planes composites nothing")
	}
}

// allProcessBits leaves every process component.
const allProcessBits = 0x0f
