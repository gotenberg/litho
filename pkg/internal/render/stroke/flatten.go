package stroke

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// maxCurveSegments caps the line segments one cubic flattens into. Anti-DoS
// guard, not spec-mandated: a curve with control points far apart would
// otherwise ask for an unbounded count. 4096 segments hold a curve spanning
// a 15000-pixel page within a tenth of a pixel, past any real page size.
const maxCurveSegments = 4096

// maxFlatPoints caps the points one path flattens into, across its subpaths.
// Anti-DoS guard, not spec-mandated: a content stream of tens of megabytes of
// curves would otherwise flatten into billions of points. Two million points
// (32 MB) exceed by far the densest page art a renderer meets.
const maxFlatPoints = 1 << 21

// piece is a polyline to stroke: a flattened subpath or one dash of it.
type piece struct {
	pts []geom.Point
	// smooth[i] reports that pts[i] lies inside a flattened curve, where the
	// stroke joins round whatever the join style.
	smooth []bool
	closed bool
	// tangent orients the cap of a piece that is a single point; the zero
	// vector stands for the x axis.
	tangent geom.Point
}

func (pc *piece) add(p geom.Point, smooth bool) {
	pc.pts = append(pc.pts, p)
	pc.smooth = append(pc.smooth, smooth)
}

// dedupe drops each point equal to its predecessor, and for a closed piece
// the last point when it equals the first. A merged point is smooth only when
// both points were, since a corner that falls on a curve point stays a
// corner.
func (pc piece) dedupe() piece {
	out := piece{closed: pc.closed, tangent: pc.tangent}
	for i, p := range pc.pts {
		if n := len(out.pts); n > 0 && out.pts[n-1] == p {
			out.smooth[n-1] = out.smooth[n-1] && pc.smooth[i]
			continue
		}
		out.add(p, pc.smooth[i])
	}
	if out.closed && len(out.pts) > 1 && out.pts[len(out.pts)-1] == out.pts[0] {
		out.pts = out.pts[:len(out.pts)-1]
		out.smooth = out.smooth[:len(out.smooth)-1]
	}
	return out
}

// transform maps the piece through m and dedupes it again, since a singular
// or shrinking m can merge points.
func (pc piece) transform(m geom.Matrix) piece {
	t := pc.tangent
	if t == (geom.Point{}) {
		t = geom.Point{X: 1}
	}
	out := piece{
		pts:     make([]geom.Point, len(pc.pts)),
		smooth:  append([]bool(nil), pc.smooth...),
		closed:  pc.closed,
		tangent: m.ApplyVector(t),
	}
	for i, p := range pc.pts {
		out.pts[i] = m.Apply(p)
	}
	return out.dedupe()
}

// flatten splits p into subpaths and flattens their curves to line segments
// within tol. Lone MoveTo steps produce no piece (ISO 32000-1, 8.5.3.2
// paints a degenerate subpath only when it is closed or holds two or more
// coincident points).
func flatten(p geom.Path, tol float64) ([]piece, error) {
	var (
		pieces  []piece
		cur     piece
		drawn   bool // cur holds a segment or a Close
		started bool // cur has a start point
		start   geom.Point
		k       int
		total   int
	)
	flush := func() {
		if started && drawn {
			pieces = append(pieces, cur.dedupe())
		}
		cur, drawn, started = piece{}, false, false
	}
	// beginAt starts a subpath at the current point when a segment follows a
	// Close: per ISO 32000-1, 8.5.2.1, h leaves the current point at the
	// start of the closed subpath, and a following l or c opens a new one
	// there.
	beginAt := func(pt geom.Point) {
		cur = piece{}
		cur.add(pt, false)
		started = true
	}
	hasCurrent := false
	for _, op := range p.Ops {
		need := 0
		switch op {
		case geom.MoveTo, geom.LineTo:
			need = 1
		case geom.CubicTo:
			need = 3
		case geom.Close:
		}
		if k+need > len(p.Pts) {
			return nil, fmt.Errorf("path step %d wants %d points, %d left: %w", op, need, len(p.Pts)-k, ErrMalformedPath)
		}
		switch op {
		case geom.MoveTo:
			flush()
			start = p.Pts[k]
			beginAt(start)
			hasCurrent = true
		case geom.LineTo:
			if !hasCurrent {
				// A segment with no current point is an error in the content
				// stream (8.5.2.1: a path begins with m or re); poppler
				// ignores the operator, and so does this.
				break
			}
			if !started {
				beginAt(start)
			}
			cur.add(p.Pts[k], false)
			drawn = true
			total++
		case geom.CubicTo:
			if !hasCurrent {
				break
			}
			if !started {
				beginAt(start)
			}
			n := cubicSteps(cur.pts[len(cur.pts)-1], p.Pts[k], p.Pts[k+1], p.Pts[k+2], tol)
			total += n
			if total > maxFlatPoints {
				return nil, fmt.Errorf("flattened path exceeds %d points: %w", maxFlatPoints, ErrTooComplex)
			}
			addCubic(&cur, p.Pts[k], p.Pts[k+1], p.Pts[k+2], n)
			drawn = true
		case geom.Close:
			if started {
				cur.closed = true
				drawn = true
				flush()
			}
		}
		if total > maxFlatPoints {
			return nil, fmt.Errorf("flattened path exceeds %d points: %w", maxFlatPoints, ErrTooComplex)
		}
		k += need
	}
	flush()
	return pieces, nil
}

// cubicSteps returns the number of equal parameter steps that keep the chords
// of the cubic within tol of it. By Wang's bound, a degree-3 Bézier split into
// n uniform steps deviates from its chords by at most 3/4·M/n², M being the
// larger norm of the second differences of the control points.
func cubicSteps(p0, p1, p2, p3 geom.Point, tol float64) int {
	m := max(
		math.Hypot(p0.X-2*p1.X+p2.X, p0.Y-2*p1.Y+p2.Y),
		math.Hypot(p1.X-2*p2.X+p3.X, p1.Y-2*p2.Y+p3.Y),
	)
	f := math.Ceil(math.Sqrt(0.75 * m / tol))
	if !(f < maxCurveSegments) { // NaN or past the cap
		return maxCurveSegments
	}
	return max(int(f), 1)
}

// addCubic appends n points of the cubic from the last point of pc, the
// interior ones marked smooth.
func addCubic(pc *piece, c1, c2, end geom.Point, n int) {
	p0 := pc.pts[len(pc.pts)-1]
	for i := 1; i < n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		pc.add(geom.Point{
			X: a*p0.X + b*c1.X + c*c2.X + d*end.X,
			Y: a*p0.Y + b*c1.Y + c*c2.Y + d*end.Y,
		}, true)
	}
	pc.add(end, false)
}
