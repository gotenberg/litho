package layout

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func shapeOutside(so css.ShapeOutside) func(*css.Style) {
	return func(s *css.Style) { s.ShapeOutside = &so }
}

func pctd(v float64) css.Dimension { return css.Dimension{Kind: css.Percent, Value: v / 100} }

func pxlen(v float64) css.Dimension { return css.Pts(px(v)) }

// strutless makes a block's strut empty: its lines are as tall as their
// inline blocks.
func strutless(s *css.Style) { s.FontSize, s.LineHeight = 0, css.Pts(0) }

// ib returns an inline block w by h CSS pixels.
func ib(w, h float64) *Node { return el(sty(display(css.InlineBlock), width(w), height(h))) }

// lineStarts returns the left edge and top of each line of b, in CSS
// pixels.
func lineStarts(b *box) [][2]float64 {
	var out [][2]float64
	for _, l := range b.lines {
		out = append(out, [2]float64{l.x / css.Pt, l.y / css.Pt})
	}
	return out
}

func TestShapeOutside_LinesWrapAroundTheFloatArea(t *testing.T) {
	// CSS Shapes 1, 2: the lines beside a float shorten to its float
	// area, not to its margin box.
	//
	// # Corpus evidence
	//
	//   - probe, html: Chromium starts a 250px-wide inline block beside
	//     circle(70px at 0 0) at 50px, 49px down; beside a triangle that
	//     frees room 33.3px down, 34px down at 66px.
	circle := &css.Shape{Kind: css.ShapeCircle, Radius: [2]css.ShapeRadius{{Side: css.RadiusLength, Length: pxlen(70)}}}
	triangle := &css.Shape{Kind: css.ShapePolygon, Points: [][2]css.Dimension{{pxlen(0), pxlen(0)}, {pxlen(100), pxlen(0)}, {pxlen(0), pxlen(100)}}}
	tests := []struct {
		name  string
		shape *css.Shape
		w     float64
		want  [2]float64
	}{
		{"circle", circle, 250, [2]float64{70 * math.Sqrt(1-49.0*49/4900), 49}},
		{"polygon", triangle, 233.3, [2]float64{66, 34}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, doc(el(sty(width(300)),
				el(sty(float(css.FloatLeft), width(100), height(100), shapeOutside(css.ShapeOutside{Set: true, Shape: tt.shape, Box: css.GeoMargin}))),
				ib(tt.w, 20))), page(400, 300))
			got := lineStarts(kid(t, rb, 0, 0))
			if len(got) != 1 || !near(got[0][1], tt.want[1]) || math.Abs(got[0][0]-tt.want[0]) > 0.01 {
				t.Errorf("lines %v, want one at %v", got, tt.want)
			}
		})
	}
}

func TestShapeOutside_MarginRoundsEveryCorner(t *testing.T) {
	// CSS Shapes 1, 6.3: shape-margin grows the shape to the contour at
	// its distance, which rounds a square corner; the area stays inside
	// the margin box.
	//
	// # Corpus evidence
	//
	//   - probe, html: beside a 100px float of margin 30px with
	//     shape-outside: border-box and shape-margin: 20px, Chromium
	//     starts 5px lines 0 at 0, 10 at 143, 15 at 147, 20 at 149, 25 to
	//     125 at 150, 145 at 143, 150 at 0.
	var kids []*Node
	kids = append(kids, el(sty(float(css.FloatLeft), width(100), height(100), margin(30),
		shapeOutside(css.ShapeOutside{Set: true, Box: css.GeoBorder, Margin: pxlen(20)}))))
	for range 40 {
		kids = append(kids, ib(200, 5))
	}
	rb := boxes(t, doc(el(sty(width(400), strutless), kids...)), page(500, 400))
	starts := map[float64]float64{}
	for _, s := range lineStarts(kid(t, rb, 0, 0)) {
		starts[math.Round(s[1])] = s[0]
	}
	want := map[float64]float64{0: 0, 10: 143.2, 15: 147.3, 20: 149.4, 25: 150, 125: 150, 145: 143.2, 150: 0}
	for y, x := range want {
		if got, ok := starts[y]; !ok || math.Abs(got-x) > 0.1 {
			t.Errorf("line at %v starts at %v, want %v", y, got, x)
		}
	}
}

