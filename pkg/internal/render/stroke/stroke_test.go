package stroke

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// fineTol keeps arc and curve chords so close to the exact shape that the
// area of a round cap or join matches π·r² to well under testAreaTol.
const fineTol = 1e-5

const testAreaTol = 2e-3

// nonzeroArea returns the area where the winding number of p is nonzero,
// every subpath closed, by integrating the covered length of horizontal
// scanlines through slab midpoints. The midpoint rule is exact for a slab
// that holds no vertex, so the error shrinks with the square of the slab
// height.
func nonzeroArea(t *testing.T, p geom.Path) float64 {
	t.Helper()
	type edge struct{ a, b geom.Point }
	var edges []edge
	k := 0
	var start, cur geom.Point
	open := false
	for _, op := range p.Ops {
		switch op {
		case geom.MoveTo:
			if open && cur != start {
				edges = append(edges, edge{cur, start})
			}
			start, cur, open = p.Pts[k], p.Pts[k], true
			k++
		case geom.LineTo:
			edges = append(edges, edge{cur, p.Pts[k]})
			cur = p.Pts[k]
			k++
		case geom.CubicTo:
			t.Fatal("outline holds a cubic")
		case geom.Close:
			if cur != start {
				edges = append(edges, edge{cur, start})
			}
			cur = start
		}
	}
	if open && cur != start {
		edges = append(edges, edge{cur, start})
	}
	r, ok := p.Bounds()
	if !ok {
		return 0
	}
	const slabs = 20000
	dy := (r.Max.Y - r.Min.Y) / slabs
	if dy == 0 {
		return 0
	}
	type crossing struct {
		x float64
		w int
	}
	// Each edge files its crossings under the slabs whose midline it spans,
	// so the work is the number of crossings, not slabs times edges.
	rows := make([][]crossing, slabs)
	for _, e := range edges {
		a, b, w := e.a, e.b, 1
		if a.Y > b.Y {
			a, b, w = b, a, -1
		}
		first := max(0, int(math.Ceil((a.Y-r.Min.Y)/dy-0.5)))
		for i := first; i < slabs; i++ {
			y := r.Min.Y + (float64(i)+0.5)*dy
			if y >= b.Y {
				break
			}
			if y < a.Y {
				continue
			}
			rows[i] = append(rows[i], crossing{a.X + (y-a.Y)*(b.X-a.X)/(b.Y-a.Y), w})
		}
	}
	area := 0.0
	for _, xs := range rows {
		slices.SortFunc(xs, func(p, q crossing) int { return cmpFloat(p.x, q.x) })
		wind := 0
		for j, c := range xs {
			if wind != 0 {
				area += (c.x - xs[j-1].x) * dy
			}
			wind += c.w
		}
	}
	return area
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func polyline(closed bool, pts ...geom.Point) geom.Path {
	var p geom.Path
	p.MoveTo(pts[0])
	for _, pt := range pts[1:] {
		p.LineTo(pt)
	}
	if closed {
		p.Close()
	}
	return p
}

func pt(x, y float64) geom.Point { return geom.Point{X: x, Y: y} }

func outline(t *testing.T, p geom.Path, s Style, ctm geom.Matrix) geom.Path {
	t.Helper()
	out, err := Outline(p, s, ctm, fineTol)
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	return out
}

func checkArea(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > testAreaTol*max(1, want) {
		t.Errorf("%s: area %.5f, want %.5f", name, got, want)
	}
}

// TestCapsAreas strokes a 10-unit horizontal line of width 2 with each cap:
// butt adds nothing, square adds a 1×2 box at each end, round a half disc of
// radius 1 at each end.
func TestCapsAreas(t *testing.T) {
	t.Parallel()
	line := polyline(false, pt(0, 0), pt(10, 0))
	for _, tc := range []struct {
		cap  Cap
		want float64
	}{
		{ButtCap, 20},
		{SquareCap, 24},
		{RoundCap, 20 + math.Pi},
		{Cap(9), 20},
	} {
		got := nonzeroArea(t, outline(t, line, Style{Width: 2, Cap: tc.cap}, geom.Identity))
		checkArea(t, "cap "+string(rune('0'+tc.cap%10)), got, tc.want)
	}
}

// TestJoinAreas strokes an L of two 10-unit legs, width 2, butt caps. The
// legs cover 20 + 20 − 1 (their 1×1 overlap); the outer corner adds a full
// unit square for a miter, half of it for a bevel, a quarter disc for round.
func TestJoinAreas(t *testing.T) {
	t.Parallel()
	ell := polyline(false, pt(0, 0), pt(10, 0), pt(10, 10))
	for _, tc := range []struct {
		name string
		join Join
		want float64
	}{
		{"miter", MiterJoin, 40},
		{"bevel", BevelJoin, 39.5},
		{"round", RoundJoin, 39 + math.Pi/4},
	} {
		s := Style{Width: 2, Join: tc.join, MiterLimit: DefaultMiterLimit}
		checkArea(t, tc.name, nonzeroArea(t, outline(t, ell, s, geom.Identity)), tc.want)
		// The mirrored L turns the other way, so the left side is outer.
		mirror := polyline(false, pt(0, 0), pt(10, 0), pt(10, -10))
		checkArea(t, tc.name+" mirrored", nonzeroArea(t, outline(t, mirror, s, geom.Identity)), tc.want)
	}
}

// TestMiterLimitSwitch pins the switch-over of 8.4.3.5 on a right angle,
// whose miter ratio 1/sin(45°) is √2.
func TestMiterLimitSwitch(t *testing.T) {
	t.Parallel()
	ell := polyline(false, pt(0, 0), pt(10, 0), pt(10, 10))
	below := Style{Width: 2, Join: MiterJoin, MiterLimit: 1.4142}
	above := Style{Width: 2, Join: MiterJoin, MiterLimit: 1.4143}
	checkArea(t, "limit below √2", nonzeroArea(t, outline(t, ell, below, geom.Identity)), 39.5)
	checkArea(t, "limit above √2", nonzeroArea(t, outline(t, ell, above, geom.Identity)), 40)
	low := Style{Width: 2, Join: MiterJoin, MiterLimit: -3}
	checkArea(t, "negative limit", nonzeroArea(t, outline(t, ell, low, geom.Identity)), 39.5)

	// A 60° turn (φ = 120°) has ratio 1/sin(60°) = 2/√3 ≈ 1.1547. With h
	// = 1, the mitered corner is a kite of area tan(30°) and the bevelled one
	// a triangle of area sin(60°)/2.
	turn := polyline(false, pt(0, 0), pt(10, 0), pt(10+5, -5*math.Sqrt(3)))
	miter := nonzeroArea(t, outline(t, turn, Style{Width: 2, MiterLimit: 1.155}, geom.Identity))
	bevel := nonzeroArea(t, outline(t, turn, Style{Width: 2, MiterLimit: 1.154}, geom.Identity))
	want := math.Tan(math.Pi/6) - math.Sin(math.Pi/3)/2
	if math.Abs(miter-bevel-want) > testAreaTol {
		t.Errorf("60° miter adds %.5f over the bevel, want %.5f", miter-bevel, want)
	}
}

// TestClosedSquare strokes a closed 10×10 square of width 2: the ring
// between a 12×12 and an 8×8 square, with its four outer corners mitered,
// bevelled or rounded. A closed subpath joins at its start, so the start
// corner is no different from the others.
func TestClosedSquare(t *testing.T) {
	t.Parallel()
	sq := polyline(true, pt(0, 0), pt(10, 0), pt(10, 10), pt(0, 10))
	for _, tc := range []struct {
		name string
		join Join
		want float64
	}{
		{"miter", MiterJoin, 80},
		{"bevel", BevelJoin, 78},
		{"round", RoundJoin, 76 + math.Pi},
	} {
		s := Style{Width: 2, Join: tc.join, Cap: RoundCap, MiterLimit: DefaultMiterLimit}
		checkArea(t, tc.name, nonzeroArea(t, outline(t, sq, s, geom.Identity)), tc.want)
	}
	// An explicit last point on the start, then h, is the same square.
	sq2 := polyline(true, pt(0, 0), pt(10, 0), pt(10, 10), pt(0, 10), pt(0, 0))
	checkArea(t, "repeated start", nonzeroArea(t, outline(t, sq2, Style{Width: 2, MiterLimit: 10}, geom.Identity)), 80)
	// The same square left open has butt ends meeting at the start corner,
	// which loses that corner's unit square.
	open := polyline(false, pt(0, 0), pt(10, 0), pt(10, 10), pt(0, 10), pt(0, 0))
	checkArea(t, "open", nonzeroArea(t, outline(t, open, Style{Width: 2, MiterLimit: 10}, geom.Identity)), 79)
}

// TestCollinearAndReversal checks a vertex that does not turn and one that
// turns back on itself.
func TestCollinearAndReversal(t *testing.T) {
	t.Parallel()
	straight := polyline(false, pt(0, 0), pt(5, 0), pt(10, 0))
	checkArea(t, "collinear", nonzeroArea(t, outline(t, straight, Style{Width: 2, MiterLimit: 10}, geom.Identity)), 20)
	back := polyline(false, pt(0, 0), pt(10, 0), pt(0, 0))
	// The reversal's miter is infinite, so it bevels and adds nothing.
	checkArea(t, "reversal miter", nonzeroArea(t, outline(t, back, Style{Width: 2, MiterLimit: 10}, geom.Identity)), 20)
	round := Style{Width: 2, Join: RoundJoin, Cap: RoundCap}
	checkArea(t, "reversal round", nonzeroArea(t, outline(t, back, round, geom.Identity)), 20+math.Pi)
}

// TestDashes lays dash patterns along a 9-unit line of width 2.
func TestDashes(t *testing.T) {
	t.Parallel()
	line := polyline(false, pt(0, 0), pt(9, 0))
	for _, tc := range []struct {
		name  string
		dash  []float64
		phase float64
		cap   Cap
		want  float64
	}{
		// On [0,2] and [5,7].
		{"phase 0", []float64{2, 3}, 0, ButtCap, 8},
		// Phase 1 is inside the first dash: on [0,1], [4,6], and the dash
		// starting at 9 begins where the line ends.
		{"phase 1", []float64{2, 3}, 1, ButtCap, 6},
		// Phase 3 is inside the gap: on [2,4] and [7,9].
		{"phase 3", []float64{2, 3}, 3, ButtCap, 8},
		// A phase past the pattern length wraps: 8 ≡ 3.
		{"phase wraps", []float64{2, 3}, 8, ButtCap, 8},
		// A negative phase wraps too: −2 ≡ 3.
		{"negative phase", []float64{2, 3}, -2, ButtCap, 8},
		// An odd array repeats: [2] is [2 2], on [0,2], [4,6], [8,9].
		{"odd", []float64{2}, 0, ButtCap, 10},
		// All zero, negative or empty arrays stroke solid.
		{"all zero", []float64{0, 0}, 0, ButtCap, 18},
		{"negative", []float64{2, -1}, 0, ButtCap, 18},
		{"empty", nil, 0, ButtCap, 18},
		// Zero-length dashes at 0, 4 and 8 paint caps only.
		{"dots round", []float64{0, 4}, 0, RoundCap, 3 * math.Pi},
		{"dots square", []float64{0, 4}, 0, SquareCap, 3 * 4},
		{"dots butt", []float64{0, 4}, 0, ButtCap, 0},
		// Square caps extend each dash [0,2], [5,7] by 1 at both ends,
		// clipped by nothing: two 4×2 boxes.
		{"square caps", []float64{2, 3}, 0, SquareCap, 16},
	} {
		s := Style{Width: 2, Cap: tc.cap, Dash: tc.dash, DashPhase: tc.phase}
		checkArea(t, tc.name, nonzeroArea(t, outline(t, line, s, geom.Identity)), tc.want)
	}
}

// TestDashAcrossCorner checks that a dash spanning a vertex is one dash with
// a join, and that each subpath restarts the pattern.
func TestDashAcrossCorner(t *testing.T) {
	t.Parallel()
	ell := polyline(false, pt(0, 0), pt(10, 0), pt(10, 10))
	// [12 100]: one dash covering [0,10] and [10,12] of the second leg, so
	// 10 + 2 legs, the miter corner included: 20 + 4 − 1 + 1.
	s := Style{Width: 2, Dash: []float64{12, 100}, MiterLimit: 10}
	checkArea(t, "across corner", nonzeroArea(t, outline(t, ell, s, geom.Identity)), 24)

	var two geom.Path
	two.MoveTo(pt(0, 0))
	two.LineTo(pt(9, 0))
	two.MoveTo(pt(0, 10))
	two.LineTo(pt(9, 10))
	s = Style{Width: 2, Dash: []float64{2, 3}}
	checkArea(t, "restart", nonzeroArea(t, outline(t, two, s, geom.Identity)), 16)
}

// TestZeroDashOrientation checks that the square of a zero-length dash lies
// along the path: on a 45° line its corners reach √2 past the point along
// the axes.
func TestZeroDashOrientation(t *testing.T) {
	t.Parallel()
	diag := polyline(false, pt(0, 0), pt(30, 30))
	out := outline(t, diag, Style{Width: 2, Cap: SquareCap, Dash: []float64{0, 100}}, geom.Identity)
	r, ok := out.Bounds()
	if !ok {
		t.Fatal("no outline")
	}
	if math.Abs(r.Max.X-math.Sqrt2) > 1e-9 || math.Abs(r.Min.Y+math.Sqrt2) > 1e-9 {
		t.Errorf("rotated square bounds %v, want ±√2", r)
	}
}

// TestDegenerateSubpaths checks 8.5.3.2: a degenerate subpath paints a
// filled circle with round caps and nothing with square or butt caps, unlike
// a zero-length dash (see TestDashes).
func TestDegenerateSubpaths(t *testing.T) {
	t.Parallel()
	var closedPoint geom.Path
	closedPoint.MoveTo(pt(5, 5))
	closedPoint.Close()
	coincident := polyline(false, pt(5, 5), pt(5, 5), pt(5, 5))
	var lone geom.Path
	lone.MoveTo(pt(5, 5))
	for _, tc := range []struct {
		name string
		path geom.Path
		cap  Cap
		want float64
	}{
		{"closed round", closedPoint, RoundCap, math.Pi},
		{"closed square", closedPoint, SquareCap, 0},
		{"closed butt", closedPoint, ButtCap, 0},
		{"coincident round", coincident, RoundCap, math.Pi},
		{"coincident square", coincident, SquareCap, 0},
		{"coincident butt", coincident, ButtCap, 0},
		{"lone moveto", lone, RoundCap, 0},
	} {
		got := nonzeroArea(t, outline(t, tc.path, Style{Width: 2, Cap: tc.cap}, geom.Identity))
		checkArea(t, tc.name, got, tc.want)
	}
	// A dash pattern starting in a gap hides the dot.
	got := nonzeroArea(t, outline(t, closedPoint, Style{Width: 2, Cap: RoundCap, Dash: []float64{1, 1}, DashPhase: 1}, geom.Identity))
	checkArea(t, "dot in gap", got, 0)
}

// TestNonUniformCTM checks that the pen is applied in user space: every
// stroke's device area is its user area times |det CTM|, and the device width
// of a horizontal line follows the y scale only.
func TestNonUniformCTM(t *testing.T) {
	t.Parallel()
	ell := polyline(false, pt(0, 0), pt(10, 0), pt(10, 10))
	user := nonzeroArea(t, outline(t, ell, Style{Width: 2, Cap: RoundCap, Join: RoundJoin}, geom.Identity))
	for _, m := range []geom.Matrix{
		{1, 0, 0, 3, 0, 0},
		{3, 0, 0, 1, 7, -2},
		{2, 1, -0.5, 1.5, 0, 0},
		{0, -2, 1, 0, 0, 0},
	} {
		got := nonzeroArea(t, outline(t, ell, Style{Width: 2, Cap: RoundCap, Join: RoundJoin}, m))
		checkArea(t, "ctm", got, user*math.Abs(m.Det()))
	}
	line := polyline(false, pt(0, 0), pt(10, 0))
	out := outline(t, line, Style{Width: 2}, geom.Matrix{1, 0, 0, 3, 0, 0})
	r, _ := out.Bounds()
	if math.Abs(r.Max.Y-r.Min.Y-6) > 1e-9 || math.Abs(r.Max.X-r.Min.X-10) > 1e-9 {
		t.Errorf("scaled line spans %v, want 10×6", r)
	}
}

// TestHairline checks that a zero width strokes one device pixel whatever
// the CTM, along the mapped path, with the dashes measured in user space.
func TestHairline(t *testing.T) {
	t.Parallel()
	line := polyline(false, pt(0, 0), pt(10, 0))
	for _, m := range []geom.Matrix{geom.Identity, {5, 0, 0, 5, 0, 0}, {5, 0, 0, 0.01, 0, 0}} {
		got := nonzeroArea(t, outline(t, line, Style{}, m))
		checkArea(t, "hairline", got, 10*m[0])
	}
	dashed := Style{Dash: []float64{2, 3}}
	got := nonzeroArea(t, outline(t, polyline(false, pt(0, 0), pt(9, 0)), dashed, geom.Matrix{5, 0, 0, 5, 0, 0}))
	checkArea(t, "dashed hairline", got, 20)
}

// TestCircleAnnulus strokes a circle drawn as four cubics: the ring between
// radii 9 and 11. The cubic circle deviates from the true one by 0.027% of
// the radius, well inside the test tolerance.
func TestCircleAnnulus(t *testing.T) {
	t.Parallel()
	const r, k = 10.0, 0.5522847498
	var c geom.Path
	c.MoveTo(pt(r, 0))
	c.CubicTo(pt(r, k*r), pt(k*r, r), pt(0, r))
	c.CubicTo(pt(-k*r, r), pt(-r, k*r), pt(-r, 0))
	c.CubicTo(pt(-r, -k*r), pt(-k*r, -r), pt(0, -r))
	c.CubicTo(pt(k*r, -r), pt(r, -k*r), pt(r, 0))
	c.Close()
	got := nonzeroArea(t, outline(t, c, Style{Width: 2, Join: BevelJoin}, geom.Identity))
	want := math.Pi * (11*11 - 9*9)
	if math.Abs(got-want) > 0.05 {
		t.Errorf("annulus area %.4f, want %.4f", got, want)
	}
	// A width wider than the curve's diameter fills the disc of radius
	// r + w/2: the inner offset folds over, and the nonzero rule fills it.
	got = nonzeroArea(t, outline(t, c, Style{Width: 30, Join: BevelJoin}, geom.Identity))
	want = math.Pi * 25 * 25
	if math.Abs(got-want) > 0.5 {
		t.Errorf("filled disc area %.4f, want %.4f", got, want)
	}
}

// TestToleranceBoundsArcs checks that a coarse tolerance still keeps every
// outline point within the pen radius plus the tolerance of the path.
func TestToleranceBoundsArcs(t *testing.T) {
	t.Parallel()
	line := polyline(false, pt(0, 0), pt(10, 0))
	out, err := Outline(line, Style{Width: 8, Cap: RoundCap}, geom.Identity, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range out.Pts {
		dx := max(0, -p.X, p.X-10)
		if d := math.Hypot(dx, p.Y); d > 4+1e-9 || d < 4-0.5-1e-9 && dx > 0 {
			t.Errorf("cap point %v at distance %.4f from the line", p, d)
		}
	}
	wantArea := 80 + 16*math.Pi
	if got := nonzeroArea(t, out); got > wantArea || got < wantArea-0.5*(2*math.Pi*4) {
		t.Errorf("coarse round caps area %.4f outside [%.4f, %.4f]", got, wantArea-4*math.Pi, wantArea)
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()
	line := polyline(false, pt(0, 0), pt(10, 0))
	for _, tc := range []struct {
		name string
		path geom.Path
		s    Style
		m    geom.Matrix
		want error
	}{
		{"negative width", line, Style{Width: -1}, geom.Identity, ErrInvalidStyle},
		{"NaN width", line, Style{Width: math.NaN()}, geom.Identity, ErrInvalidStyle},
		{"NaN point", polyline(false, pt(0, 0), pt(math.NaN(), 0)), Style{Width: 1}, geom.Identity, ErrNonFinite},
		{"Inf matrix", line, Style{Width: 1}, geom.Matrix{math.Inf(1), 0, 0, 1, 0, 0}, ErrNonFinite},
		{"overflowing matrix", line, Style{Width: 1}, geom.Matrix{1e200, 0, 0, 1e200, 0, 0}, ErrNonFinite},
		{"short points", geom.Path{Ops: []geom.Op{geom.MoveTo, geom.CubicTo}, Pts: []geom.Point{{}, {}}}, Style{Width: 1}, geom.Identity, ErrMalformedPath},
		{"too many dashes", polyline(false, pt(0, 0), pt(1e6, 0)), Style{Width: 1, Dash: []float64{1e-3}}, geom.Identity, ErrTooComplex},
	} {
		if _, err := Outline(tc.path, tc.s, tc.m, 0); !errors.Is(err, tc.want) {
			t.Errorf("%s: err %v, want %v", tc.name, err, tc.want)
		}
	}
}

// TestDegenerateInputs covers inputs that stroke to nothing or are ignored
// without an error.
func TestDegenerateInputs(t *testing.T) {
	t.Parallel()
	line := polyline(false, pt(0, 0), pt(10, 0))
	if out, err := Outline(line, Style{Width: 1}, geom.Matrix{0, 0, 0, 0, 3, 4}, 0); err != nil || !out.Empty() {
		t.Errorf("zero CTM: %v, %v", out, err)
	}
	if out, err := Outline(geom.Path{}, Style{Width: 1}, geom.Identity, 0); err != nil || !out.Empty() {
		t.Errorf("empty path: %v, %v", out, err)
	}
	// A segment with no current point is a content-stream error, ignored.
	var p geom.Path
	p.LineTo(pt(5, 5))
	p.MoveTo(pt(0, 0))
	p.LineTo(pt(10, 0))
	checkArea(t, "orphan lineto", nonzeroArea(t, outline(t, p, Style{Width: 2}, geom.Identity)), 20)
	// A segment after h opens a new subpath at the closed one's start.
	var q geom.Path
	q.MoveTo(pt(0, 0))
	q.LineTo(pt(10, 0))
	q.Close()
	q.LineTo(pt(0, 10))
	got := nonzeroArea(t, outline(t, q, Style{Width: 2, Join: BevelJoin}, geom.Identity))
	// The closed 10-unit back-and-forth stroke (two bevelled reversals: a
	// 10×2 box) plus the vertical 10×2 box, overlapping in a 1×1 square.
	checkArea(t, "lineto after close", got, 20+20-1)
	// A rank-1 CTM squashes the stroke to zero area.
	got = nonzeroArea(t, outline(t, line, Style{Width: 2}, geom.Matrix{1, 1, 1, 1, 0, 0}))
	checkArea(t, "rank-1 CTM", got, 0)
}

func TestMaxStretch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		m    geom.Matrix
		want float64
	}{
		{geom.Identity, 1},
		{geom.Matrix{3, 0, 0, -5, 1, 1}, 5},
		{geom.Matrix{0, 2, -2, 0, 0, 0}, 2},
		{geom.Matrix{1, 1, 1, 1, 0, 0}, 2},
	} {
		if got := maxStretch(tc.m); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("maxStretch(%v) = %v, want %v", tc.m, got, tc.want)
		}
	}
}
