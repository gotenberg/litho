package chart

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestRender_Funnel(t *testing.T) {
	c := &Chart{
		Axes: plainAxes(),
		Groups: []*Group{{Kind: Funnel, GapWidth: 100, XAxis: 0, YAxis: 1, Series: []*Series{{
			Values: Data{Values: nums(4, 1, 2), Cached: true},
			Fill:   &Paint{Color: red},
			Labels: Labels{ShowValue: true, Placement: LabelOutsideEnd, Style: TextStyle{Size: 8}},
		}}}},
	}
	c.Axes[0].Reverse = true
	frags := Render(context.Background(), ahem(t), c, chartRect)
	bars := paths(frags, red)
	if len(bars) != 3 {
		t.Fatalf("paints %d bars, want 3", len(bars))
	}
	a, b, d := bounds(bars[0]), bounds(bars[1]), bounds(bars[2])
	// The greatest is as wide as the plot area, the others in proportion,
	// all centred on it; the first at the top.
	if math.Abs(b.Width-a.Width/4) > 1e-6 || math.Abs(d.Width-a.Width/2) > 1e-6 {
		t.Errorf("bar widths %v %v %v, want 4 to 1 to 2", a.Width, b.Width, d.Width)
	}
	if ca, cb := a.X+a.Width/2, b.X+b.Width/2; math.Abs(ca-cb) > 1e-6 || a.Y >= b.Y || b.Y >= d.Y {
		t.Errorf("bars %+v %+v %+v, want them centred, from the top down", a, b, d)
	}
	// Half a category high at gapWidth 100.
	if catH := (d.Y + d.Height/2 - (a.Y + a.Height/2)) / 2; math.Abs(a.Height-catH/2) > 1e-6 {
		t.Errorf("bars %v high, want half the category, %v", a.Height, catH/2)
	}
	// The labels sit in the bars' middles, whatever their position says.
	got := texts(frags)
	if len(got) != 3 {
		t.Fatalf("labels %q, want 3", got)
	}
}

func TestRender_FunnelLabelOverNoFill(t *testing.T) {
	for _, none := range []bool{false, true} {
		c := &Chart{
			Axes: plainAxes(),
			Groups: []*Group{{Kind: Funnel, XAxis: 0, YAxis: 1, Series: []*Series{{
				Values: Data{Values: nums(4), Cached: true},
				Fill:   &Paint{Color: red, None: none},
				Labels: Labels{ShowValue: true, Style: TextStyle{Size: 8, Color: blue}},
			}}}},
		}
		want := onFillLabelInk
		if none {
			want = unfilledLabelInk
		}
		walk(Render(context.Background(), ahem(t), c, chartRect), 0, func(fr typeset.Fragment, _ int) {
			if tx, ok := fr.(*typeset.Text); ok && !near(tx.Color, want) {
				t.Errorf("unfilled %v: label in %v, want %v", none, tx.Color, want)
			}
		})
	}
}
