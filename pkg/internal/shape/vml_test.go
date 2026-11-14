package shape

import (
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// vmlBox is the box the VML tests fit their geometries to.
var vmlBox = typeset.Rect{X: 10, Y: 20, Width: 100, Height: 50}

// vmlPaths compiles src and returns its paths in vmlBox.
func vmlPaths(t *testing.T, src VMLSource) ([]Path, []string) {
	t.Helper()
	c, problems := VML(src)
	return Paths(&Geometry{Custom: c}, vmlBox), problems
}

// TestVML_PathGrammar locks the commands of ECMA-376 Part 4, 14.1.2.19
// path over a coordinate space fitted to the box: empty parameters are 0,
// r is relative to the current point, x closes, e ends a path.
func TestVML_PathGrammar(t *testing.T) {
	t.Parallel()
	// Word's text box type, _x0000_t202.
	ps, problems := vmlPaths(t, VMLSource{Path: "m,l,21600r21600,l21600,xe", SizeX: 21600, SizeY: 21600})
	if len(problems) > 0 || len(ps) != 1 {
		t.Fatalf("paths %d, problems %v", len(ps), problems)
	}
	want := []typeset.Point{{X: 10, Y: 20}, {X: 10, Y: 70}, {X: 110, Y: 70}, {X: 110, Y: 20}}
	got := pts(ps[0].Ops)
	if len(got) != len(want) {
		t.Fatalf("points %v", got)
	}
	for i := range want {
		if !near(got[i], want[i]) {
			t.Errorf("point %d = %v, want %v", i, got[i], want[i])
		}
	}
	if k := ps[0].Ops[len(ps[0].Ops)-1].Kind; k != typeset.ClosePath {
		t.Errorf("last op %d, want a close", k)
	}
	if !ps[0].Fill || !ps[0].Stroke {
		t.Errorf("fill %v stroke %v", ps[0].Fill, ps[0].Stroke)
	}
}

// TestVML_FormulasAndAdjust locks the formulas of 14.1.2.6 over the adj
// values: Word's isosceles triangle, _x0000_t5, puts its apex at #0, and a
// sum or a prod reads its operands in the order v + p1 - p2, v * p1 / p2.
func TestVML_FormulasAndAdjust(t *testing.T) {
	t.Parallel()
	src := VMLSource{
		Path:     "m@0,l,21600r21600,xe",
		Formulas: []string{"val #0", "prod #0 1 2", "sum @1 10800 0"},
		Adjust:   []float64{5400},
		SizeX:    21600, SizeY: 21600,
	}
	ps, problems := vmlPaths(t, src)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if p := pts(ps[0].Ops)[0]; !near(p, typeset.Point{X: 10 + 25, Y: 20}) {
		t.Errorf("apex %v, want x 35", p)
	}
	src.Path = "m@2,l,21600r21600,xe"
	ps, _ = vmlPaths(t, src)
	if p := pts(ps[0].Ops)[0]; !near(p, typeset.Point{X: 10 + 62.5, Y: 20}) {
		t.Errorf("apex of sum(prod) %v, want x 72.5", p)
	}
	c, _ := VML(src)
	if err := Check(&Geometry{Custom: c}); err != nil {
		t.Error(err)
	}
}

// TestVML_EveryOperation evaluates each operation of 14.1.2.6 once.
func TestVML_EveryOperation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		eqn  string
		want float64
	}{
		{"val 7", 7},
		{"sum 1 2 3", 0},
		{"prod 6 4 3", 8},
		{"mid 2 6", 4},
		{"abs -5", 5},
		{"min 2 3", 2},
		{"max 2 3", 3},
		{"if -1 2 3", 3},
		{"if 1 2 3", 2},
		{"mod 3 4 0", 5},
		{"atan2 1 1", 45 * fd},
		{"sin 10 5898240", 10},
		{"cos 10 0", 10},
		{"tan 10 2949120", 10},
		{"cosatan2 10 1 0", 10},
		{"sinatan2 10 0 1", 10},
		{"sqrt 16", 4},
		{"sumangle 0 1 0", fd},
		{"ellipse 0 10 7", 7},
		{"val width", 200},
		{"val xcenter", 150},
	} {
		c, problems := VML(VMLSource{Path: "m@0,0", Formulas: []string{tc.eqn}, OriginX: 50, SizeX: 200, SizeY: 100})
		if len(problems) > 0 {
			t.Errorf("%s: %v", tc.eqn, problems)
			continue
		}
		ev := newEvaluator(c, nil, typeset.Rect{Width: 1, Height: 1})
		if got := ev.guide(len(c.Adjust)); got < tc.want-1e-6*max(1, tc.want) || got > tc.want+1e-6*max(1, tc.want) {
			t.Errorf("%s = %g, want %g", tc.eqn, got, tc.want)
		}
	}
}