func TestShapeOutside_FloatsPlaceByMarginBox(t *testing.T) {
	// CSS Shapes 1, 2: float positioning is not affected by the float
	// area; a second left float goes beside the first's margin box though
	// its shape leaves room.
	small := &css.Shape{Kind: css.ShapeCircle, Radius: [2]css.ShapeRadius{{Side: css.RadiusLength, Length: pxlen(10)}}, Center: [2]css.Dimension{pxlen(0), pxlen(0)}}
	rb := boxes(t, doc(el(sty(width(300)),
		el(sty(float(css.FloatLeft), width(100), height(100), shapeOutside(css.ShapeOutside{Set: true, Shape: small, Box: css.GeoMargin}))),
		el(sty(float(css.FloatLeft), width(50), height(50))))), page(400, 300))
	if got := rect(kid(t, rb, 0, 0, 1)); got[0] != 100 || got[1] != 0 {
		t.Errorf("second float at %v, want 100, 0", got[:2])
	}
}

func TestShapeOutside_ImageAlpha(t *testing.T) {
	// CSS Shapes 1, 4: an image's pixels over shape-image-threshold make
	// the float area. A gradient opaque over its upper left half frees
	// room for a 250px line once its edge is 50px in, a pixel row at a
	// time.
	g := &css.Gradient{Angle: 135, Stops: gstops(black, pctd(50), css.Color{}, pctd(50))}
	rb := boxes(t, doc(el(sty(width(300), strutless),
		el(sty(float(css.FloatLeft), width(100), height(100), shapeOutside(css.ShapeOutside{Set: true, Box: css.GeoContent, Image: css.MaskImage{Gradient: g}}))),
		ib(250, 20))), page(400, 300))
	got := lineStarts(kid(t, rb, 0, 0))
	if len(got) != 1 || got[0][1] < 49-eps || got[0][1] > 50+eps || got[0][0] > 50+eps {
		t.Errorf("lines %v, want one 49 or 50 down, at most 50 in", got)
	}
}

func TestShapeOutside_EmptyShapeLeavesLinesAlone(t *testing.T) {
	// Chromium's float area of a shape without area, inset(0 50%) a
	// vertical line, leaves the lines alone where CSS Shapes 1, 2 keeps
	// its edges.
	line := &css.Shape{Kind: css.ShapeInset, Inset: [4]css.Dimension{pxlen(0), pctd(50), pxlen(0), pctd(50)}}
	rb := boxes(t, doc(el(sty(width(300)),
		el(sty(float(css.FloatLeft), width(100), height(100), shapeOutside(css.ShapeOutside{Set: true, Shape: line, Box: css.GeoMargin}))),
		ib(250, 20))), page(400, 300))
	if got := lineStarts(kid(t, rb, 0, 0)); len(got) != 1 || got[0] != [2]float64{0, 0} {
		t.Errorf("lines %v, want one at 0, 0", got)
	}
}

func TestSegmentExtent_MatchesSampling(t *testing.T) {
	// The extent of a segment grown by a margin over a band is the span
	// of the points within the margin of the segment inside the band.
	segs := [][2]wrapPt{
		{{0, 0}, {10, 10}}, {{5, -3}, {5, 20}}, {{-4, 2}, {12, 2}}, {{0, 12}, {9, -1}}, {{3, 3}, {3, 3}},
	}
	for _, s := range segs {
		for _, band := range [][2]float64{{-6, -4}, {0, 1}, {4, 6}, {11, 13}, {14, 16}} {
			for _, m := range []float64{0, 1.5, 4} {
				lo, hi, ok := segmentExtent(s[0], s[1], band[0], band[1], m)
				slo, shi, sok := sampleExtent(s[0], s[1], band[0], band[1], m)
				if ok != sok || ok && (math.Abs(lo-slo) > 0.05 || math.Abs(hi-shi) > 0.05) {
					t.Errorf("segment %v band %v margin %v: %v %v %v, sampled %v %v %v", s, band, m, lo, hi, ok, slo, shi, sok)
				}
			}
		}
	}
}

// sampleExtent finds the extent segmentExtent computes by sampling the
// segment and the margin discs around its points.
func sampleExtent(a, b wrapPt, y0, y1, m float64) (float64, float64, bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	const n = 4000
	for i := 0; i <= n; i++ {
		t := float64(i) / n
		x, y := a.x+t*(b.x-a.x), a.y+t*(b.y-a.y)
		d := max(y0-y, y-y1, 0)
		if m == 0 {
			if y > y0 && y < y1 || y0 == y1 && y == y0 {
				lo, hi = min(lo, x), max(hi, x)
			}
			continue
		}
		if d < m || y0 == y1 && d == m {
			// A disc that touches a band of some height at its edge
			// alone leaves it free.
			r := math.Sqrt(m*m - d*d)
			lo, hi = min(lo, x-r), max(hi, x+r)
		}
	}
	return lo, hi, lo <= hi
}

