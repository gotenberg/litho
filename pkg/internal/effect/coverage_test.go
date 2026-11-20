package effect

import (
	"context"
	"errors"
	"image"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/raster"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
)

func TestFill_Coverage(t *testing.T) {
	line := []typeset.PathOp{{Kind: typeset.MoveTo, P: pt3(-2, 2)}, {Kind: typeset.LineTo, P: pt3(6, 2)}}
	for _, tc := range []struct {
		name  string
		paths []Path
		tr    typeset.Matrix
		want  []uint8
	}{
		{"square", []Path{{Ops: rect(1, 1, 3, 3)}}, typeset.Identity, []uint8{
			0, 0, 0, 0,
			0, 255, 255, 0,
			0, 255, 255, 0,
			0, 0, 0, 0,
		}},
		{"scaled", []Path{{Ops: rect(0.5, 0.5, 1.5, 1.5)}}, typeset.Matrix{A: 2, D: 2}, []uint8{
			0, 0, 0, 0,
			0, 255, 255, 0,
			0, 255, 255, 0,
			0, 0, 0, 0,
		}},
		{"half pixel", []Path{{Ops: rect(0, 0, 0.5, 4)}}, typeset.Identity, []uint8{
			128, 0, 0, 0,
			128, 0, 0, 0,
			128, 0, 0, 0,
			128, 0, 0, 0,
		}},
		{"quarter pixel", []Path{{Ops: rect(1.5, 1.5, 2, 2)}}, typeset.Identity, []uint8{
			0, 0, 0, 0,
			0, 64, 0, 0,
			0, 0, 0, 0,
			0, 0, 0, 0,
		}},
		{"opposite subpaths cancel", []Path{{Ops: append(rect(0, 0, 4, 4), reverse(rect(1, 1, 3, 3))...)}}, typeset.Identity, []uint8{
			255, 255, 255, 255,
			255, 0, 0, 255,
			255, 0, 0, 255,
			255, 255, 255, 255,
		}},
		{"same subpaths union", []Path{{Ops: append(rect(0, 0, 4, 4), rect(1, 1, 3, 3)...)}}, typeset.Identity, slices.Repeat([]uint8{255}, 16)},
		{"even-odd hole", []Path{{Ops: append(rect(0, 0, 4, 4), rect(1, 1, 3, 3)...), Rule: EvenOdd}}, typeset.Identity, []uint8{
			255, 255, 255, 255,
			255, 0, 0, 255,
			255, 0, 0, 255,
			255, 255, 255, 255,
		}},
		{"paths union", []Path{{Ops: rect(0, 0, 2, 4)}, {Ops: reverse(rect(1, 0, 4, 4))}}, typeset.Identity, slices.Repeat([]uint8{255}, 16)},
		{"past the left edge", []Path{{Ops: rect(-100, 1, 1, 2)}}, typeset.Identity, []uint8{
			0, 0, 0, 0,
			255, 0, 0, 0,
			0, 0, 0, 0,
			0, 0, 0, 0,
		}},
		{"past every edge", []Path{{Ops: rect(-1e5, -1e5, 1e5, 1e5)}}, typeset.Identity, slices.Repeat([]uint8{255}, 16)},
		{"stroke", []Path{{Ops: line, Width: 2}}, typeset.Identity, []uint8{
			0, 0, 0, 0,
			255, 255, 255, 255,
			255, 255, 255, 255,
			0, 0, 0, 0,
		}},
		{"non-finite skipped", []Path{{Ops: rect(0, 0, 1e30, 4)}}, typeset.Identity, make([]uint8, 16)},
		{"lineto before moveto skipped", []Path{{Ops: append([]typeset.PathOp{{Kind: typeset.LineTo, P: pt3(4, 4)}}, rect(1, 1, 3, 3)...)}}, typeset.Identity, []uint8{
			0, 0, 0, 0,
			0, 255, 255, 0,
			0, 255, 255, 0,
			0, 0, 0, 0,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := filled(t, 4, 4, tc.tr, tc.paths...)
			if !slices.Equal(s.Pix, tc.want) {
				t.Errorf("coverage = %v, want %v", s.Pix, tc.want)
			}
		})
	}
}

