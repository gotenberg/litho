package chart

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestWaterfallSpans(t *testing.T) {
	nan := math.NaN()
	lo, hi := waterfallSpans([]float64{100, 45, -12, nan, -30, 95}, []int{0, 5})
	wantLo := []float64{nan, 100, 145, nan, 133, nan}
	wantHi := []float64{100, 145, 133, nan, 103, 95}
	same := func(a, b float64) bool { return a == b || math.IsNaN(a) && math.IsNaN(b) }
	for j := range wantLo {
		if !same(lo[j], wantLo[j]) || !same(hi[j], wantHi[j]) {
			t.Errorf("point %d spans %v to %v, want %v to %v", j, lo[j], hi[j], wantLo[j], wantHi[j])
		}
	}
}

// waterfallChart returns a waterfall of values whose first and last points
// are totals, with connectors of colour ink.
func waterfallChart(ink typeset.Color, values ...float64) *Chart {
	green := typeset.Color{G: 1, A: 1}
	return &Chart{
		Axes:    plainAxes(),
		Palette: []typeset.Color{red, blue, green},
		Groups: []*Group{{Kind: Waterfall, GapWidth: 50, XAxis: 0, YAxis: 1, Series: []*Series{{
			Name:      "S",
			Values:    Data{Values: nums(values...), Cached: true},
			Subtotals: []int{0, len(values) - 1},
			Connector: &Stroke{Color: ink, Width: 0.75},
		}}}},
		Legend: &Legend{Position: LegendTop, Style: TextStyle{Size: 10}},
	}
}

func TestRender_Waterfall(t *testing.T) {
	ink := typeset.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	frags := Render(context.Background(), ahem(t), waterfallChart(ink, 100, 50, -30, 120), chartRect)
	green := typeset.Color{G: 1, A: 1}
	// Bars: the increase in the first automatic colour, the decrease in
	// the second, the totals in the third; each legend key too.
	inc, dec, tot := paths(frags, red), paths(frags, blue), paths(frags, green)
	if len(inc) != 2 || len(dec) != 2 || len(tot) != 3 {
		t.Fatalf("paints %d increase, %d decrease, %d total paths, want 2, 2 and 3 (bars and keys)", len(inc), len(dec), len(tot))
	}
	start, up, down := bounds(tot[1]), bounds(inc[1]), bounds(dec[1])
	end := bounds(tot[2])
	base := start.Y + start.Height
	if math.Abs(end.Y+end.Height-base) > 1e-6 {
		t.Errorf("the total bars stand at %v and %v, want one axis", base, end.Y+end.Height)
	}
	// The increase starts at the first total's top and rises half as high;
	// the decrease hangs from the increase's top.
	if math.Abs(up.Y+up.Height-start.Y) > 1e-6 || math.Abs(up.Height-start.Height/2) > 1e-6 {
		t.Errorf("increase %+v, want it on the first total %+v, half its height", up, start)
	}
	if math.Abs(down.Y-up.Y) > 1e-6 || math.Abs(down.Height-float64(start.Height*0.3)) > 1e-6 {
		t.Errorf("decrease %+v, want it hanging from %v, 0.3 of the first total high", down, up.Y)
	}
	// Three connectors, each at the end of a column and as long as the
	// gap: half a column at GapWidth 50.
	var lines []typeset.Rect
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if p, ok := fr.(*typeset.Path); ok && p.Stroke != nil && near(p.Stroke.Color, ink) {
			lines = append(lines, bounds(p))
		}
	})
	if len(lines) != 3 {
		t.Fatalf("draws %d connectors, want 3", len(lines))
	}
	if l := lines[0]; math.Abs(l.Y-start.Y) > 1e-6 || math.Abs(l.X-(start.X+start.Width)) > 1e-6 || math.Abs(l.Width-start.Width/2) > 1e-6 {
		t.Errorf("first connector %+v, want it from the first total's top right corner, %v long", l, start.Width/2)
	}
	if l := lines[2]; math.Abs(l.Y-end.Y) > 1e-6 {
		t.Errorf("last connector at %v, want at the last total's top %v", l.Y, end.Y)
	}
	if got := strings.Join(texts(frags), "|"); !slices.Equal(strings.Split(got, "|"), []string{"Increase", "Decrease", "Total"}) {
		t.Errorf("legend %q, want the three kinds of points", got)
	}
}

func TestRender_WaterfallWithoutConnectors(t *testing.T) {
	ink := typeset.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	c := waterfallChart(ink, 10, 5, 15)
	c.Groups[0].Series[0].Connector = nil
	n := 0
	walk(Render(context.Background(), ahem(t), c, chartRect), 0, func(fr typeset.Fragment, _ int) {
		if p, ok := fr.(*typeset.Path); ok && p.Stroke != nil && near(p.Stroke.Color, ink) {
			n++
		}
	})
	if n != 0 {
		t.Errorf("draws %d connectors, want none", n)
	}
}
