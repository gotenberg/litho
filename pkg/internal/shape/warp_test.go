package shape

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// warpOf builds a custom warp from paths of (command, x, y...) strings.
func warpOf(paths ...[][]string) *TextWarp {
	var d DrawingML
	d.Guide("adj", "val 50000", true)
	d.Guide("y", "*/ h adj 100000", false)
	for _, p := range paths {
		d.Path(0, 0, FillNorm, true)
		for _, c := range p {
			kind := map[string]CmdKind{"m": CmdMove, "l": CmdLine, "c": CmdCubic, "a": CmdArc}[c[0]]
			d.Command(kind, c[1:]...)
		}
	}
	return &TextWarp{Custom: d.Custom()}
}

func nearPoint(a, b typeset.Point) bool { return math.Abs(a.X-b.X) < 1e-6 && math.Abs(a.Y-b.Y) < 1e-6 }

func TestWarpLanes_PairsAnEvenCountOfPaths(t *testing.T) {
	w := warpOf(
		[][]string{{"m", "l", "y"}, {"l", "hc", "t"}, {"l", "r", "y"}},
		[][]string{{"m", "l", "b"}, {"l", "r", "b"}},
	)
	r := typeset.Rect{X: 10, Y: 20, Width: 100, Height: 50}
	lanes := WarpLanes(w, r)
	if len(lanes) != 1 || !lanes[0].Envelope() {
		t.Fatalf("lanes = %d, envelope %v; want one envelope", len(lanes), len(lanes) == 1 && lanes[0].Envelope())
	}
	l := &lanes[0]
	// The top path is two equal sides of a triangle: its middle is the
	// apex, a quarter along the middle of its first side.
	for _, c := range []struct {
		u, v float64
		want typeset.Point
	}{
		{0, 0, typeset.Point{X: 10, Y: 45}},
		{0.5, 0, typeset.Point{X: 60, Y: 20}},
		{0.25, 0, typeset.Point{X: 35, Y: 32.5}},
		{0.5, 1, typeset.Point{X: 60, Y: 70}},
		{0.5, 0.5, typeset.Point{X: 60, Y: 45}},
		{1, 1, typeset.Point{X: 110, Y: 70}},
	} {
		if got := l.Map(c.u, c.v); !nearPoint(got, c.want) {
			t.Errorf("Map(%v, %v) = %v, want %v", c.u, c.v, got, c.want)
		}
	}
}

func TestWarpLanes_FollowsEachOfAnOddCountOfPaths(t *testing.T) {
	w := warpOf(
		[][]string{{"m", "l", "t"}, {"l", "r", "t"}},
		[][]string{{"m", "l", "vc"}, {"l", "r", "vc"}},
		[][]string{{"m", "l", "b"}, {"l", "r", "b"}},
	)
	lanes := WarpLanes(w, typeset.Rect{Width: 80, Height: 40})
	if len(lanes) != 3 {
		t.Fatalf("lanes = %d, want 3", len(lanes))
	}
	for i, want := range []float64{0, 20, 40} {
		if lanes[i].Envelope() {
			t.Fatalf("lane %d is an envelope", i)
		}
		p, dir := lanes[i].Top.At(0.25)
		if !nearPoint(p, typeset.Point{X: 20, Y: want}) || !nearPoint(dir, typeset.Point{X: 1}) {
			t.Errorf("lane %d At(0.25) = %v, %v", i, p, dir)
		}
	}
}

func TestWarpCurve_MeasuresAnArcByItsLength(t *testing.T) {
	// The upper half of a circle of radius 50 centred on (50, 50).
	w := warpOf([][]string{{"m", "l", "vc"}, {"a", "wd2", "hd2", "cd2", "cd2"}})
	lanes := WarpLanes(w, typeset.Rect{Width: 100, Height: 100})
	if len(lanes) != 1 {
		t.Fatalf("lanes = %d", len(lanes))
	}
	c := &lanes[0].Top
	if got := c.Length(); math.Abs(got-50*math.Pi) > 0.05 {
		t.Errorf("Length = %v, want %v", got, 50*math.Pi)
	}
	p, dir := c.At(0.5)
	if math.Abs(p.X-50) > 0.01 || math.Abs(p.Y) > 0.01 || math.Abs(dir.X-1) > 0.01 {
		t.Errorf("At(0.5) = %v, %v; want the top, heading right", p, dir)
	}
	q, _ := c.At(1.0 / 6)
	want := typeset.Point{X: 50 - 50*math.Cos(math.Pi/6), Y: 50 - 50*math.Sin(math.Pi/6)}
	if math.Abs(q.X-want.X) > 0.05 || math.Abs(q.Y-want.Y) > 0.05 {
		t.Errorf("At(1/6) = %v, want %v", q, want)
	}
}