// TestFill_ExactArea compares the fixed-point coverage of filled paths
// with their exact area, computed in floating point by clipping each
// subpath, its curves cut into 512 chords, to each pixel: they agree
// within one level. It logs how far raster.Draw, on
// golang.org/x/image/vector, lands from the same areas.
func TestFill_ExactArea(t *testing.T) {
	for _, tc := range []struct {
		name string
		ops  []typeset.PathOp
		tr   typeset.Matrix
	}{
		{"ellipse", ellipse(60, 40, 50, 30), typeset.Identity},
		{"ellipse scaled", ellipse(12, 8, 10, 6), typeset.Matrix{A: 5, D: 5}},
		{"small ellipse", ellipse(10.3, 9.6, 3.2, 2.1), typeset.Identity},
		{"star", star(60, 40, 38), typeset.Identity},
		{"rotated rectangle", rect(20, 10, 80, 50), typeset.Matrix{A: 0.8, B: 0.6, C: -0.6, D: 0.8, E: 40, F: -20}},
		{"subpixel rectangle", rect(10.3, 10.7, 90.2, 60.45), typeset.Identity},
		{"thin sliver", rect(-5, 20.1, 130, 20.3), typeset.Matrix{A: 1, B: 0.05, D: 1}},
		{"past the edges", ellipse(60, 40, 90, 70), typeset.Identity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := filled(t, 120, 80, tc.tr, Path{Ops: tc.ops})
			want := exactCoverage(tc.ops, tc.tr, 120, 80)
			m := image.NewAlpha(image.Rect(0, 0, 120, 80))
			raster.Draw(m, []raster.Path{{Ops: tc.ops}}, tc.tr)
			worst, rworst := 0.0, 0.0
			for i := range s.Pix {
				worst = max(worst, math.Abs(float64(s.Pix[i])-want[i]))
				rworst = max(rworst, math.Abs(float64(m.Pix[i])-want[i]))
			}
			t.Logf("effect within %.2f levels of the exact area, raster.Draw within %.2f", worst, rworst)
			if worst > 1 {
				t.Errorf("coverage off the exact area by %.2f levels, want at most 1", worst)
			}
		})
	}
}

// exactCoverage returns the area of ops mapped by tr in each pixel of a w
// by h grid, in levels of 255: the signed areas of the subpaths summed,
// the magnitude clamped to the pixel.
func exactCoverage(ops []typeset.PathOp, tr typeset.Matrix, w, h int) []float64 {
	type p2 struct{ x, y float64 }
	at := func(p typeset.Point) p2 { return p2{tr.A*p.X + tr.C*p.Y + tr.E, tr.B*p.X + tr.D*p.Y + tr.F} }
	var polys [][]p2
	for _, op := range ops {
		switch op.Kind {
		case typeset.MoveTo:
			polys = append(polys, []p2{at(op.P[0])})
		case typeset.LineTo:
			polys[len(polys)-1] = append(polys[len(polys)-1], at(op.P[0]))
		case typeset.CurveTo:
			cur := polys[len(polys)-1]
			a, b, c, d := cur[len(cur)-1], at(op.P[0]), at(op.P[1]), at(op.P[2])
			for i := 1; i <= 512; i++ {
				u := float64(i) / 512
				v := 1 - u
				k0, k1, k2, k3 := v*v*v, 3*v*v*u, 3*v*u*u, u*u*u
				cur = append(cur, p2{k0*a.x + k1*b.x + k2*c.x + k3*d.x, k0*a.y + k1*b.y + k2*c.y + k3*d.y})
			}
			polys[len(polys)-1] = cur
		case typeset.ClosePath:
		}
	}
	clip := func(poly []p2, axis int, v float64, keepAbove bool) []p2 {
		get := func(p p2) float64 {
			if axis == 0 {
				return p.x
			}
			return p.y
		}
		in := func(p p2) bool { return get(p) >= v == keepAbove || get(p) == v }
		var out []p2
		for i := range poly {
			a, b := poly[(i+len(poly)-1)%len(poly)], poly[i]
			cross := func() p2 {
				t := (v - get(a)) / (get(b) - get(a))
				return p2{a.x + t*(b.x-a.x), a.y + t*(b.y-a.y)}
			}
			if in(b) {
				if !in(a) {
					out = append(out, cross())
				}
				out = append(out, b)
			} else if in(a) {
				out = append(out, cross())
			}
		}
		return out
	}
	out := make([]float64, w*h)
	for y := range h {
		for x := range w {
			sum := 0.0
			for _, poly := range polys {
				q := clip(clip(clip(clip(poly, 0, float64(x), true), 0, float64(x+1), false), 1, float64(y), true), 1, float64(y+1), false)
				for i := range q {
					j := (i + 1) % len(q)
					sum += q[i].x*q[j].y - q[j].x*q[i].y
				}
			}
			out[y*w+x] = min(math.Abs(sum)/2, 1) * 255
		}
	}
	return out
}

