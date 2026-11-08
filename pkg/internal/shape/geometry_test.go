package shape

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// TestPresetTable_EveryPresetEvaluates evaluates every preset of the
// table at its default adjustments in boxes of several shapes, the
// degenerate ones included, and checks the outlines are finite, start
// with a move and stay near the box.
func TestPresetTable_EveryPresetEvaluates(t *testing.T) {
	if n := len(PresetNames()); n != 187 {
		t.Fatalf("%d presets, want the 187 of ST_ShapeType", n)
	}
	boxes := []compose.Rect{
		{X: 10, Y: 20, Width: 200, Height: 100},
		{X: 0, Y: 0, Width: 50, Height: 300},
		{X: 5, Y: 5, Width: 0, Height: 40},
		{X: 5, Y: 5, Width: 0, Height: 0},
	}
	for _, name := range PresetNames() {
		g := &Geometry{Preset: name}
		if !Known(g) {
			t.Errorf("%s: not known", name)
		}
		if err := Check(g); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		for _, r := range boxes {
			paths := Paths(g, r)
			if len(paths) == 0 && r.Width > 0 && r.Height > 0 {
				t.Errorf("%s in %v: no path", name, r)
			}
			// Callouts and arcs reach outside the box by design (their
			// adjustments place the pointer a box away); a box's worth of
			// slack each way catches a wrong scale without flagging them.
			lo, hi := r.X-2*r.Width-r.Height, r.X+3*r.Width+r.Height
			for _, p := range paths {
				if p.Ops[0].Kind != compose.MoveTo {
					t.Errorf("%s: a path starts with op %d", name, p.Ops[0].Kind)
				}
				for _, op := range p.Ops {
					for _, pt := range op.P {
						if !finite(pt.X) || !finite(pt.Y) {
							t.Fatalf("%s in %v: point %v", name, r, pt)
						}
						if op.Kind != compose.ClosePath && r.Width > 0 && (pt.X < lo || pt.X > hi) {
							t.Errorf("%s in %v: x %g far outside the box", name, r, pt.X)
						}
					}
				}
			}
			tr := TextRect(g, r)
			if !finite(tr.X) || !finite(tr.Width) || tr.Width < 0 || tr.Height < 0 {
				t.Errorf("%s: text rectangle %v", name, tr)
			}
		}
	}
}

// pts flattens the end points of a path's operations.
func pts(ops []compose.PathOp) []compose.Point {
	var out []compose.Point
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo, compose.LineTo:
			out = append(out, op.P[0])
		case compose.CurveTo:
			out = append(out, op.P[2])
		case compose.ClosePath:
		}
	}
	return out
}

// near reports whether a and b are equal to a thousandth of a point.
func near(a, b compose.Point) bool {
	return math.Abs(a.X-b.X) < 1e-3 && math.Abs(a.Y-b.Y) < 1e-3
}