func TestWarpLanes_WarpsNothingWithoutAPath(t *testing.T) {
	if l := WarpLanes(nil, typeset.Rect{Width: 1, Height: 1}); l != nil {
		t.Errorf("nil warp: %v", l)
	}
	if l := WarpLanes(&TextWarp{Preset: "textNoShape"}, typeset.Rect{Width: 1, Height: 1}); l != nil {
		t.Errorf("textNoShape: %v", l)
	}
	if l := WarpLanes(warpOf([][]string{{"m", "l", "t"}}), typeset.Rect{Width: 1, Height: 1}); l != nil {
		t.Errorf("a path of no length: %v", l)
	}
}

func TestWarpCurve_CapsItsVertices(t *testing.T) {
	var ops []typeset.PathOp
	ops = append(ops, typeset.PathOp{Kind: typeset.MoveTo})
	for i := range 2 * MaxWarpCurvePoints {
		ops = append(ops, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{{X: float64(i + 1)}}})
	}
	if c := flatten(ops); len(c.pts) != MaxWarpCurvePoints {
		t.Errorf("vertices = %d, want %d", len(c.pts), MaxWarpCurvePoints)
	}
}

func TestWarpLanes_EveryPreset(t *testing.T) {
	r := typeset.Rect{Width: 116.87, Height: 138.23}
	lanes := map[string]int{"textArchUp": 1, "textArchDown": 1, "textCircle": 1, "textButton": 3,
		"textButtonPour": 3, "textDeflateInflate": 2, "textDeflateInflateDeflate": 3}
	if len(textWarpTable) != 40 {
		t.Fatalf("%d presets, want 40", len(textWarpTable))
	}
	for name := range textWarpTable {
		w := &TextWarp{Preset: name}
		if err := Check(&Geometry{Custom: textWarpTable[name]}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		got := WarpLanes(w, r)
		want, ok := lanes[name]
		if !ok {
			want = 1
		}
		if len(got) != want {
			t.Errorf("%s: %d lanes, want %d", name, len(got), want)
			continue
		}
		single := name == "textArchUp" || name == "textArchDown" || name == "textCircle" || name == "textButton"
		for i := range got {
			if got[i].Envelope() == single {
				t.Errorf("%s lane %d: envelope %v", name, i, got[i].Envelope())
			}
		}
	}
}

// TestWarpLanes_ReadsTheHeightForTheShorterSide locks the paths of
// textDeflate in a box taller than wide, as PowerPoint draws them.
//
// # Corpus evidence
//
//   - litho/pptx text-warp-wordart: PowerPoint's textDeflate in a 116.87
//     by 138.23 point box dips its top path to 25.9 points at its middle,
//     0.1875 of the height times 4/3 times 3/4.
func TestWarpLanes_ReadsTheHeightForTheShorterSide(t *testing.T) {
	r := typeset.Rect{Width: 116.87, Height: 138.23}
	l := WarpLanes(&TextWarp{Preset: "textDeflate"}, r)
	if len(l) != 1 {
		t.Fatalf("lanes = %d", len(l))
	}
	top := l[0].Map(0.5, 0)
	if math.Abs(top.Y-0.1875*138.23) > 0.01 {
		t.Errorf("top middle at %v, want %v", top.Y, 0.1875*138.23)
	}
	adj := WarpLanes(&TextWarp{Preset: "textDeflate", Adjust: []Adjust{{Name: "adj", Value: 0}}}, r)
	if p := adj[0].Map(0.5, 0); math.Abs(p.Y) > 1e-9 {
		t.Errorf("adj 0: top middle at %v, want 0", p.Y)
	}
}