// TestFill_StrokeMatchesSamples compares stroked paths with the fraction
// of 64 by 64 sample points of each pixel within half the width of the
// path: within one level. It logs how far raster.Draw lands from the
// same fractions.
func TestFill_StrokeMatchesSamples(t *testing.T) {
	for _, tc := range []struct {
		name string
		ops  []typeset.PathOp
		w    float64
	}{
		{"diagonal line", []typeset.PathOp{{Kind: typeset.MoveTo, P: pt3(4.3, 5.1)}, {Kind: typeset.LineTo, P: pt3(33.6, 27.4)}}, 4.5},
		{"ring", ellipse(20, 16, 12, 9), 3},
		{"thin ring", ellipse(20, 16, 12, 9), 1},
		{"open zigzag", []typeset.PathOp{
			{Kind: typeset.MoveTo, P: pt3(3, 25)}, {Kind: typeset.LineTo, P: pt3(12, 5)}, {Kind: typeset.LineTo, P: pt3(21, 25)},
			{Kind: typeset.LineTo, P: pt3(30, 6)}, {Kind: typeset.LineTo, P: pt3(37, 20)},
		}, 2.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const w, h, n = 40, 32, 64
			s := filled(t, w, h, typeset.Identity, Path{Ops: tc.ops, Width: tc.w})
			m := image.NewAlpha(image.Rect(0, 0, w, h))
			raster.Draw(m, []raster.Path{{Ops: tc.ops, Width: tc.w}}, typeset.Identity)
			segs := sampleSegments(tc.ops)
			worst, rworst := 0.0, 0.0
			near := make([][4]float64, 0, len(segs))
			for y := range h {
				for x := range w {
					near = near[:0]
					reach := tc.w/2 + 1
					for _, sg := range segs {
						if min(sg[0], sg[2])-reach <= float64(x+1) && max(sg[0], sg[2])+reach >= float64(x) &&
							min(sg[1], sg[3])-reach <= float64(y+1) && max(sg[1], sg[3])+reach >= float64(y) {
							near = append(near, sg)
						}
					}
					in := 0
					for sy := range n {
						for sx := range n {
							px, py := float64(x)+(float64(sx)+0.5)/n, float64(y)+(float64(sy)+0.5)/n
							for _, sg := range near {
								if segDist(px, py, sg) <= tc.w/2 {
									in++
									break
								}
							}
						}
					}
					want := float64(in) / (n * n) * 255
					worst = max(worst, math.Abs(float64(s.Pix[y*w+x])-want))
					rworst = max(rworst, math.Abs(float64(m.Pix[y*w+x])-want))
				}
			}
			t.Logf("effect within %.2f levels of the samples, raster.Draw within %.2f", worst, rworst)
			if worst > 1 {
				t.Errorf("stroke off the samples by %.2f levels, want at most 1", worst)
			}
		})
	}
}