func TestShapeOutside_EachFragmentTakesItsOwnBox(t *testing.T) {
	// A page break cuts a float's area in two, each sized to its own
	// fragment, as Chromium sizes them.
	//
	// # Corpus evidence
	//
	//   - probe, html: a 1000px float of polygon(0 0, 100% 100%, 0 100%)
	//     on 600px pages: Chromium starts the lines of the first page at
	//     200 (y + 20) / 600, those of the second at 200 (y - 580) / 400.
	tri := &css.Shape{Kind: css.ShapePolygon, Points: [][2]css.Dimension{{pxlen(0), pxlen(0)}, {pctd(100), pctd(100)}, {pxlen(0), pctd(100)}}}
	kids := []*Node{el(sty(float(css.FloatLeft), width(200), height(1000), shapeOutside(css.ShapeOutside{Set: true, Shape: tri, Box: css.GeoMargin})))}
	for range 80 {
		kids = append(kids, ib(150, 20))
	}
	rb := boxes(t, doc(el(sty(width(400), strutless), kids...)), page(400, 600))
	starts := map[float64]float64{}
	for _, s := range lineStarts(kid(t, rb, 0, 0)) {
		starts[math.Round(s[1])] = s[0]
	}
	for y, x := range map[float64]float64{0: 200 * 20.0 / 600, 580: 200, 600: 10, 620: 20} {
		if got := starts[y]; math.Abs(got-x) > 0.01 {
			t.Errorf("line at %v starts at %v, want %v", y, got, x)
		}
	}
}

func TestShapeOutside_PolygonRoundsItsVertices(t *testing.T) {
	// CSS Shapes 1, 3.1, polygon(): round <length> rounds each vertex of
	// the float area as it rounds a clip-path's, a circle tangent to both
	// sides, clamped to half of each side.
	//
	// # Corpus evidence
	//
	//   - probe, html: beside a 200px square float of
	//     polygon(round 100px, ...), Chromium starts 20px lines at 160,
	//     180, 192, 198 and 200.
	sq := func(v float64) [2]css.Dimension { return [2]css.Dimension{pxlen(v), pxlen(v)} }
	shape := &css.Shape{Kind: css.ShapePolygon, Round: px(100), Points: [][2]css.Dimension{
		sq(0), {pxlen(200), pxlen(0)}, sq(200), {pxlen(0), pxlen(200)},
	}}
	kids := []*Node{el(sty(float(css.FloatLeft), width(200), height(200), shapeOutside(css.ShapeOutside{Set: true, Shape: shape, Box: css.GeoMargin})))}
	for range 10 {
		kids = append(kids, ib(400, 20))
	}
	rb := boxes(t, doc(el(sty(width(800), strutless), kids...)), page(900, 400))
	got := lineStarts(kid(t, rb, 0, 0))
	for i, y := range []float64{20, 40, 60, 80, 100} {
		want := 100 + math.Sqrt(100*100-(100-y)*(100-y))
		if i >= len(got) || math.Abs(got[i][0]-want) > 0.2 {
			t.Errorf("line %d starts %v, want %v", i, got, want)
		}
	}
}

// TestSegmentExtent_MarginTouchLeavesBandFree locks the band of some
// height that the shape-margin of an edge reaches only at its top or
// bottom: it stays free, as a band an edge without a margin touches does,
// so a line whose bottom meets the grown shape of a polygon sits beside it
// (CSS Shapes 1, 6.3).
//
// # Corpus evidence
//
//   - wpt: css-shapes/shape-outside/supported-shapes/polygon/
//     shape-outside-polygon-012 to -015 and -032 draw a 20px line of
//     Ahem whose bottom meets a polygon grown by its shape-margin across
//     the whole float, as their references have it.
func TestSegmentExtent_MarginTouchLeavesBandFree(t *testing.T) {
	a, b := wrapPt{0, 10}, wrapPt{10, 10}
	if _, _, ok := segmentExtent(a, b, 0, 5, 5); ok {
		t.Error("a band touched at its bottom by the margin is taken")
	}
	if _, _, ok := segmentExtent(a, b, 15, 20, 5); ok {
		t.Error("a band touched at its top by the margin is taken")
	}
	if _, _, ok := segmentExtent(a, b, 0, 5.01, 5); !ok {
		t.Error("a band the margin reaches into is free")
	}
	if _, _, ok := segmentExtent(a, b, 5, 5, 5); !ok {
		t.Error("the line at the margin's edge is free")
	}
}
