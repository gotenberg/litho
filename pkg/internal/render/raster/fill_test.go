package raster

import (
	"errors"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

func pt(x, y float64) geom.Point { return geom.Point{X: x, Y: y} }

func polygon(pts ...geom.Point) geom.Path {
	var p geom.Path
	p.MoveTo(pts[0])
	for _, q := range pts[1:] {
		p.LineTo(q)
	}
	p.Close()
	return p
}

func rect(x0, y0, x1, y1 float64) geom.Path {
	return polygon(pt(x0, y0), pt(x1, y0), pt(x1, y1), pt(x0, y1))
}

// circle builds a circle from four cubic arcs with the usual 0.5523 handle.
func circle(cx, cy, r float64) geom.Path {
	k := r * 4 * (math.Sqrt2 - 1) / 3
	var p geom.Path
	p.MoveTo(pt(cx+r, cy))
	p.CubicTo(pt(cx+r, cy+k), pt(cx+k, cy+r), pt(cx, cy+r))
	p.CubicTo(pt(cx-k, cy+r), pt(cx-r, cy+k), pt(cx-r, cy))
	p.CubicTo(pt(cx-r, cy-k), pt(cx-k, cy-r), pt(cx, cy-r))
	p.CubicTo(pt(cx+k, cy-r), pt(cx+r, cy-k), pt(cx+r, cy))
	p.Close()
	return p
}

func area(c *Coverage) float64 {
	var s float64
	for _, v := range c.A {
		s += float64(v)
	}
	return s
}

func mustFill(t *testing.T, p geom.Path, rule FillRule, w, h int, clip *surface.Mask) *Coverage {
	t.Helper()
	c, err := Fill(p, rule, w, h, clip)
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	for _, v := range c.A {
		if !(v >= 0 && v <= 1) {
			t.Fatalf("coverage value %v outside [0, 1]", v)
		}
	}
	return c
}

func TestFillAxisAlignedRectIsExact(t *testing.T) {
	t.Parallel()
	c := mustFill(t, rect(1.25, 2.5, 7.75, 6.1), NonZero, 10, 10, nil)
	if got, want := area(c), 6.5*3.6; math.Abs(got-want) > 1e-5 {
		t.Errorf("area = %v, want %v", got, want)
	}
	cases := []struct {
		x, y int
		want float64
	}{
		{1, 2, 0.75 * 0.5}, {4, 4, 1}, {7, 4, 0.75}, {7, 6, 0.75 * 0.1}, {4, 2, 0.5}, {0, 4, 0}, {8, 4, 0}, {4, 7, 0},
	}
	for _, tc := range cases {
		if got := float64(c.At(tc.x, tc.y)); math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("pixel (%d, %d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestFillHalfPixelEdges(t *testing.T) {
	t.Parallel()
	c := mustFill(t, rect(0.5, 0.5, 2.5, 1.5), NonZero, 4, 3, nil)
	want := [3][4]float64{{0.25, 0.5, 0.25, 0}, {0.25, 0.5, 0.25, 0}, {0, 0, 0, 0}}
	for y := range 3 {
		for x := range 4 {
			if got := float64(c.At(x, y)); math.Abs(got-want[y][x]) > 1e-6 {
				t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want[y][x])
			}
		}
	}
}

func TestFillRotatedRectIsExact(t *testing.T) {
	t.Parallel()
	for _, deg := range []float64{7, 30, 45, 63.5, 90, 151} {
		th := deg * math.Pi / 180
		m := geom.Matrix{math.Cos(th), math.Sin(th), -math.Sin(th), math.Cos(th), 50.3, 49.7}
		p := rect(-20, -7.5, 20, 7.5).Transform(m)
		c := mustFill(t, p, NonZero, 100, 100, nil)
		if got := area(c); math.Abs(got-600) > 1e-3 {
			t.Errorf("%v°: area = %v, want 600", deg, got)
		}
		// A pixel deep inside is fully covered whatever the angle.
		if got := c.At(50, 50); got != 1 {
			t.Errorf("%v°: centre pixel = %v, want 1", deg, got)
		}
	}
}

func TestFillCircleArea(t *testing.T) {
	t.Parallel()
	const r = 40
	c := mustFill(t, circle(50, 50, r), NonZero, 100, 100, nil)
	// The four-arc Bézier circle bulges outwards by at most 0.027 % of r, and
	// the inscribed chords stay within FillTolerance of the curve, so the
	// area lost is under the perimeter times the tolerance.
	want := math.Pi * r * r
	bound := 2*math.Pi*r*FillTolerance + 0.0003*want
	if got := area(c); math.Abs(got-want) > bound {
		t.Errorf("area = %v, want %v within %v", got, want, bound)
	}
	if c.At(50, 50) != 1 || c.At(50+r+1, 50) != 0 {
		t.Errorf("centre %v, outside %v, want 1 and 0", c.At(50, 50), c.At(50+r+1, 50))
	}
}

func star(cx, cy, r float64) geom.Path {
	var pts []geom.Point
	for i := range 5 {
		th := float64(i*2)*2*math.Pi/5 - math.Pi/2
		pts = append(pts, pt(cx+r*math.Cos(th), cy+r*math.Sin(th)))
	}
	return polygon(pts...)
}

// TestFillStarRules draws a pentagram, whose centre pentagon is wound twice:
// nonzero fills it, even-odd leaves it empty (ISO 32000-1, 8.5.3.3).
func TestFillStarRules(t *testing.T) {
	t.Parallel()
	nz := mustFill(t, star(50, 50, 40), NonZero, 100, 100, nil)
	eo := mustFill(t, star(50, 50, 40), EvenOdd, 100, 100, nil)
	if nz.At(50, 50) != 1 {
		t.Errorf("nonzero centre = %v, want 1", nz.At(50, 50))
	}
	if eo.At(50, 50) != 0 {
		t.Errorf("even-odd centre = %v, want 0", eo.At(50, 50))
	}
	// A star point is wound once, so both rules fill it.
	if nz.At(50, 14) != 1 || eo.At(50, 14) != 1 {
		t.Errorf("star point = %v / %v, want 1 under both rules", nz.At(50, 14), eo.At(50, 14))
	}
	// The pentagram with outer radius R has inner pentagon area
	// (5/2)·ri²·sin 72°, ri = R·sin 18°/sin 126°; nonzero minus even-odd is
	// exactly that pentagon.
	ri := 40 * math.Sin(math.Pi/10) / math.Sin(7*math.Pi/10)
	pent := 2.5 * ri * ri * math.Sin(2*math.Pi/5)
	if got := area(nz) - area(eo); math.Abs(got-pent)/pent > 1e-2 {
		t.Errorf("nonzero - evenodd = %v, want the inner pentagon %v", got, pent)
	}
}

// TestFillWindingDirectionMatters checks two nested squares: wound the same
// way, nonzero fills the hole; wound opposite ways, both rules leave it.
func TestFillWindingDirectionMatters(t *testing.T) {
	t.Parallel()
	outer := rect(0, 0, 10, 10)
	same := rect(3, 3, 7, 7)
	opposite := polygon(pt(3, 3), pt(3, 7), pt(7, 7), pt(7, 3))
	join := func(a, b geom.Path) geom.Path {
		return geom.Path{Ops: append(append([]geom.Op{}, a.Ops...), b.Ops...), Pts: append(append([]geom.Point{}, a.Pts...), b.Pts...)}
	}
	if got := area(mustFill(t, join(outer, same), NonZero, 10, 10, nil)); got != 100 {
		t.Errorf("nonzero, same direction: %v, want 100", got)
	}
	if got := area(mustFill(t, join(outer, opposite), NonZero, 10, 10, nil)); got != 84 {
		t.Errorf("nonzero, opposite direction: %v, want 84", got)
	}
	if got := area(mustFill(t, join(outer, same), EvenOdd, 10, 10, nil)); got != 84 {
		t.Errorf("even-odd: %v, want 84", got)
	}
}

func TestFillOutsideAndPartlyOutside(t *testing.T) {
	t.Parallel()
	if c := mustFill(t, rect(-100, -100, -1, 50), NonZero, 10, 10, nil); !c.Empty() {
		t.Errorf("a path left of the raster covered %v", area(c))
	}
	if c := mustFill(t, rect(20, 0, 30, 5), NonZero, 10, 10, nil); !c.Empty() {
		t.Errorf("a path right of the raster covered %v", area(c))
	}
	c := mustFill(t, rect(-100, -3, 5.5, 4), NonZero, 10, 10, nil)
	if got := area(c); math.Abs(got-22) > 1e-6 {
		t.Errorf("partly outside: area = %v, want 22", got)
	}
	c = mustFill(t, rect(-1e300, -1e300, 1e300, 1e300), NonZero, 10, 10, nil)
	if got := area(c); math.Abs(got-100) > 1e-6 {
		t.Errorf("huge rectangle: area = %v, want 100", got)
	}
	// A triangle with a vertex far away crosses the raster along its edges.
	c = mustFill(t, polygon(pt(0, 0), pt(1e18, 1e18), pt(0, 1e18)), NonZero, 10, 10, nil)
	if got := area(c); math.Abs(got-50) > 1e-3 {
		t.Errorf("far vertex: area = %v, want 50", got)
	}
	// A huge curve crossing the raster is capped, not exploded.
	var p geom.Path
	p.MoveTo(pt(-1e300, 5))
	p.CubicTo(pt(0, -1e300), pt(0, 1e300), pt(1e300, 5))
	if _, err := Fill(p, NonZero, 10, 10, nil); err != nil {
		t.Errorf("huge curve: %v", err)
	}
}

func TestFillDegenerate(t *testing.T) {
	t.Parallel()
	var single geom.Path
	single.MoveTo(pt(3, 3))
	line := polygon(pt(1, 1), pt(8, 8))
	for name, p := range map[string]geom.Path{"empty": {}, "point": single, "line": line, "flat": rect(1, 4, 8, 4)} {
		if c := mustFill(t, p, NonZero, 10, 10, nil); area(c) != 0 {
			t.Errorf("%s: area = %v, want 0", name, area(c))
		}
	}
	// An open subpath is closed implicitly (ISO 32000-1, 8.5.3.1).
	var open geom.Path
	open.MoveTo(pt(0, 0))
	open.LineTo(pt(4, 0))
	open.LineTo(pt(4, 4))
	if got := area(mustFill(t, open, NonZero, 10, 10, nil)); math.Abs(got-8) > 1e-6 {
		t.Errorf("open triangle: area = %v, want 8", got)
	}
}

func TestFillRejects(t *testing.T) {
	t.Parallel()
	nan := rect(0, 0, math.NaN(), 4)
	if _, err := Fill(nan, NonZero, 10, 10, nil); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("NaN: err = %v, want ErrInvalidPath", err)
	}
	if _, err := Fill(rect(0, 0, 1, 1), FillRule(9), 10, 10, nil); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("unknown rule: err = %v, want ErrInvalidPath", err)
	}
	if _, err := Fill(rect(0, 0, 1, 1), NonZero, 0, 10, nil); !errors.Is(err, ErrSizeMismatch) {
		t.Errorf("zero width: err = %v, want ErrSizeMismatch", err)
	}
	m, _ := surface.NewMask(5, 5, 25)
	if _, err := Fill(rect(0, 0, 1, 1), NonZero, 10, 10, m); !errors.Is(err, ErrSizeMismatch) {
		t.Errorf("clip size: err = %v, want ErrSizeMismatch", err)
	}
	short := geom.Path{Ops: []geom.Op{geom.MoveTo, geom.CubicTo}, Pts: []geom.Point{{}, {}}}
	if _, err := Fill(short, NonZero, 10, 10, nil); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("missing points: err = %v, want ErrInvalidPath", err)
	}
	var many geom.Path
	many.MoveTo(pt(0, 0))
	for i := range maxFlattenSegments + 1 {
		many.LineTo(pt(float64(i%10), float64(i%7)))
	}
	if _, err := Fill(many, NonZero, 10, 10, nil); !errors.Is(err, ErrTooComplex) {
		t.Errorf("too many segments: err = %v, want ErrTooComplex", err)
	}
}

