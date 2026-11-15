package chart

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// hierarchyChart returns a chart of kind k over the probe's six leaves in
// three parents, coloured red, blue and green in the data's order.
func hierarchyChart(k Kind, banner bool) *Chart {
	return &Chart{
		Rules:   OfficeRules,
		Palette: []typeset.Color{red, blue, {G: 1, A: 1}},
		Groups: []*Group{{Kind: k, Series: []*Series{{
			Values:     Data{Values: nums(30, 20, 25, 10, 40, 15), Cached: true},
			Categories: Data{Values: strs("Apples", "Pears", "Carrots", "Leeks", "Wheat", "Rye"), Cached: true},
			Hierarchy: &Hierarchy{
				Parents:   []Data{{Values: strs("Fruit", "Fruit", "Veg", "Veg", "Grain", "Grain"), Cached: true}},
				Banner:    banner,
				LineWidth: 0.75,
			},
		}}}},
	}
}

// TestSquarify_ProbeTreemap holds the treemap's squarified layout to the
// cells Word drew for the probe: the parents as columns from the left,
// the largest first, each split from the top down.
//
// # Corpus evidence
//
//   - probe, docx: Grain, Fruit and Veg 151.0, 137.25 and 96.0 pt wide in
//     a plot area 384.26 by 154.75 pt; 40 over 15 in Grain, 112.75 and
//     42.0 pt high; under an 18.75 pt banner, 40 left of 15 in a cell
//     151 by 136 pt, 109.75 and 41.25 pt wide.
func TestSquarify_ProbeTreemap(t *testing.T) {
	ns := func(vs ...float64) []*node {
		var out []*node
		for _, v := range vs {
			out = append(out, &node{value: v})
		}
		return out
	}
	var got []typeset.Rect
	squarify(ns(55, 50, 35), typeset.Rect{Width: 384.26, Height: 154.75}, func(_ *node, r typeset.Rect) { got = append(got, r) })
	for i, w := range []float64{150.96, 137.24, 96.06} {
		if math.Abs(got[i].Width-w) > 0.05 || math.Abs(got[i].Height-154.75) > 1e-9 {
			t.Errorf("parent %d: cell %+v, want a column %v wide", i, got[i], w)
		}
	}
	got = nil
	squarify(ns(40, 15), typeset.Rect{Width: 151, Height: 154.75}, func(_ *node, r typeset.Rect) { got = append(got, r) })
	if math.Abs(got[0].Height-112.55) > 0.05 || got[0].Width != 151 || got[1].Y <= got[0].Y {
		t.Errorf("leaves %+v, want 40 over 15, 112.55 pt high", got)
	}
	got = nil
	squarify(ns(40, 15), typeset.Rect{Width: 151, Height: 136}, func(_ *node, r typeset.Rect) { got = append(got, r) })
	if math.Abs(got[0].Width-109.82) > 0.05 || got[0].Height != 136 || got[1].X <= got[0].X {
		t.Errorf("leaves %+v, want 40 left of 15, 109.82 pt wide", got)
	}
}

