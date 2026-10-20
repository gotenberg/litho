package raster

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// maxFlattenSegments caps the line segments one path flattens to. Anti-DoS
// guard, not spec-mandated: a content stream can repeat path operators
// without limit, and every segment costs an edge (under 100 bytes) in the
// scan converter, so the cap bounds one fill near 100 MB. A dense vector map
// holds tens of thousands of segments per path, far below it.
const maxFlattenSegments = 1 << 20

// maxCurveSegments caps the segments one cubic flattens to. Anti-DoS guard,
// not spec-mandated: the count grows with the square root of the curve's
// size over the tolerance, so a curve with control points at 1e300 would ask
// for an astronomic count. A curve spanning a 600 dpi page at a 0.1 pixel
// tolerance needs about 500 segments, so real curves never reach the cap; a
// capped curve stays a valid polygon through its control points' chords,
// only coarser than the tolerance.
const maxCurveSegments = 1 << 12

// Polyline is one flattened subpath: the points it visits in order, and
// whether the path closed it with h (ISO 32000-1, 8.5.2.1). An open polyline
// is still closed implicitly when filled (8.5.3.1); stroking draws it open.
type Polyline struct {
	Pts    []geom.Point
	Closed bool
}

// Flatten replaces every cubic of p by line segments that stay within
// tolerance of the curve, in the units of p's coordinates, and returns the
// subpaths as polylines. It fails with [ErrInvalidPath] on a coordinate that
// is not finite, a tolerance that is not positive, or an operation missing its
// points, and with [ErrTooComplex] when the result would exceed the segment
// budget.
func Flatten(p geom.Path, tolerance float64) ([]Polyline, error) {
	f := flattener{tol: tolerance}
	if err := f.run(p); err != nil {
		return nil, err
	}
	return f.out, nil
}

// flattener walks a path and builds its polylines. cull, when set, is the
// raster rectangle: a cubic whose control hull lies wholly outside it
// flattens to its chord, since it cannot cover a pixel (see cubic).
type flattener struct {
	tol      float64
	cull     *geom.Rect
	out      []Polyline
	segments int
}

func (f *flattener) run(p geom.Path) error {
	if !(f.tol > 0) || math.IsInf(f.tol, 0) {
		return fmt.Errorf("%w: flattening tolerance %v", ErrInvalidPath, f.tol)
	}
	var (
		cur      *Polyline
		start    geom.Point
		hasStart bool
		pi       int
	)
	// begin opens a new polyline at pt. Per ISO 32000-1, 8.5.2.1, a segment
	// appended after h starts a new subpath at the closed one's start, and a
	// segment with no current point (a malformed m-less path) starts at its
	// own first point instead of failing the whole path.
	begin := func(pt geom.Point) {
		f.out = append(f.out, Polyline{Pts: []geom.Point{pt}})
		cur = &f.out[len(f.out)-1]
	}
	for i, op := range p.Ops {
		need := 0
		switch op {
		case geom.MoveTo, geom.LineTo:
			need = 1
		case geom.CubicTo:
			need = 3
		case geom.Close:
		default:
			return fmt.Errorf("%w: unknown path operation %d at step %d", ErrInvalidPath, op, i)
		}
		if pi+need > len(p.Pts) {
			return fmt.Errorf("%w: step %d needs %d points, %d left", ErrInvalidPath, i, need, len(p.Pts)-pi)
		}
		pts := p.Pts[pi : pi+need]
		pi += need
		for _, pt := range pts {
			if !finite(pt) {
				return fmt.Errorf("%w: coordinate (%v, %v) at step %d", ErrInvalidPath, pt.X, pt.Y, i)
			}
		}
		switch op {
		case geom.MoveTo:
			begin(pts[0])
			start, hasStart = pts[0], true
		case geom.LineTo, geom.CubicTo:
			if cur == nil {
				if hasStart {
					begin(start)
				} else {
					begin(pts[0])
					start, hasStart = pts[0], true
				}
			}
			if op == geom.LineTo {
				if err := f.add(cur, pts[0]); err != nil {
					return err
				}
				continue
			}
			if err := f.cubic(cur, cur.Pts[len(cur.Pts)-1], pts[0], pts[1], pts[2]); err != nil {
				return err
			}
		case geom.Close:
			if cur != nil {
				cur.Closed = true
				cur = nil
			}
		}
	}
	return nil
}

func (f *flattener) add(pl *Polyline, pt geom.Point) error {
	f.segments++
	if f.segments > maxFlattenSegments {
		return fmt.Errorf("%w: more than %d segments", ErrTooComplex, maxFlattenSegments)
	}
	pl.Pts = append(pl.Pts, pt)
	return nil
}

// cubic appends the flattening of the Bézier p0 p1 p2 p3. The segment count
// comes from Wang's bound: n uniform steps keep the chords within
// sqrt(3/4 · M / n²) of the curve, M the larger second difference of the
// control points, so n = ceil(sqrt(0.75 · M / tol)) meets the tolerance.
// Uniform steps keep the output a pure function of the input.
func (f *flattener) cubic(pl *Polyline, p0, p1, p2, p3 geom.Point) error {
	if f.cull != nil && hullOutside(*f.cull, p0, p1, p2, p3) {
		return f.add(pl, p3)
	}
	// Halved differences keep control points near the float64 limit from
	// overflowing to infinity.
	dd := max(
		math.Hypot(p0.X/2-p1.X+p2.X/2, p0.Y/2-p1.Y+p2.Y/2),
		math.Hypot(p1.X/2-p2.X+p3.X/2, p1.Y/2-p2.Y+p3.Y/2),
	)
	n := 1
	if est := math.Ceil(math.Sqrt(1.5 * dd / f.tol)); est >= maxCurveSegments || math.IsNaN(est) {
		n = maxCurveSegments
	} else if est > 1 {
		n = int(est)
	}
	for i := 1; i < n; i++ {
		t := float64(i) / float64(n)
		if err := f.add(pl, bezier(p0, p1, p2, p3, t)); err != nil {
			return err
		}
	}
	return f.add(pl, p3)
}

// bezier evaluates the cubic at t in Bernstein form, which stays finite for
// finite control points.
func bezier(p0, p1, p2, p3 geom.Point, t float64) geom.Point {
	u := 1 - t
	b0, b1, b2, b3 := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	return geom.Point{
		X: b0*p0.X + b1*p1.X + b2*p2.X + b3*p3.X,
		Y: b0*p0.Y + b1*p1.Y + b2*p2.Y + b3*p3.Y,
	}
}

// hullOutside reports whether the four control points lie wholly above,
// below, left or right of r. A curve inside its control hull then covers no
// pixel of r, and its chord crosses every horizontal line the same signed
// number of times (the signed count depends on the endpoints only), so the
// chord leaves the winding numbers inside r unchanged.
func hullOutside(r geom.Rect, pts ...geom.Point) bool {
	above, below, left, right := true, true, true, true
	for _, p := range pts {
		above = above && p.Y <= r.Min.Y
		below = below && p.Y >= r.Max.Y
		left = left && p.X <= r.Min.X
		right = right && p.X >= r.Max.X
	}
	return above || below || left || right
}

func finite(p geom.Point) bool {
	return !math.IsNaN(p.X) && !math.IsInf(p.X, 0) && !math.IsNaN(p.Y) && !math.IsInf(p.Y, 0)
}
