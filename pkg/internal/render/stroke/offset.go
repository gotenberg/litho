package stroke

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// maxOutlinePoints caps the points of one outline. Anti-DoS guard, not
// spec-mandated: the outline grows with the flattened path, the dashes and
// the arcs, each capped on its own, and this bounds their product. Four
// million points (64 MB) exceed by far what a page's densest stroke needs.
const maxOutlinePoints = 1 << 22

// maxArcSegments caps the chords of a full turn of a round join, cap or dot.
// Anti-DoS guard, not spec-mandated: a line width huge against the
// tolerance would otherwise ask for an unbounded count. 4096 chords keep a
// circle of radius 100000 pixels within a third of a pixel.
const maxArcSegments = 4096

// minArcSegments is the fewest chords of a full turn, so a dot smaller than
// the tolerance still paints a square rather than a sliver.
const minArcSegments = 4

// builder accumulates the outline, mapping each point through m.
type builder struct {
	m    geom.Matrix
	out  geom.Path
	over bool
}

func (b *builder) moveTo(p geom.Point) {
	if b.full() {
		return
	}
	b.out.MoveTo(b.m.Apply(p))
}

func (b *builder) lineTo(p geom.Point) {
	if b.full() {
		return
	}
	b.out.LineTo(b.m.Apply(p))
}

func (b *builder) close() {
	if !b.over {
		b.out.Close()
	}
}

func (b *builder) full() bool {
	if len(b.out.Pts) >= maxOutlinePoints {
		b.over = true
	}
	return b.over
}

// stroker strokes polylines with a pen of radius half, in the space the
// pieces are in.
type stroker struct {
	builder
	half  float64
	cap   Cap
	join  Join
	limit float64
	tol   float64
}

func (s *stroker) piece(pc piece) {
	switch {
	case len(pc.pts) == 0:
	case len(pc.pts) == 1:
		s.dot(pc.pts[0], pc.tangent)
	case pc.closed:
		s.closed(pc.pts, pc.smooth)
		s.closed(reversed(pc.pts, true), reversedBools(pc.smooth, true))
	default:
		s.open(pc)
	}
}

// dot paints a single point: a circle of the line width with round caps, a
// square of side the line width with square caps, aligned on the tangent,
// and nothing with butt caps. ISO 32000-1, 8.5.3.2 paints a degenerate
// subpath with round caps only. The square is a deliberate extension of that
// clause: it reads the point as a zero-length segment whose projecting caps
// extend half the width each way, the reading a zero-length dash needs
// anyway (8.4.3.6 places a cap at each end of every dash).
//
// TODO: confirm the square against the pinned poppler once an image that
// ships pdftoppm is provisioned.
func (s *stroker) dot(p, tangent geom.Point) {
	d := unit(tangent)
	n := left(d)
	switch s.cap {
	case RoundCap:
		s.moveTo(add(p, scale(n, s.half)))
		s.arc(p, n, -2*math.Pi)
		s.close()
	case SquareCap:
		s.moveTo(add(p, scale(sub(n, d), s.half)))
		s.lineTo(add(p, scale(add(n, d), s.half)))
		s.lineTo(add(p, scale(sub(d, n), s.half)))
		s.lineTo(add(p, scale(sub(scale(n, -1), d), s.half)))
		s.close()
	case ButtCap:
	}
}

// open strokes an open polyline as one contour: the left side forward, the
// end cap, the right side backward and the start cap.
func (s *stroker) open(pc piece) {
	dirs := directions(pc.pts, false)
	s.moveTo(add(pc.pts[0], scale(left(dirs[0]), s.half)))
	s.side(pc.pts, dirs, pc.smooth)
	s.capAt(pc.pts[len(pc.pts)-1], dirs[len(dirs)-1])
	rp := reversed(pc.pts, false)
	rd := directions(rp, false)
	s.side(rp, rd, reversedBools(pc.smooth, false))
	s.capAt(rp[len(rp)-1], rd[len(rd)-1])
	s.close()
}

// closed strokes the left side of a closed polyline as a closed contour,
// joining at every vertex, the start included. Called once forward and once
// on the reversed polyline, it covers both sides.
func (s *stroker) closed(pts []geom.Point, smooth []bool) {
	dirs := directions(pts, true)
	m := len(dirs)
	s.moveTo(add(pts[0], scale(left(dirs[0]), s.half)))
	for i := range m {
		if s.over {
			return
		}
		next := (i + 1) % len(pts)
		s.lineTo(add(pts[next], scale(left(dirs[i]), s.half)))
		s.joinAt(pts[next], dirs[i], dirs[(i+1)%m], smooth[next])
	}
	s.close()
}

// side walks the left offset of the polyline, from the end of its first
// segment to the end of its last, joining at interior vertices.
func (s *stroker) side(pts []geom.Point, dirs []geom.Point, smooth []bool) {
	for i, d := range dirs {
		if s.over {
			return
		}
		s.lineTo(add(pts[i+1], scale(left(d), s.half)))
		if i+1 < len(dirs) {
			s.joinAt(pts[i+1], d, dirs[i+1], smooth[i+1])
		}
	}
}