// TestRender_Treemap holds a treemap to its rules: every leaf filled in
// its top-level parent's colour and outlined in it, the cells filling the
// plot area, a banner lowering each parent's cells, and the legend
// listing the parents.
func TestRender_Treemap(t *testing.T) {
	r := typeset.Rect{Width: 400, Height: 200}
	area := func(ps []*typeset.Path) float64 {
		a := 0.0
		for _, p := range ps {
			b := bounds(p)
			a += b.Width * b.Height
		}
		return a
	}
	frags := Render(context.Background(), ahem(t), hierarchyChart(Treemap, false), r)
	reds, blues := paths(frags, red), paths(frags, blue)
	if len(reds) != 2 || len(blues) != 2 {
		t.Fatalf("%d red and %d blue cells, want two of each", len(reds), len(blues))
	}
	for _, p := range reds {
		if p.Stroke == nil || !near(p.Stroke.Color, red) || p.Stroke.Width != 0.75 {
			t.Errorf("cell stroke %+v, want 0.75 pt in its fill", p.Stroke)
		}
	}
	// Fruit holds 50 of 140, Veg 35.
	if ratio := area(reds) / area(blues); math.Abs(ratio-50.0/35) > 1e-6 {
		t.Errorf("fruit to veg area %v, want %v", ratio, 50.0/35)
	}
	top := func(c *Chart) float64 {
		y := math.Inf(1)
		for _, p := range paths(Render(context.Background(), ahem(t), c, r), blue) {
			y = math.Min(y, bounds(p).Y)
		}
		return y
	}
	if d := top(hierarchyChart(Treemap, true)) - top(hierarchyChart(Treemap, false)); math.Abs(d-treemapBanner) > 1e-6 {
		t.Errorf("banner lowers the cells by %v, want %v", d, treemapBanner)
	}
	c := hierarchyChart(Treemap, false)
	c.Legend = &Legend{Position: LegendTop, Style: TextStyle{Size: 9}}
	got := texts(Render(context.Background(), ahem(t), c, r))
	if len(got) != 3 || got[0] != "Fruit" || got[1] != "Veg" || got[2] != "Grain" {
		t.Errorf("legend %q, want the parents in the data's order", got)
	}
}

// TestRender_Sunburst holds a sunburst to its rules: a hole and a ring a
// level, each as wide as the hole's radius, the outer radius half the
// plot area's lesser side; the parents clockwise from twelve o'clock, the
// largest first; leaf labels white, along the radius, reading from the
// left.
func TestRender_Sunburst(t *testing.T) {
	c := hierarchyChart(Sunburst, false)
	c.Groups[0].Series[0].Labels = Labels{ShowValue: true, Style: TextStyle{Size: 9, Color: typeset.Black}}
	r := typeset.Rect{Width: 300, Height: 240}
	frags := Render(context.Background(), ahem(t), c, r)
	greens := paths(frags, typeset.Color{G: 1, A: 1})
	if len(greens) != 3 {
		t.Fatalf("%d green sectors, want Grain and its two leaves", len(greens))
	}
	// Grain, the largest, starts at twelve o'clock and runs clockwise to
	// 141.43 degrees: its sectors lie right of the centre line.
	var outer typeset.Rect
	for i, p := range greens {
		b := bounds(p)
		if i == 0 || b.Width*b.Height > outer.Width*outer.Height {
			outer = b
		}
	}
	cx := r.Width / 2
	if math.Abs(outer.X-cx) > 0.5 {
		t.Errorf("grain's outer sector starts at x %v, want the centre %v", outer.X, cx)
	}
	var turns []float64
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if g, ok := fr.(*typeset.Group); ok && g.Transform.B != 0 {
			white := false
			walk(g.Content, 0, func(f typeset.Fragment, _ int) {
				if tx, ok := f.(*typeset.Text); ok && near(tx.Color, onFillLabelInk) {
					white = true
				}
			})
			if white {
				turns = append(turns, math.Atan2(-g.Transform.B, g.Transform.A)*180/math.Pi)
			}
		}
	})
	if len(turns) != 6 {
		t.Fatalf("%d turned white labels, want six", len(turns))
	}
	// Over sectors the style leaves unfilled, the labels are grey.
	c.Groups[0].Series[0].Fill = &Paint{None: true}
	grey := 0
	walk(Render(context.Background(), ahem(t), c, r), 0, func(fr typeset.Fragment, _ int) {
		if tx, ok := fr.(*typeset.Text); ok && near(tx.Color, unfilledLabelInk) {
			grey++
		}
	})
	if grey == 0 {
		t.Errorf("no grey label over unfilled sectors")
	}
	// Wheat, 40, centred at 51.43 degrees, reads up the radius at 38.57
	// degrees; Pears, 20, at 244.29, reads inward at 25.71.
	want := []float64{38.57, -32.14, -90, 25.71, -32.14, -77.14}
	for i, w := range want {
		if math.Abs(turns[i]-w) > 0.05 {
			t.Errorf("label %d turned %v, want %v", i, turns[i], w)
		}
	}
}
