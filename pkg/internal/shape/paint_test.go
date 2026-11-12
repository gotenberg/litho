package shape

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestPresetDash(t *testing.T) {
	tests := []struct {
		name  string
		width float64
		cap   typeset.LineCap
		want  []float64
	}{
		{"dot", 2, typeset.ButtCap, []float64{2, 6}},
		{"lgDashDotDot", 1, typeset.ButtCap, []float64{8, 3, 1, 3, 1, 3}},
		{"sysDash", 3, typeset.ButtCap, []float64{9, 3}},
		{"dash", 1, typeset.RoundCap, []float64{3.01, 3.99}},
		{"solid", 1, typeset.ButtCap, nil},
		{"nonsense", 1, typeset.ButtCap, nil},
	}
	for _, tt := range tests {
		got := PresetDash(tt.name, tt.width, tt.cap)
		if len(got) != len(tt.want) {
			t.Fatalf("%s: %v, want %v", tt.name, got, tt.want)
		}
		for i := range got {
			if math.Abs(got[i]-tt.want[i]) > 1e-9 {
				t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
			}
		}
	}
}

// line returns a horizontal line from (0, 0) to (100, 0).
func line() []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: 0, Y: 0}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 100, Y: 0}}},
	}
}

// TestStroke_Arrowheads checks a triangle at the end of a 2 pt line: the
// tip at the end, the base 6 pt back and 6 pt wide (medium: 3 line
// widths), the line stopping a fifteenth of the width short of the base;
// and a diamond centred on the start, the line stopping short of its
// centre.
func TestStroke_Arrowheads(t *testing.T) {
	frags := Stroke(line(), Line{
		Width: 2, Color: typeset.Color{A: 1},
		Head: Arrowhead{Kind: ArrowDiamond}, Tail: Arrowhead{Kind: ArrowTriangle},
	})
	if len(frags) != 3 {
		t.Fatalf("%d fragments", len(frags))
	}
	body := frags[0].(*typeset.Path)
	if got := body.Ops[1].P[0].X; math.Abs(got-(94+0.4)) > 1e-9 {
		t.Errorf("the line ends at %g, want 94.4", got)
	}
	if got := body.Ops[0].P[0].X; math.Abs(got-(3-0.4)) > 1e-9 {
		t.Errorf("the line starts at %g, want 2.6", got)
	}
	tri := frags[2].(*typeset.Path)
	want := []typeset.Point{{X: 100, Y: 0}, {X: 94, Y: 3}, {X: 94, Y: -3}}
	for i, w := range want {
		if p := tri.Ops[i].P[0]; math.Abs(p.X-w.X) > 1e-9 || math.Abs(p.Y-w.Y) > 1e-9 {
			t.Errorf("triangle point %d: %v, want %v", i, p, w)
		}
	}
	dia := frags[1].(*typeset.Path)
	if p := dia.Ops[0].P[0]; math.Abs(p.X+3) > 1e-9 {
		t.Errorf("the diamond's tip %v, want 3 pt past the start", p)
	}
	// A thin line's arrowhead is sized from 0.7 mm.
	thin := Stroke(line(), Line{Width: 0.5, Tail: Arrowhead{Kind: ArrowTriangle, Width: ArrowLarge, Length: ArrowSmall}})
	tri = thin[1].(*typeset.Path)
	base := 70.0 * 72 / 2540
	if p := tri.Ops[1].P[0]; math.Abs(p.X-(100-2*base)) > 1e-9 || math.Abs(p.Y-2.5*base) > 1e-9 {
		t.Errorf("thin triangle %v", p)
	}
	// A closed outline has none.
	closed := append(line(), typeset.PathOp{Kind: typeset.ClosePath})
	if got := Stroke(closed, Line{Width: 1, Tail: Arrowhead{Kind: ArrowOval}}); len(got) != 1 {
		t.Errorf("closed outline: %d fragments", len(got))
	}
	if Stroke(line(), Line{}) != nil {
		t.Error("a zero width stroked")
	}
}

