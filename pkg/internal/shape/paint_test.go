package shape

import (
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func TestPresetDash(t *testing.T) {
	tests := []struct {
		name  string
		width float64
		cap   compose.LineCap
		want  []float64
	}{
		{"dot", 2, compose.ButtCap, []float64{2, 6}},
		{"lgDashDotDot", 1, compose.ButtCap, []float64{8, 3, 1, 3, 1, 3}},
		{"sysDash", 3, compose.ButtCap, []float64{9, 3}},
		{"dash", 1, compose.RoundCap, []float64{3.01, 3.99}},
		{"solid", 1, compose.ButtCap, nil},
		{"nonsense", 1, compose.ButtCap, nil},
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
func line() []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: 0, Y: 0}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: 100, Y: 0}}},
	}
}

// TestStroke_Arrowheads checks a triangle at the end of a 2 pt line: the
// tip at the end, the base 6 pt back and 6 pt wide (medium: 3 line
// widths), the line stopping a fifteenth of the width short of the base;
// and a diamond centred on the start, the line stopping short of its
// centre.
func TestStroke_Arrowheads(t *testing.T) {
	frags := Stroke(line(), Line{
		Width: 2, Color: compose.Color{A: 1},
		Head: Arrowhead{Kind: ArrowDiamond}, Tail: Arrowhead{Kind: ArrowTriangle},
	})
	if len(frags) != 3 {
		t.Fatalf("%d fragments", len(frags))
	}
	body := frags[0].(*compose.Path)
	if got := body.Ops[1].P[0].X; math.Abs(got-(94+0.4)) > 1e-9 {
		t.Errorf("the line ends at %g, want 94.4", got)
	}
	if got := body.Ops[0].P[0].X; math.Abs(got-(3-0.4)) > 1e-9 {
		t.Errorf("the line starts at %g, want 2.6", got)
	}
	tri := frags[2].(*compose.Path)
	want := []compose.Point{{X: 100, Y: 0}, {X: 94, Y: 3}, {X: 94, Y: -3}}
	for i, w := range want {
		if p := tri.Ops[i].P[0]; math.Abs(p.X-w.X) > 1e-9 || math.Abs(p.Y-w.Y) > 1e-9 {
			t.Errorf("triangle point %d: %v, want %v", i, p, w)
		}
	}
	dia := frags[1].(*compose.Path)
	if p := dia.Ops[0].P[0]; math.Abs(p.X+3) > 1e-9 {
		t.Errorf("the diamond's tip %v, want 3 pt past the start", p)
	}
	// A thin line's arrowhead is sized from 0.7 mm.
	thin := Stroke(line(), Line{Width: 0.5, Tail: Arrowhead{Kind: ArrowTriangle, Width: ArrowLarge, Length: ArrowSmall}})
	tri = thin[1].(*compose.Path)
	base := 70.0 * 72 / 2540
	if p := tri.Ops[1].P[0]; math.Abs(p.X-(100-2*base)) > 1e-9 || math.Abs(p.Y-2.5*base) > 1e-9 {
		t.Errorf("thin triangle %v", p)
	}
	// A closed outline has none.
	closed := append(line(), compose.PathOp{Kind: compose.ClosePath})
	if got := Stroke(closed, Line{Width: 1, Tail: Arrowhead{Kind: ArrowOval}}); len(got) != 1 {
		t.Errorf("closed outline: %d fragments", len(got))
	}
	if Stroke(line(), Line{}) != nil {
		t.Error("a zero width stroked")
	}
}