// joinAt joins, on the left side, the segment arriving at p along din to the
// one leaving along dout, ending at p + half·left(dout). Where the left side
// is the inner side of the turn the contour pivots through p; the loop that
// leaves adds winding inside the stroke only, so the nonzero fill is
// unchanged. The outer side follows the join style (ISO 32000-1, 8.4.3.4).
// A vertex inside a flattened curve joins round, since the offset of a
// smooth curve is the envelope of the pen.
func (s *stroker) joinAt(p, din, dout geom.Point, smooth bool) {
	cross := din.X*dout.Y - din.Y*dout.X
	dot := din.X*dout.X + din.Y*dout.Y
	nin, nout := left(din), left(dout)
	end := add(p, scale(nout, s.half))
	if cross > 0 {
		s.lineTo(p)
		s.lineTo(end)
		return
	}
	if cross == 0 && dot > 0 {
		s.lineTo(end)
		return
	}
	style := s.join
	if smooth {
		style = RoundJoin
	}
	switch style {
	case RoundJoin:
		// Abs, not negation: at a reversal cross is 0 or −0, and
		// atan2(−0, −1) is −π, which would sweep the arc inside the turn.
		s.arc(p, nin, -math.Atan2(math.Abs(cross), dot))
	case BevelJoin:
	case MiterJoin:
		// The miter length over the line width is 1/sin(φ/2), φ the angle
		// between the segments (8.4.3.5); with cos θ = din·dout for the turn
		// θ = π − φ, that is sqrt(2/(1+cos θ)). Past the limit the corner
		// bevels. A reversal has an infinite miter and always bevels.
		if dot > -1 && 2/(1+dot) <= s.limit*s.limit && s.limit >= 1 {
			k := s.half / (1 + dot)
			s.lineTo(add(p, scale(add(nin, nout), k)))
		}
	}
	s.lineTo(end)
}

// capAt caps the end p of a segment running along d, from p + half·left(d)
// to p − half·left(d) (ISO 32000-1, 8.4.3.3).
func (s *stroker) capAt(p, d geom.Point) {
	n := left(d)
	switch s.cap {
	case RoundCap:
		s.arc(p, n, -math.Pi)
	case SquareCap:
		s.lineTo(add(p, scale(add(n, d), s.half)))
		s.lineTo(add(p, scale(sub(d, n), s.half)))
	case ButtCap:
	}
	s.lineTo(add(p, scale(n, -s.half)))
}

// arc emits the chords of the arc of radius half around c that starts in the
// unit direction from and turns by sweep radians, the last point included
// and the first one not. The chords are inscribed, each within tol of the
// arc: a chord spanning angle δ deviates from it by r·(1 − cos(δ/2)).
func (s *stroker) arc(c, from geom.Point, sweep float64) {
	n := arcSteps(s.half, s.tol, math.Abs(sweep))
	for i := 1; i <= n && !s.over; i++ {
		a := sweep * float64(i) / float64(n)
		sin, cos := math.Sincos(a)
		v := geom.Point{X: from.X*cos - from.Y*sin, Y: from.X*sin + from.Y*cos}
		s.lineTo(add(c, scale(v, s.half)))
	}
}

func arcSteps(r, tol, sweep float64) int {
	step := 2 * math.Pi / minArcSegments
	if tol < r {
		step = min(step, 2*math.Acos(1-tol/r))
	}
	f := math.Ceil(sweep / step)
	if limit := math.Ceil(sweep / (2 * math.Pi) * maxArcSegments); !(f <= limit) {
		f = limit
	}
	return max(int(f), 1)
}

// directions returns the unit direction of each segment of pts, closing back
// to the first point when closed. pts holds no two consecutive equal points.
func directions(pts []geom.Point, closed bool) []geom.Point {
	n := len(pts) - 1
	if closed {
		n++
	}
	dirs := make([]geom.Point, n)
	for i := range dirs {
		dirs[i] = unit(sub(pts[(i+1)%len(pts)], pts[i]))
	}
	return dirs
}

// reversed returns pts in reverse order. A closed polyline keeps its first
// point first, so its reversed loop starts where the forward one does.
func reversed(pts []geom.Point, closed bool) []geom.Point {
	out := make([]geom.Point, len(pts))
	for i := range pts {
		out[i] = pts[len(pts)-1-i]
	}
	if closed {
		copy(out[1:], out[:len(out)-1])
		out[0] = pts[0]
	}
	return out
}

func reversedBools(v []bool, closed bool) []bool {
	out := make([]bool, len(v))
	for i := range v {
		out[i] = v[len(v)-1-i]
	}
	if closed {
		copy(out[1:], out[:len(out)-1])
		out[0] = v[0]
	}
	return out
}

// unit returns v scaled to length 1, and the x axis for the zero vector.
func unit(v geom.Point) geom.Point {
	l := math.Hypot(v.X, v.Y)
	if l == 0 || !finite(l) {
		return geom.Point{X: 1}
	}
	return geom.Point{X: v.X / l, Y: v.Y / l}
}

// left returns v turned a quarter turn counterclockwise.
func left(v geom.Point) geom.Point { return geom.Point{X: -v.Y, Y: v.X} }

func add(a, b geom.Point) geom.Point { return geom.Point{X: a.X + b.X, Y: a.Y + b.Y} }

func sub(a, b geom.Point) geom.Point { return geom.Point{X: a.X - b.X, Y: a.Y - b.Y} }

func scale(v geom.Point, k float64) geom.Point { return geom.Point{X: v.X * k, Y: v.Y * k} }