func TestFill_Gradient(t *testing.T) {
	box := typeset.Rect{X: 0, Y: 0, Width: 200, Height: 100}
	stops := []typeset.ColorStop{{Offset: 0.8, Color: typeset.Color{B: 1, A: 1}}, {Offset: 0.2, Color: typeset.Color{R: 1, A: 1}}}
	f := Fill(RectOps(box), box, Paint{Kind: PaintGradient, Stops: stops, Angle: 90})
	sh := f[0].(*typeset.Path).Fill.Shading
	if sh.Kind != typeset.Axial || math.Abs(sh.From.X-100) > 1e-9 || math.Abs(sh.From.Y) > 1e-9 || math.Abs(sh.To.Y-100) > 1e-9 {
		t.Errorf("axial %v to %v", sh.From, sh.To)
	}
	if len(sh.Stops) != 4 || sh.Stops[0].Offset != 0 || sh.Stops[0].Color.R != 1 || sh.Stops[3].Offset != 1 || sh.Stops[3].Color.B != 1 {
		t.Errorf("stops %v", sh.Stops)
	}
	// At 0 degrees the axis spans the width.
	sh = Fill(RectOps(box), box, Paint{Kind: PaintGradient, Stops: stops})[0].(*typeset.Path).Fill.Shading
	if sh.From != (typeset.Point{X: 0, Y: 50}) || sh.To != (typeset.Point{X: 200, Y: 50}) {
		t.Errorf("0 degrees: %v to %v", sh.From, sh.To)
	}
	sh = Fill(RectOps(box), box, Paint{Kind: PaintGradient, Stops: stops, Radial: true})[0].(*typeset.Path).Fill.Shading
	if sh.Kind != typeset.Radial || sh.Transform.E != 100 || sh.Transform.F != 50 {
		t.Errorf("radial %+v", sh)
	}
	// A box without area: the first stop, flat.
	flat := Fill(RectOps(box), typeset.Rect{Width: 10}, Paint{Kind: PaintGradient, Stops: stops})
	if p := flat[0].(*typeset.Path); p.Fill.Shading != nil || p.Fill.Color.R != 1 {
		t.Errorf("flat %+v", p.Fill)
	}
}

func TestFill_Kinds(t *testing.T) {
	box := typeset.Rect{Width: 60, Height: 30}
	ops := RectOps(box)
	if f := Fill(ops, box, Paint{Kind: PaintSolid, Color: typeset.Color{G: 1, A: 1}}); len(f) != 1 || f[0].(*typeset.Path).Fill.Rule != FillRule {
		t.Errorf("solid %v", f)
	}
	if f := Fill(line(), box, Paint{Kind: PaintSolid}); f != nil {
		t.Errorf("a line filled: %v", f)
	}
	if f := Fill(ops, box, Paint{Kind: PaintNone}); f != nil {
		t.Errorf("none %v", f)
	}
	src := &typeset.ImageSource{}
	pic := Fill(ops, box, Paint{Kind: PaintPicture, Picture: src})[0].(*typeset.Group)
	if pic.Clip == nil || len(pic.Content) != 1 {
		t.Errorf("picture %+v", pic)
	}
	tiled := Fill(ops, box, Paint{Kind: PaintPicture, Picture: src, TileWidth: 20, TileHeight: 20})[0].(*typeset.Group)
	if len(tiled.Content) != 6 {
		t.Errorf("%d tiles, want 6", len(tiled.Content))
	}
	ink, paper := typeset.Color{A: 1}, typeset.Color{R: 1, G: 1, B: 1, A: 1}
	lines := Fill(ops, box, Paint{Kind: PaintPattern, Pattern: "horz", Color: ink, Background: paper})
	g, ok := lines[1].(*typeset.Group)
	if len(lines) != 2 || !ok || g.Clip == nil || g.Content[0].(*typeset.Path).Stroke == nil {
		t.Fatalf("horz pattern %v", lines)
	}
	if n := len(g.Content[0].(*typeset.Path).Ops); n != 2*5 {
		t.Errorf("horz: %d ops, want 5 lines 6 pt apart", n)
	}
	blend := Fill(ops, box, Paint{Kind: PaintPattern, Pattern: "pct25", Color: ink, Background: paper})
	if g := blend[1].(*typeset.Group); g.Opacity != 0.25 {
		t.Errorf("pct25 opacity %g", g.Opacity)
	}
	// A transparent background or foreground paints nothing.
	if clear := Fill(ops, box, Paint{Kind: PaintPattern, Pattern: "horz", Color: ink}); len(clear) != 1 {
		t.Errorf("transparent background: %d fragments, want the lines alone", len(clear))
	}
	if clear := Fill(ops, box, Paint{Kind: PaintPattern, Pattern: "horz", Background: paper}); len(clear) != 1 {
		t.Errorf("transparent foreground: %d fragments, want the background alone", len(clear))
	}
}

func TestShade(t *testing.T) {
	c := typeset.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	tests := []struct {
		m    FillMode
		want float64
	}{{FillDarken, 0.3}, {FillDarkenLess, 0.4}, {FillLighten, 0.7}, {FillLightenLess, 0.6}, {FillNorm, 0.5}}
	for _, tt := range tests {
		if got := Shade(c, tt.m); math.Abs(got.R-tt.want) > 1e-9 || got.A != 1 {
			t.Errorf("%d: %v", tt.m, got)
		}
	}
}