// sampleSegments returns the segments of ops, curves cut into 256
// chords.
func sampleSegments(ops []typeset.PathOp) [][4]float64 {
	var out [][4]float64
	var start, pen typeset.Point
	for _, op := range ops {
		switch op.Kind {
		case typeset.MoveTo:
			start, pen = op.P[0], op.P[0]
		case typeset.LineTo:
			out = append(out, [4]float64{pen.X, pen.Y, op.P[0].X, op.P[0].Y})
			pen = op.P[0]
		case typeset.CurveTo:
			a, b, c, d := pen, op.P[0], op.P[1], op.P[2]
			for i := 1; i <= 256; i++ {
				u := float64(i) / 256
				v := 1 - u
				k0, k1, k2, k3 := v*v*v, 3*v*v*u, 3*v*u*u, u*u*u
				q := typeset.Point{X: k0*a.X + k1*b.X + k2*c.X + k3*d.X, Y: k0*a.Y + k1*b.Y + k2*c.Y + k3*d.Y}
				out = append(out, [4]float64{pen.X, pen.Y, q.X, q.Y})
				pen = q
			}
		case typeset.ClosePath:
			out = append(out, [4]float64{pen.X, pen.Y, start.X, start.Y})
			pen = start
		}
	}
	return out
}

// segDist returns the distance from (x, y) to the segment sg.
func segDist(x, y float64, sg [4]float64) float64 {
	dx, dy := sg[2]-sg[0], sg[3]-sg[1]
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = min(max(((x-sg[0])*dx+(y-sg[1])*dy)/l, 0), 1)
	}
	return math.Hypot(x-sg[0]-t*dx, y-sg[1]-t*dy)
}

func TestFill_Area(t *testing.T) {
	// A disc of radius 30 pixels covers pi 900 = 2827.4 pixels.
	s := filled(t, 80, 80, typeset.Identity, Path{Ops: ellipse(40, 40, 30, 30)})
	sum := 0
	for _, v := range s.Pix {
		sum += int(v)
	}
	if got := float64(sum) / 255; got < 2826 || got > 2829 {
		t.Errorf("disc area = %.1f pixels, want 2827.4 within 1.5", got)
	}
}

func TestFill_Refusals(t *testing.T) {
	ctx := context.Background()
	rgba := newSurface(t, RGBA, 2, 2)
	if err := Fill(ctx, rgba, nil, typeset.Identity); !errors.Is(err, ErrFormat) {
		t.Errorf("Fill on RGBA = %v, want ErrFormat", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	s := newSurface(t, Coverage, 4, 4)
	if err := Fill(cctx, s, []Path{{Ops: rect(0, 0, 4, 4)}}, typeset.Identity); !errors.Is(err, context.Canceled) {
		t.Errorf("Fill on a cancelled context = %v, want context.Canceled", err)
	}
	bctx, b, done := limits.WithBudget(ctx, models.Limits{MaxConvertMemoryBytes: 1 << 16})
	defer done()
	big, err := New(bctx, Coverage, 200, 200, Grid{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	many := make([]typeset.PathOp, 0, 20000)
	for i := range 4000 {
		many = append(many, rect(float64(i%200), 0, float64(i%200)+0.5, 200)...)
	}
	if err := Fill(bctx, big, []Path{{Ops: many}}, typeset.Identity); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("Fill past the budget = %v, want ErrMemoryBudget", err)
	}
	big.Release()
	if b.Held() != 0 {
		t.Errorf("budget holds %d bytes after the refusal and the release, want 0", b.Held())
	}
}