func TestFill_Gradient(t *testing.T) {
	box := compose.Rect{X: 0, Y: 0, Width: 200, Height: 100}
	stops := []compose.ColorStop{{Offset: 0.8, Color: compose.Color{B: 1, A: 1}}, {Offset: 0.2, Color: compose.Color{R: 1, A: 1}}}
	f := Fill(RectOps(box), box, Paint{Kind: PaintGradient, Stops: stops, Angle: 90})
	sh := f[0].(*compose.Path).Fill.Shading
	if sh.Kind != compose.Axial || math.Abs(sh.From.X-100) > 1e-9 || math.Abs(sh.From.Y) > 1e-9 || math.Abs(sh.To.Y-100) > 1e-9 {
		t.Errorf("axial %v to %v", sh.From, sh.To)
	}
	if len(sh.Stops) != 4 || sh.Stops[0].Offset != 0 || sh.Stops[0].Color.R != 1 || sh.Stops[3].Offset != 1 || sh.Stops[3].Color.B != 1 {
		t.Errorf("stops %v", sh.Stops)
	}
	// At 0 degrees the axis spans the width.
	sh = Fill(RectOps(box), box, Paint{Kind: PaintGradient, Stops: stops})[0].(*compose.Path).Fill.Shading
	if sh.From != (compose.Point{X: 0, Y: 50}) || sh.To != (compose.Point{X: 200, Y: 50}) {
		t.Errorf("0 degrees: %v to %v", sh.From, sh.To)
	}
	sh = Fill(RectOps(box), box, Paint{Kind: PaintGradient, Stops: stops, Radial: true})[0].(*compose.Path).Fill.Shading
	if sh.Kind != compose.Radial || sh.Transform.E != 100 || sh.Transform.F != 50 {
		t.Errorf("radial %+v", sh)
	}
	// A box without area: the first stop, flat.
	flat := Fill(RectOps(box), compose.Rect{Width: 10}, Paint{Kind: PaintGradient, Stops: stops})
	if p := flat[0].(*compose.Path); p.Fill.Shading != nil || p.Fill.Color.R != 1 {
		t.Errorf("flat %+v", p.Fill)
	}
}

func TestFill_Kinds(t *testing.T) {
	box := compose.Rect{Width: 60, Height: 30}
	ops := RectOps(box)
	if f := Fill(ops, box, Paint{Kind: PaintSolid, Color: compose.Color{G: 1, A: 1}}); len(f) != 1 || f[0].(*compose.Path).Fill.Rule != FillRule {
		t.Errorf("solid %v", f)
	}
	if f := Fill(line(), box, Paint{Kind: PaintSolid}); f != nil {
		t.Errorf("a line filled: %v", f)
	}
	if f := Fill(ops, box, Paint{Kind: PaintNone}); f != nil {
		t.Errorf("none %v", f)
	}
	src := &compose.ImageSource{}
	pic := Fill(ops, box, Paint{Kind: PaintPicture, Picture: src})[0].(*compose.Group)
	if pic.Clip == nil || len(pic.Content) != 1 {
		t.Errorf("picture %+v", pic)
	}
	tiled := Fill(ops, box, Paint{Kind: PaintPicture, Picture: src, TileWidth: 20, TileHeight: 20})[0].(*compose.Group)
	if len(tiled.Content) != 6 {
		t.Errorf("%d tiles, want 6", len(tiled.Content))
	}
	lines := Fill(ops, box, Paint{Kind: PaintPattern, Pattern: "horz"})
	g, ok := lines[1].(*compose.Group)
	if len(lines) != 2 || !ok || g.Clip == nil || g.Content[0].(*compose.Path).Stroke == nil {
		t.Fatalf("horz pattern %v", lines)
	}
	if n := len(g.Content[0].(*compose.Path).Ops); n != 2*5 {
		t.Errorf("horz: %d ops, want 5 lines 6 pt apart", n)
	}
	blend := Fill(ops, box, Paint{Kind: PaintPattern, Pattern: "pct25"})
	if g := blend[1].(*compose.Group); g.Opacity != 0.25 {
		t.Errorf("pct25 opacity %g", g.Opacity)
	}
}

func TestShade(t *testing.T) {
	c := compose.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
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
	ops := slices.Concat(RectOps(compose.Rect{Width: 1, Height: 1}), line())
	got := splitOpen(Path{Ops: ops, Fill: true, Stroke: true})
	if len(got) != 2 || len(got[0].Ops) != 5 || got[0].Stroke || got[1].Fill || len(got[1].Ops) != 7 {
		t.Errorf("%+v", got)
	}
}