func TestSplitOpen(t *testing.T) {
	ops := slices.Concat(RectOps(typeset.Rect{Width: 1, Height: 1}), line())
	got := splitOpen(Path{Ops: ops, Fill: true, Stroke: true})
	if len(got) != 2 || len(got[0].Ops) != 5 || got[0].Stroke || got[1].Fill || len(got[1].Ops) != 7 {
		t.Errorf("%+v", got)
	}
}

// TestFill_FarFromOrigin fills a box so far from the origin that adding a
// tile or a pattern's pitch to its coordinates leaves them unchanged: the
// tiles and the lines stop where a step does not move, instead of looping
// for ever.
func TestFill_FarFromOrigin(t *testing.T) {
	box := typeset.Rect{X: 1e18, Y: 1e18, Width: 1000, Height: 1000}
	ink, paper := typeset.Color{A: 1}, typeset.Color{R: 1, G: 1, B: 1, A: 1}
	paints := map[string]Paint{
		"tiled picture": {Kind: PaintPicture, Picture: &typeset.ImageSource{}, TileWidth: 20, TileHeight: 20},
		"horz":          {Kind: PaintPattern, Pattern: "horz", Color: ink, Background: paper},
		"diagCross":     {Kind: PaintPattern, Pattern: "diagCross", Color: ink, Background: paper},
	}
	for name, p := range paints {
		done := make(chan []typeset.Fragment, 1)
		go func() { done <- Fill(RectOps(box), box, p) }()
		select {
		case f := <-done:
			if len(f) == 0 {
				t.Errorf("%s: nothing painted", name)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: Fill did not return", name)
		}
	}
}

// TestFill_RectGradient checks a rectangle path gradient: four triangles
// from the box's centre, each shaded from the centre to its edge, under
// the shape's clip.
func TestFill_RectGradient(t *testing.T) {
	box := typeset.Rect{X: 10, Y: 20, Width: 100, Height: 50}
	ops := Paths(nil, box)[0].Ops
	stops := []typeset.ColorStop{{Offset: 0, Color: typeset.Color{R: 1, A: 1}}, {Offset: 1, Color: typeset.Color{B: 1, A: 1}}}
	frags := Fill(ops, box, Paint{Kind: PaintGradient, Stops: stops, Rect: true})
	g, ok := frags[0].(*typeset.Group)
	if len(frags) != 1 || !ok || g.Clip == nil || len(g.Content) != 4 {
		t.Fatalf("%+v", frags)
	}
	top := g.Content[0].(*typeset.Path).Fill.Shading
	if top.From != (typeset.Point{X: 60, Y: 45}) || top.To != (typeset.Point{X: 60, Y: 20}) {
		t.Errorf("top triangle shades %v to %v, want the centre to the top edge", top.From, top.To)
	}
}

// TestGradientStroke checks that a gradient line fills its stroke's
// outline with the gradient: a 4-point line along x has an outline 4
// points tall.
func TestGradientStroke(t *testing.T) {
	ops := []typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: 0, Y: 10}}}, {Kind: typeset.LineTo, P: [3]typeset.Point{{X: 100, Y: 10}}}}
	stops := []typeset.ColorStop{{Offset: 0, Color: typeset.Color{R: 1, A: 1}}, {Offset: 1, Color: typeset.Color{B: 1, A: 1}}}
	frags := GradientStroke(ops, Line{Width: 4}, Paint{Kind: PaintGradient, Stops: stops}, typeset.Rect{Width: 100, Height: 20})
	if len(frags) != 1 {
		t.Fatalf("%d fragments", len(frags))
	}
	p := frags[0].(*typeset.Path)
	if p.Fill == nil || p.Fill.Shading == nil || p.Stroke != nil {
		t.Fatalf("not a shaded fill: %+v", p)
	}
	y0, y1 := math.Inf(1), math.Inf(-1)
	for _, op := range p.Ops {
		if op.Kind != typeset.ClosePath {
			y0, y1 = math.Min(y0, op.P[0].Y), math.Max(y1, op.P[0].Y)
		}
	}
	if math.Abs(y0-8) > 1e-6 || math.Abs(y1-12) > 1e-6 {
		t.Errorf("outline spans y %v..%v, want 8..12", y0, y1)
	}
	if GradientStroke(ops, Line{}, Paint{}, typeset.Rect{}) != nil {
		t.Error("a line of no width painted")
	}
}