// TestPaths_HandComputed compares presets' outlines to points computed by
// hand from their Annex D definitions.
func TestPaths_HandComputed(t *testing.T) {
	r := compose.Rect{X: 10, Y: 20, Width: 200, Height: 100}
	tests := []struct {
		name string
		g    *Geometry
		want []compose.Point
	}{
		{"nil", nil, []compose.Point{{10, 20}, {210, 20}, {210, 120}, {10, 120}}},
		{"rect", &Geometry{Preset: "rect"}, []compose.Point{{10, 20}, {210, 20}, {210, 120}, {10, 120}}},
		// roundRect at adj 25000: the radius is a quarter of the shorter
		// side, 25 pt; the path starts at (l, dx1) and runs clockwise.
		{"roundRect", &Geometry{Preset: "roundRect", Adjust: []Adjust{{"adj", 25000}}}, []compose.Point{
			{10, 45}, {35, 20}, {185, 20}, {210, 45}, {210, 95}, {185, 120}, {35, 120}, {10, 95},
		}},
		// ellipse: from (l, vc), four quarter arcs.
		{"ellipse", &Geometry{Preset: "ellipse"}, []compose.Point{{10, 70}, {110, 20}, {210, 70}, {110, 120}, {10, 70}}},
		// rightArrow at its defaults: the shaft spans half the height
		// (y1 = vc - h/4), the head is ss/2 = 50 long.
		{"rightArrow", &Geometry{Preset: "rightArrow"}, []compose.Point{
			{10, 45}, {160, 45}, {160, 20}, {210, 70}, {160, 120}, {160, 95}, {10, 95},
		}},
		// rightArrow with adj1 100000 and adj2 20000: a full-height shaft,
		// a head 20 long.
		{"rightArrow adjusted", &Geometry{Preset: "rightArrow", Adjust: []Adjust{{"adj1", 100000}, {"adj2", 20000}}}, []compose.Point{
			{10, 20}, {190, 20}, {190, 20}, {210, 70}, {190, 120}, {190, 120}, {10, 120},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pts(Paths(tt.g, r)[0].Ops)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d points %v, want %v", len(got), got, tt.want)
			}
			for i := range got {
				if !near(got[i], tt.want[i]) {
					t.Errorf("point %d: got %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestPaths_Star5 checks star5's points against Annex D: the outer ones
// on the ellipse of radii wd2*hf and hd2*vf about (hc, vc*vf), at 18
// degrees above the horizontal, the inner ones at adj/50000 of it, at 54
// degrees, and the top at (hc, t).
func TestPaths_Star5(t *testing.T) {
	r := compose.Rect{Width: 100, Height: 100}
	got := pts(Paths(&Geometry{Preset: "star5"}, r)[0].Ops)
	if len(got) != 10 {
		t.Fatalf("got %d points, want 10", len(got))
	}
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	swd2, shd2, svc := 50*1.05146, 50*1.10557, 50*1.10557
	iwd2, ihd2 := swd2*19098/50000, shd2*19098/50000
	want := []compose.Point{
		{X: 50 - swd2*math.Cos(rad(18)), Y: svc - shd2*math.Sin(rad(18))},
		{X: 50 - iwd2*math.Cos(rad(54)), Y: svc - ihd2*math.Sin(rad(54))},
		{X: 50, Y: 0},
		{X: 50 + iwd2*math.Cos(rad(54)), Y: svc - ihd2*math.Sin(rad(54))},
		{X: 50 + swd2*math.Cos(rad(18)), Y: svc - shd2*math.Sin(rad(18))},
	}
	for i, w := range want {
		if !near(got[i], w) {
			t.Errorf("point %d: got %v, want %v", i, got[i], w)
		}
	}
}

// TestPaths_CustomGuidesAndArc draws a custom geometry with guides and an
// arc: a half disc over a 100 by 50 path space.
func TestPaths_CustomGuidesAndArc(t *testing.T) {
	var d DrawingML
	d.Guide("half", "*/ w 1 2", false)
	d.Guide("rad", "+- half 0 0", false)
	d.Path(0, 0, FillNorm, true)
	d.Command(CmdMove, "l", "b")
	d.Command(CmdArc, "rad", "h", "cd2", "cd2")
	d.Command(CmdClose)
	c := d.Custom()
	if len(c.Unresolved) != 0 {
		t.Fatalf("unresolved %v", c.Unresolved)
	}
	r := compose.Rect{X: 0, Y: 0, Width: 100, Height: 50}
	ops := Paths(&Geometry{Custom: c}, r)[0].Ops
	got := pts(ops)
	// From (0, 50) the arc of radii 50 and 50 starts at 180 degrees and
	// sweeps 180 clockwise: through (50, 0) to (100, 50).
	want := []compose.Point{{0, 50}, {50, 0}, {100, 50}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if !near(got[i], want[i]) {
			t.Errorf("point %d: got %v, want %v", i, got[i], want[i])
		}
	}
	// The first quarter's control points follow the circle's tangents.
	c1 := ops[1].P[0]
	if !near(c1, compose.Point{X: 0, Y: 50 - 50*kappa}) {
		t.Errorf("first control point %v", c1)
	}
}

// TestPaths_PathSpaceAndModes checks a path with its own coordinate space
// and fill and stroke settings.
func TestPaths_PathSpaceAndModes(t *testing.T) {
	var d DrawingML
	d.Path(10, 10, FillDarken, false)
	d.Command(CmdMove, "0", "0")
	d.Command(CmdLine, "10", "5")
	d.Path(0, 0, FillNone, true)
	d.Command(CmdLine, "w", "h")
	paths := Paths(&Geometry{Custom: d.Custom()}, compose.Rect{X: 1, Y: 2, Width: 20, Height: 40})
	if len(paths) != 2 {
		t.Fatalf("%d paths", len(paths))
	}
	if !paths[0].Fill || paths[0].Stroke || paths[0].Mode != FillDarken {
		t.Errorf("path 0 modes %+v", paths[0])
	}
	if got := pts(paths[0].Ops); !near(got[1], compose.Point{X: 21, Y: 22}) {
		t.Errorf("path 0 points %v", got)
	}
	if paths[1].Fill || !paths[1].Stroke {
		t.Errorf("path 1 modes %+v", paths[1])
	}
	// A line with no move first starts at the origin of the box.
	if got := pts(paths[1].Ops); len(got) != 2 || !near(got[0], compose.Point{X: 1, Y: 2}) || !near(got[1], compose.Point{X: 21, Y: 42}) {
		t.Errorf("path 1 points %v", got)
	}
}

// TestCheck_Problems reports unknown names and cycles.
func TestCheck_Problems(t *testing.T) {
	var d DrawingML
	d.Guide("a", "+- nope 0 0", false)
	d.Path(0, 0, FillNorm, true)
	d.Command(CmdMove, "a", "0")
	if err := Check(&Geometry{Custom: d.Custom()}); err == nil {
		t.Error("an unknown name passed")
	}
	cyc := &Custom{Guides: []Guide{
		{Name: "x", Op: OpVal, Args: [3]Arg{{Kind: ArgGuide, Ref: 1}}},
		{Name: "y", Op: OpVal, Args: [3]Arg{{Kind: ArgGuide, Ref: 0}}},
	}, Paths: []PathDef{{Cmds: []Cmd{{Kind: CmdMove, Args: []Arg{{Kind: ArgGuide, Ref: 0}, {}}}}}}}
	if err := Check(&Geometry{Custom: cyc}); err == nil {
		t.Error("a cycle passed")
	}
	if err := Check(&Geometry{Preset: "star5"}); err != nil {
		t.Error(err)
	}
	if Known(&Geometry{Preset: "nosuchshape"}) {
		t.Error("an unknown preset is known")
	}
	if got := Paths(&Geometry{Preset: "nosuchshape"}, compose.Rect{Width: 1, Height: 1}); len(got) != 1 || len(got[0].Ops) != 5 {
		t.Errorf("an unknown preset draws %v", got)
	}
}

// TestTextRect_Presets checks the text rectangles of a few presets.
func TestTextRect_Presets(t *testing.T) {
	r := compose.Rect{X: 10, Y: 20, Width: 200, Height: 100}
	il := 200 * 29289.0 / 200000 // ellipse: idx = wd2 * cos(45°) inset
	tests := []struct {
		g    *Geometry
		want compose.Rect
	}{
		{&Geometry{Preset: "rect"}, r},
		{&Geometry{Preset: "ellipse"}, compose.Rect{X: 10 + il, Y: 20 + 100*29289.0/200000, Width: 200 - 2*il, Height: 100 - 2*100*29289.0/200000}},
		{&Geometry{Preset: "rightArrow"}, compose.Rect{X: 10, Y: 45, Width: 175, Height: 50}},
		{&Geometry{Preset: "rect", MirrorH: true}, r},
	}
	for _, tt := range tests {
		got := TextRect(tt.g, r)
		if math.Abs(got.X-tt.want.X) > 0.01 || math.Abs(got.Y-tt.want.Y) > 0.01 || math.Abs(got.Width-tt.want.Width) > 0.01 || math.Abs(got.Height-tt.want.Height) > 0.01 {
			t.Errorf("%s: got %v, want %v", tt.g.Preset, got, tt.want)
		}
	}
}

// TestPaths_Mirror mirrors a right arrow into a left one.
func TestPaths_Mirror(t *testing.T) {
	r := compose.Rect{X: 10, Y: 20, Width: 200, Height: 100}
	got := pts(Paths(&Geometry{Preset: "rightArrow", MirrorH: true}, r)[0].Ops)
	if !near(got[3], compose.Point{X: 10, Y: 70}) {
		t.Errorf("the tip %v, want (10, 70)", got[3])
	}
}