// TestVML_RelativeCurves locks v, whose three points are relative to the
// segment's start, and m starting a second subpath of the same path, the
// hole an even-odd fill leaves.
func TestVML_RelativeCurves(t *testing.T) {
	t.Parallel()
	ps, problems := vmlPaths(t, VMLSource{Path: "m10,10v,10,10,10,10,20l,30xm40,40r10,l50,50xe", SizeX: 100, SizeY: 100})
	if len(problems) > 0 || len(ps) != 1 {
		t.Fatalf("paths %d, problems %v", len(ps), problems)
	}
	var curve *typeset.PathOp
	moves := 0
	for i, op := range ps[0].Ops {
		if op.Kind == typeset.CurveTo && curve == nil {
			curve = &ps[0].Ops[i]
		}
		if op.Kind == typeset.MoveTo {
			moves++
		}
	}
	// (10,10) + (10,20) is (20,30) in a 100 by 100 space: (30, 35) in
	// the box.
	if curve == nil || !near(curve.P[2], typeset.Point{X: 30, Y: 35}) {
		t.Errorf("curve %v", curve)
	}
	if moves != 2 {
		t.Errorf("%d subpaths, want 2", moves)
	}
}

// TestVML_FillStrokeAndEnd locks nf and ns, which leave the path they are
// in unfilled or unstroked, and e, which cuts the paths.
func TestVML_FillStrokeAndEnd(t *testing.T) {
	t.Parallel()
	ps, problems := vmlPaths(t, VMLSource{Path: "m,l10,10nfem,l5,5nsxe", SizeX: 10, SizeY: 10})
	if len(problems) > 0 || len(ps) != 2 {
		t.Fatalf("paths %d, problems %v", len(ps), problems)
	}
	if ps[0].Fill || !ps[0].Stroke || !ps[1].Fill || ps[1].Stroke {
		t.Errorf("fill and stroke %v %v, %v %v", ps[0].Fill, ps[0].Stroke, ps[1].Fill, ps[1].Stroke)
	}
}

// TestVML_AngleEllipse locks al: a full turn of 360 * 65,536 draws the
// ellipse of its centre and radii, and a negative swing covers the same
// arc as its positive twin from the other end.
func TestVML_AngleEllipse(t *testing.T) {
	t.Parallel()
	ps, problems := vmlPaths(t, VMLSource{Path: "al10800,10800,10800,10800,0,23592960xe", SizeX: 21600, SizeY: 21600})
	if len(problems) > 0 || len(ps) != 1 {
		t.Fatalf("paths %d, problems %v", len(ps), problems)
	}
	x0, y0, x1, y1 := 1e9, 1e9, -1e9, -1e9
	for _, p := range pts(ps[0].Ops) {
		x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
	}
	if !near(typeset.Point{X: x0, Y: y0}, typeset.Point{X: 10, Y: 20}) || !near(typeset.Point{X: x1, Y: y1}, typeset.Point{X: 110, Y: 70}) {
		t.Errorf("ellipse spans %g,%g to %g,%g", x0, y0, x1, y1)
	}
	a, _ := vmlPaths(t, VMLSource{Path: "al50,50,50,50,0,5898240e", SizeX: 100, SizeY: 100})
	b, _ := vmlPaths(t, VMLSource{Path: "al50,50,50,50,5898240,-5898240e", SizeX: 100, SizeY: 100})
	if !slices.Equal(a[0].Ops, b[0].Ops) {
		t.Errorf("a negative swing draws %v, want %v", b[0].Ops, a[0].Ops)
	}
}