func TestClipIntersects(t *testing.T) {
	t.Parallel()
	first, err := Clip(rect(0, 0, 6, 6), NonZero, nil, 10, 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Clip(rect(3.5, 2, 10, 10), EvenOdd, first, 10, 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	var s float64
	for _, v := range second.A {
		s += float64(v)
	}
	if want := 2.5 * 4; math.Abs(s-want) > 1e-6 {
		t.Errorf("clip area = %v, want %v", s, want)
	}
	if first.At(1, 1) != 1 || second.At(1, 1) != 0 || second.At(3, 3) != 0.5 {
		t.Errorf("clip pixels: first(1,1) %v, second(1,1) %v, second(3,3) %v", first.At(1, 1), second.At(1, 1), second.At(3, 3))
	}
	// Filling through the clip yields the triple intersection.
	c := mustFill(t, rect(0, 0, 10, 4), NonZero, 10, 10, second)
	if got := area(c); math.Abs(got-2.5*2) > 1e-6 {
		t.Errorf("fill through clip: area = %v, want 5", got)
	}
	if _, err := Clip(rect(0, 0, 1, 1), NonZero, nil, 10, 10, 50); !errors.Is(err, surface.ErrTooLarge) {
		t.Errorf("over budget: err = %v, want ErrTooLarge", err)
	}
}

func TestPaintSolid(t *testing.T) {
	t.Parallel()
	c, err := surface.NewCanvas(4, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	c.Fill(1, 1, 1, 1)
	cov := mustFill(t, rect(0.5, -1, 2, 2), NonZero, 4, 1, nil)
	PaintSolid(c, cov, 0, 0, 0, 0.5)
	for x, want := range []float32{0.75, 0.5, 1, 1} {
		if r, _, _, a := c.At(x, 0); math.Abs(float64(r-want)) > 1e-6 || a != 1 {
			t.Errorf("pixel %d = (r %v, a %v), want (r %v, a 1)", x, r, a, want)
		}
	}
	PaintSolid(c, nil, 0, 0, 0, 1)
	PaintSolid(c, &Coverage{X: -2, Y: -2, Width: 1, Height: 1, A: []float32{1}}, 0, 0, 0, 1)
}

func TestFlatten(t *testing.T) {
	t.Parallel()
	var p geom.Path
	p.MoveTo(pt(0, 0))
	p.CubicTo(pt(0, 100), pt(100, 100), pt(100, 0))
	p.Close()
	p.LineTo(pt(5, 5))
	pls, err := Flatten(p, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pls) != 2 || !pls[0].Closed || pls[1].Closed {
		t.Fatalf("polylines = %d (closed %v), want a closed one, then an open one", len(pls), pls[0].Closed)
	}
	if pls[1].Pts[0] != pt(0, 0) {
		t.Errorf("segment after h starts at %v, want the subpath start (0, 0)", pls[1].Pts[0])
	}
	// Every chord midpoint stays within the tolerance of the true curve,
	// whose parameter at a chord midpoint is near the mean of its ends.
	pts := pls[0].Pts
	n := len(pts) - 1
	for i := range n {
		mid := pt((pts[i].X+pts[i+1].X)/2, (pts[i].Y+pts[i+1].Y)/2)
		on := bezier(pt(0, 0), pt(0, 100), pt(100, 100), pt(100, 0), (float64(i)+0.5)/float64(n))
		if d := math.Hypot(mid.X-on.X, mid.Y-on.Y); d > 0.1 {
			t.Errorf("chord %d strays %v from the curve", i, d)
		}
	}
	if _, err := Flatten(p, 0); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("zero tolerance: err = %v, want ErrInvalidPath", err)
	}
}
