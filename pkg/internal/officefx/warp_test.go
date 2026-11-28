package officefx

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// lanesOf returns the lanes of a warp of straight paths from (l, y) to
// (r, y), y each of ys, fitted to box.
func lanesOf(box typeset.Rect, ys ...string) []shape.WarpLane {
	var d shape.DrawingML
	for _, y := range ys {
		d.Path(0, 0, shape.FillNorm, true)
		d.Command(shape.CmdMove, "l", y)
		d.Command(shape.CmdLine, "r", y)
	}
	return shape.WarpLanes(&shape.TextWarp{Custom: d.Custom()}, box)
}

// twoGlyphs returns a run of two block glyphs of size points at x 10 on
// the baseline y 100.
func twoGlyphs(size float64) *typeset.Text {
	t := blockRun(size)
	t.Glyphs = []typeset.Glyph{{GID: 1, Text: "a"}, {GID: 1, X: 0.6 * size, Text: "a"}}
	return t
}

// warpedBounds returns the box of the points of every path of frags.
func warpedBounds(frags []typeset.Fragment) (x0, y0, x1, y1 float64, n int) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				n++
				for _, op := range v.Ops {
					for _, p := range op.P[:opPoints(op.Kind)] {
						x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
					}
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(frags)
	return
}

func TestWarpText_StretchesTheInkBetweenTwoPaths(t *testing.T) {
	box := typeset.Rect{X: 200, Y: 300, Width: 100, Height: 50}
	w := &Warp{Lanes: lanesOf(box, "t", "b")}
	run := twoGlyphs(10)
	out, notes, err := WarpText(context.Background(), []typeset.Fragment{run}, typeset.Matrix{A: 1, D: 1, E: 5, F: 7}, w, &text.Outlines{})
	if err != nil || notes != (WarpNotes{}) {
		t.Fatalf("err %v, notes %+v", err, notes)
	}
	x0, y0, x1, y1, n := warpedBounds(out)
	if n != 1 || math.Abs(x0-200) > 1e-6 || math.Abs(y0-300) > 1e-6 || math.Abs(x1-300) > 1e-6 || math.Abs(y1-350) > 1e-6 {
		t.Errorf("outlines %d, box (%v, %v)-(%v, %v), want one filling the box", n, x0, y0, x1, y1)
	}
	var hidden *typeset.Text
	for _, f := range out {
		if g, ok := f.(*typeset.Group); ok {
			for _, c := range g.Content {
				if tx, ok := c.(*typeset.Text); ok {
					hidden = tx
				}
			}
		}
	}
	if hidden == nil || !hidden.Invisible || len(hidden.Glyphs) != 2 || hidden.Glyphs[1].Text != "a" {
		t.Errorf("the run is not kept as invisible text: %+v", hidden)
	}
	if run.Invisible {
		t.Error("the laid-out run was changed in place")
	}
}

func TestWarpText_SetsGlyphsOnAPath(t *testing.T) {
	box := typeset.Rect{Width: 120, Height: 60}
	run := twoGlyphs(10)
	// The run advances 12 points; fitted, it spans the 120-point path.
	for _, fit := range []bool{false, true} {
		w := &Warp{Lanes: lanesOf(box, "vc"), Fit: fit, Block: 12}
		out, _, err := WarpText(context.Background(), []typeset.Fragment{run}, typeset.Matrix{A: 1, D: 1, E: -10, F: -100}, w, &text.Outlines{})
		if err != nil || len(out) != 2 {
			t.Fatalf("fit %v: %d fragments, err %v; want a group a glyph", fit, len(out), err)
		}
		k := 1.0
		if fit {
			k = 10
		}
		for i, f := range out {
			g := f.(*typeset.Group)
			if math.Abs(g.Transform.A-k) > 1e-9 || g.Transform.B != 0 {
				t.Errorf("fit %v glyph %d: transform %+v, want a scale of %v", fit, i, g.Transform, k)
			}
			tx := g.Content[0].(*typeset.Text)
			if len(tx.Glyphs) != 1 || tx.Invisible {
				t.Errorf("fit %v glyph %d: %d glyphs, invisible %v", fit, i, len(tx.Glyphs), tx.Invisible)
			}
			// The glyph's baseline lies pathOffset ems below the path.
			y := mapPoint(g.Transform, typeset.Point{X: 10, Y: 100}).Y
			if want := 30 + pathOffset*10*k; math.Abs(y-want) > 1e-9 {
				t.Errorf("fit %v glyph %d: baseline at %v, want %v", fit, i, y, want)
			}
		}
	}
}

func TestWarpText_DrawsUnwarpedPastItsBounds(t *testing.T) {
	box := typeset.Rect{Width: 100, Height: 50}
	run := blockRun(10)
	run.Glyphs = make([]typeset.Glyph, MaxWarpGlyphs+1)
	m := typeset.Matrix{A: 1, D: 1, E: 3}
	out, notes, err := WarpText(context.Background(), []typeset.Fragment{run}, m, &Warp{Lanes: lanesOf(box, "t", "b")}, &text.Outlines{})
	if err != nil || !notes.Capped || len(out) != 1 || out[0].(*typeset.Group).Transform != m {
		t.Errorf("err %v, notes %+v, %d fragments; want the text unwarped", err, notes, len(out))
	}
	// A turned text is not warped either.
	turned := typeset.Matrix{B: 1, C: -1}
	out, _, _ = WarpText(context.Background(), []typeset.Fragment{blockRun(10)}, turned, &Warp{Lanes: lanesOf(box, "t", "b")}, &text.Outlines{})
	if len(out) != 1 || out[0].(*typeset.Group).Transform != turned {
		t.Error("a turned text was warped")
	}
}

func TestWarpText_LeavesEffectBitmapsOutAndStopsWithTheContext(t *testing.T) {
	box := typeset.Rect{Width: 100, Height: 50}
	frags := []typeset.Fragment{&typeset.Image{Rect: typeset.Rect{Width: 1, Height: 1}}, blockRun(10)}
	out, notes, err := WarpText(context.Background(), frags, typeset.Identity, &Warp{Lanes: lanesOf(box, "t", "b")}, &text.Outlines{})
	if err != nil || !notes.Effects {
		t.Fatalf("err %v, notes %+v", err, notes)
	}
	for _, f := range out {
		if _, ok := f.(*typeset.Image); ok {
			t.Error("an effect bitmap was kept")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := WarpText(ctx, []typeset.Fragment{blockRun(10)}, typeset.Identity, &Warp{Lanes: lanesOf(box, "t", "b")}, &text.Outlines{}); err == nil {
		t.Error("a cancelled context warped the text")
	}
}

func TestWarpText_ShrinksALineLongerThanItsPath(t *testing.T) {
	run := twoGlyphs(10)
	// The run advances 12 points: on a 6-point path it shrinks by half,
	// on a 120-point path it keeps its size.
	for _, c := range []struct{ width, k float64 }{{6, 0.5}, {120, 1}} {
		w := &Warp{Lanes: lanesOf(typeset.Rect{Width: c.width, Height: 60}, "vc"), Block: 12}
		out, _, err := WarpText(context.Background(), []typeset.Fragment{run}, typeset.Matrix{A: 1, D: 1, E: -10, F: -100}, w, &text.Outlines{})
		if err != nil || len(out) != 2 {
			t.Fatalf("path %v: %d fragments, err %v", c.width, len(out), err)
		}
		if a := out[0].(*typeset.Group).Transform.A; math.Abs(a-c.k) > 1e-9 {
			t.Errorf("path %v: scale %v, want %v", c.width, a, c.k)
		}
	}
}