// TestVML_Params locks how parameters split: @n and a sign start a new
// value, an empty one beside a comma is 0.
func TestVML_Params(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"@4@5", []string{"@4", "@5"}},
		{"7200@2,14400@2", []string{"7200", "@2", "14400", "@2"}},
		{",", []string{"0", "0"}},
		{",64,57,", []string{"0", "64", "57", "0"}},
		{"5,,8,4", []string{"5", "0", "8", "4"}},
		{"-3,8,-8", []string{"-3", "8", "-8"}},
		{" 1 2 ", []string{"1", "2"}},
		{"", nil},
	} {
		if got := params(tc.in, nil); !slices.Equal(got, tc.want) {
			t.Errorf("params(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestVML_Problems locks what VML reports and refuses: an unknown command
// or operation, parameters left over, a name or reference that names
// nothing, and a formula naming a later one, which resolves.
func TestVML_Problems(t *testing.T) {
	t.Parallel()
	_, problems := VML(VMLSource{Path: "m,l10,10,5zxe", Formulas: []string{"frob 1"}})
	joined := strings.Join(problems, "; ")
	for _, want := range []string{"path command z", "formula operation frob", "left over after command l"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems %q lack %q", joined, want)
		}
	}
	c, _ := VML(VMLSource{Path: "m@0,@7l#3,@2", Formulas: []string{"val @1", "sum 3 0 0", "val nosuch"}})
	if err := Check(&Geometry{Custom: c}); err == nil || !strings.Contains(err.Error(), "@7") || !strings.Contains(err.Error(), "#3") || !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("Check = %v", err)
	}
	ev := newEvaluator(c, nil, typeset.Rect{Width: 1, Height: 1})
	if v := ev.guide(0); v != 3 {
		t.Errorf("@0 naming the later @1 = %g, want 3", v)
	}
	cyc, _ := VML(VMLSource{Path: "m@0,0", Formulas: []string{"val @1", "val @0"}})
	if err := Check(&Geometry{Custom: cyc}); err == nil {
		t.Error("a cycle of formulas checks")
	}
}

// TestVML_Caps locks the bounds on what a file can make VML compute: the
// formulas past MaxVMLFormulas, the adjustments past MaxVMLAdjust and the
// commands past MaxCommands are dropped with a problem.
func TestVML_Caps(t *testing.T) {
	t.Parallel()
	fs := make([]string, MaxVMLFormulas+5)
	for i := range fs {
		fs[i] = "val 1"
	}
	c, problems := VML(VMLSource{Path: "m,l1,1e", Formulas: fs, Adjust: make([]float64, MaxVMLAdjust+1)})
	if len(c.Guides) > MaxGuides || !strings.Contains(strings.Join(problems, ";"), "formulas past") || !strings.Contains(strings.Join(problems, ";"), "adjustments past") {
		t.Errorf("guides %d, problems %v", len(c.Guides), problems)
	}
	path := "m,l" + strings.Repeat("1,1,", MaxCommands+10) + "e"
	c, problems = VML(VMLSource{Path: path})
	n := 0
	for _, p := range c.Paths {
		n += len(p.Cmds)
	}
	if n > MaxCommands+1 || !strings.Contains(strings.Join(problems, ";"), "commands past") {
		t.Errorf("%d commands, problems %v", n, problems)
	}
	// Relative commands over formulas need guides: they stop at
	// MaxGuides.
	path = "m@0,@0" + strings.Repeat("r1,1", MaxGuides)
	c, problems = VML(VMLSource{Path: path, Formulas: []string{"val 1"}})
	if len(c.Adjust)+len(c.Guides) > MaxGuides || !strings.Contains(strings.Join(problems, ";"), "guides past") {
		t.Errorf("%d guides, problems %v", len(c.Guides), problems)
	}
}

// TestVML_QuadraticAndQuadrants locks qb, a quadratic B-spline whose
// inner points are the midpoints of its control points, and qx and qy,
// which alternate.
func TestVML_QuadraticAndQuadrants(t *testing.T) {
	t.Parallel()
	c, problems := VML(VMLSource{Path: "m,qb10,0,10,10,0,10qx10,0,0,10e", SizeX: 10, SizeY: 10})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	var kinds []CmdKind
	for _, cmd := range c.Paths[0].Cmds {
		kinds = append(kinds, cmd.Kind)
	}
	want := []CmdKind{CmdMove, CmdQuad, CmdQuad, CmdQuadrantX, CmdQuadrantY}
	if !slices.Equal(kinds, want) {
		t.Errorf("commands %v, want %v", kinds, want)
	}
	// The first quadratic ends between (10,0) and (10,10).
	if a := c.Paths[0].Cmds[1].Args; a[2].Value != 10 || a[3].Value != 5 {
		t.Errorf("first end %v", a)
	}
}

// FuzzVML compiles and draws arbitrary paths and formulas.
func FuzzVML(f *testing.F) {
	f.Add("m@0,l,21600r21600,xe", "val #0")
	f.Add("m@0,l,216", "prod #0 1")
	f.Fuzz(func(t *testing.T, path, eqn string) {
		c, _ := VML(VMLSource{Path: path, Formulas: []string{eqn, "sum @0 1 0"}, Adjust: []float64{5400}, SizeX: 21600, SizeY: 21600})
		g := &Geometry{Custom: c}
		_ = Check(g) // Check's report is not under test: Paths must not fail.
		Paths(g, vmlBox)
		TextRect(g, vmlBox)
	})
}
