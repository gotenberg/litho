package stroke

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// Cap is a line cap style, numbered as the operand of the J operator (ISO
// 32000-1, 8.4.3.3, Table 54).
type Cap int

// The line cap styles.
const (
	// ButtCap squares the stroke off at the endpoint.
	ButtCap Cap = 0
	// RoundCap ends the stroke with a semicircle of the line width.
	RoundCap Cap = 1
	// SquareCap extends the stroke by half the line width past the endpoint.
	SquareCap Cap = 2
)

// Join is a line join style, numbered as the operand of the j operator (ISO
// 32000-1, 8.4.3.4, Table 55).
type Join int

// The line join styles.
const (
	// MiterJoin extends the outer edges until they meet, unless the miter
	// limit turns the corner into a bevel.
	MiterJoin Join = 0
	// RoundJoin rounds the outer corner with an arc of the line width.
	RoundJoin Join = 1
	// BevelJoin cuts the outer corner with a straight edge.
	BevelJoin Join = 2
)

// DefaultMiterLimit is the initial miter limit of the graphics state (ISO
// 32000-1, 8.4.1, Table 52).
const DefaultMiterLimit = 10.0

// DefaultTolerance is the device-space flattening tolerance, in pixels,
// [Outline] uses when the caller passes none. A quarter pixel keeps the
// error of a curve or a round join below what anti-aliasing shows.
const DefaultTolerance = 0.25

// hairlineWidth is the device-space width of a zero-width line: ISO 32000-1,
// 8.4.3.2 makes a line width of 0 the thinnest line the device renders, one
// device pixel.
const hairlineWidth = 1.0

// Style holds the stroke parameters of the graphics state (ISO 32000-1,
// 8.4.3). Width and the dash lengths are in user space.
type Style struct {
	// Width is the line width (8.4.3.2). Zero is the thinnest line the device
	// can render; a negative or non-finite width is rejected.
	Width float64
	// Cap is the line cap style (8.4.3.3). An unknown value strokes as
	// [ButtCap].
	Cap Cap
	// Join is the line join style (8.4.3.4). An unknown value strokes as
	// [MiterJoin].
	Join Join
	// MiterLimit bounds the ratio of the miter length to the line width
	// (8.4.3.5). A value below 1 bevels every corner; NaN strokes with
	// [DefaultMiterLimit].
	MiterLimit float64
	// Dash is the dash array and DashPhase the dash phase (8.4.3.6). An empty
	// array strokes a solid line.
	Dash      []float64
	DashPhase float64
}

// Outline returns the device-space outline of path stroked with s, path being
// in user space and ctm mapping user space to device space. Fill the outline
// with the nonzero winding rule. tolerance is the largest distance, in device
// pixels, between a flattened curve or arc and the exact one; a value that is
// not positive and finite selects [DefaultTolerance].
//
// A subpath of a single point, closed or made of coincident points, paints a
// filled circle with round caps and nothing otherwise (8.5.3.2); a lone
// MoveTo paints nothing. A zero-length dash paints its caps: a circle with
// round caps, a square along the path with square caps (8.4.3.6). Open
// subpaths get caps at both ends, closed ones a join at their start. Each
// subpath restarts the dash pattern (8.4.3.6).
//
// Outline returns [ErrInvalidStyle] for a negative or non-finite width,
// [ErrNonFinite] for a non-finite point or matrix entry, and [ErrTooComplex]
// when a cap is exceeded.
func Outline(path geom.Path, s Style, ctm geom.Matrix, tolerance float64) (geom.Path, error) {
	if !finite(s.Width) || s.Width < 0 {
		return geom.Path{}, fmt.Errorf("line width %v: %w", s.Width, ErrInvalidStyle)
	}
	for _, v := range ctm {
		if !finite(v) {
			return geom.Path{}, fmt.Errorf("transform %v: %w", ctm, ErrNonFinite)
		}
	}
	for _, p := range path.Pts {
		if !finite(p.X) || !finite(p.Y) {
			return geom.Path{}, fmt.Errorf("path point %v: %w", p, ErrNonFinite)
		}
	}
	if !finite(tolerance) || tolerance <= 0 {
		tolerance = DefaultTolerance
	}
	stretch := maxStretch(ctm)
	if !finite(stretch) {
		return geom.Path{}, fmt.Errorf("transform %v overflows: %w", ctm, ErrNonFinite)
	}
	if stretch == 0 {
		// A CTM whose linear part is zero maps every stroke onto one point.
		return geom.Path{}, nil
	}
	userTol := tolerance / stretch

	pieces, err := flatten(path, userTol)
	if err != nil {
		return geom.Path{}, fmt.Errorf("flattening the stroked path: %w", err)
	}
	if pattern, ok := newDashPattern(s.Dash, s.DashPhase); ok {
		pieces, err = pattern.apply(pieces)
		if err != nil {
			return geom.Path{}, fmt.Errorf("dashing the stroked path: %w", err)
		}
	}

	st := stroker{
		half:  s.Width / 2,
		cap:   s.Cap,
		join:  s.Join,
		limit: s.MiterLimit,
		tol:   userTol,
		m:     ctm,
	}
	if math.IsNaN(st.limit) {
		st.limit = DefaultMiterLimit
	}
	// Tables 54 and 55 define styles 0 to 2 only; any other value strokes as
	// the initial style of the graphics state (Table 52).
	if st.cap < ButtCap || st.cap > SquareCap {
		st.cap = ButtCap
	}
	if st.join < MiterJoin || st.join > BevelJoin {
		st.join = MiterJoin
	}
	if s.Width == 0 {
		// The thinnest line is one device pixel wide whatever the CTM, so the
		// pen is applied in device space to the mapped centre line. The dash
		// pattern has already been laid out in user space, where 8.4.3.6
		// measures it.
		for i := range pieces {
			pieces[i] = pieces[i].transform(ctm)
		}
		st.half = hairlineWidth / 2
		st.tol = tolerance
		st.m = geom.Identity
	}
	for _, pc := range pieces {
		st.piece(pc)
		if st.over {
			return geom.Path{}, fmt.Errorf("outline exceeds %d points: %w", maxOutlinePoints, ErrTooComplex)
		}
	}
	for _, p := range st.out.Pts {
		if !finite(p.X) || !finite(p.Y) {
			return geom.Path{}, fmt.Errorf("outline point %v: %w", p, ErrNonFinite)
		}
	}
	return st.out, nil
}

// maxStretch returns the largest factor by which the linear part of m
// lengthens a vector: its largest singular value. A user-space error e
// becomes at most e times this in device space.
func maxStretch(m geom.Matrix) float64 {
	s := m[0]*m[0] + m[1]*m[1] + m[2]*m[2] + m[3]*m[3]
	det := m.Det()
	disc := s*s - 4*det*det
	if disc < 0 {
		disc = 0
	}
	return math.Sqrt((s + math.Sqrt(disc)) / 2)
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
