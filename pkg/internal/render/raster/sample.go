package raster

import (
	"cmp"
	"image"
	"math"
	"slices"
)

// sampleScale is how many cells a pixel spans along each axis in the grid
// [Shape.SampleIn] tests: the 4 by 4 grid poppler's Splash anti-aliases on
// (splashAASize). A pixel is tested at the cell at its top-left corner.
const sampleScale = 4

// sampleSeg is one segment of a shape in sample cells, oriented top to
// bottom: (x0, y0) is its top end. dir is +1 or -1 by the way the path walks
// it.
type sampleSeg struct {
	x0, y0, x1, y1 float64
	dir            int
}

// sampleSpan is the run of cells, x0 to x1 inclusive, a segment crosses on
// one row of cells, and the winding it adds to the cells right of the run.
type sampleSpan struct {
	x0, x1 int
	count  int
}

// SampleIn returns, over box, a part of the raster, row by row, 1 where the
// shape reaches the pixel's sample cell and 0 elsewhere. The sample cell is
// the top-left quarter by quarter of a pixel, the first cell of the 4 by 4
// grid poppler anti-aliases on: a pixel is in when a segment of the shape
// crosses that cell, or when the cell's top-left corner lies inside under
// the fill rule, its winding counted over the segments that cross the
// cell row's top edge left of the cell.
//
// This is poppler's reading of a clipping path where it paints whole
// pixels, a glyph bitmap or a transparency group's result
// (SplashClip::test over SplashXPathScanner), not a spec rule: ISO
// 32000-1, 8.5.4 makes the clipping path an exact region and leaves its
// rasterisation to the renderer.
//
// The segments are sorted on the first call; each call then costs a pass
// over them, plus the segments each of box's rows crosses, plus box's
// pixels. It is safe for concurrent use, as the Shape is.
func (s *Shape) SampleIn(box image.Rectangle) []float32 {
	box = box.Intersect(image.Rect(0, 0, s.width, s.height))
	out := make([]float32, box.Dx()*box.Dy())
	if box.Empty() {
		return out
	}
	s.samplesOnce.Do(func() { s.samples = s.sampleSegs() })
	segs := s.samples
	if len(segs) == 0 {
		return out
	}
	w := box.Dx()
	// count[i] adds the winding of spans ending left of column box.Min.X+i;
	// touched[i] marks a column a span crosses.
	count := make([]int, w+1)
	touched := make([]bool, w)
	var (
		active []sampleSeg
		spans  []sampleSpan
		next   int
	)
	for y := box.Min.Y; y < box.Max.Y; y++ {
		row := float64(y * sampleScale)
		kept := active[:0]
		for _, sg := range active {
			if math.Floor(sg.y1) >= row {
				kept = append(kept, sg)
			}
		}
		active = kept
		for next < len(segs) && math.Floor(segs[next].y0) <= row {
			if math.Floor(segs[next].y1) >= row {
				active = append(active, segs[next])
			}
			next++
		}
		if len(active) == 0 {
			continue
		}
		spans = spans[:0]
		for _, sg := range active {
			spans = append(spans, sg.span(row))
		}
		clear(count)
		clear(touched)
		for _, sp := range spans {
			// The columns whose sample cell, at x*sampleScale, the span
			// covers.
			lo := ceilDiv(sp.x0, sampleScale) - box.Min.X
			hi := floorDiv(sp.x1, sampleScale) - box.Min.X
			for x := max(lo, 0); x <= min(hi, w-1); x++ {
				touched[x] = true
			}
			if sp.count != 0 {
				// The columns past the span see its winding.
				from := floorDiv(sp.x1, sampleScale) + 1 - box.Min.X
				count[min(max(from, 0), w)] += sp.count
			}
		}
		at := (y - box.Min.Y) * w
		winding := 0
		for x := range w {
			winding += count[x]
			if touched[x] || s.inside(winding) {
				out[at+x] = 1
			}
		}
	}
	return out
}

// inside reports whether a winding number lies inside under the shape's
// fill rule.
func (s *Shape) inside(winding int) bool {
	if s.rule == EvenOdd {
		return winding&1 != 0
	}
	return winding != 0
}

// span returns the cells sg crosses on the row of cells starting at row,
// and the winding it adds right of them: its direction when it crosses the
// row's top edge, nothing when it starts on the row.
func (sg sampleSeg) span(row float64) sampleSpan {
	var xa, xb float64
	if math.Floor(sg.y0) == math.Floor(sg.y1) {
		xa, xb = sg.x0, sg.x1
	} else {
		lo, hi := min(sg.x0, sg.x1), max(sg.x0, sg.x1)
		at := func(y float64) float64 {
			return min(max(sg.x0+(y-sg.y0)*(sg.x1-sg.x0)/(sg.y1-sg.y0), lo), hi)
		}
		xa = sg.x0
		if row > sg.y0 {
			xa = at(row)
		}
		xb = at(row + 1)
	}
	sp := sampleSpan{x0: cellOf(min(xa, xb)), x1: cellOf(max(xa, xb))}
	if sg.y0 < row {
		sp.count = sg.dir
	}
	return sp
}

// cellOf returns the cell holding x, clamped far outside any raster so the
// conversion cannot overflow.
func cellOf(x float64) int {
	const limit = 1 << 40
	return int(min(max(math.Floor(x), -limit), limit))
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

func ceilDiv(a, b int) int {
	return -floorDiv(-a, b)
}

// sampleSegs returns the shape's segments in sample cells, sorted by their
// top.
func (s *Shape) sampleSegs() []sampleSeg {
	var segs []sampleSeg
	for _, pl := range s.polys {
		n := len(pl.Pts)
		if n < 2 {
			continue
		}
		for i, a := range pl.Pts {
			b := pl.Pts[(i+1)%n]
			sg := sampleSeg{x0: a.X * sampleScale, y0: a.Y * sampleScale, x1: b.X * sampleScale, y1: b.Y * sampleScale, dir: 1}
			if sg.y0 > sg.y1 {
				sg = sampleSeg{x0: sg.x1, y0: sg.y1, x1: sg.x0, y1: sg.y0, dir: -1}
			}
			segs = append(segs, sg)
		}
	}
	slices.SortFunc(segs, func(p, q sampleSeg) int { return cmp.Compare(p.y0, q.y0) })
	return segs
}
