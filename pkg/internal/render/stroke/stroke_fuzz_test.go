package stroke

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// fuzzPath decodes data into a path: each step is one op byte followed by
// the coordinates it needs, each a signed 16-bit value over 16, so the
// points are finite and the fuzzer reaches every geometry quickly.
func fuzzPath(data []byte) geom.Path {
	var p geom.Path
	coord := func() (float64, bool) {
		if len(data) < 2 {
			return 0, false
		}
		v := float64(int16(binary.LittleEndian.Uint16(data))) / 16
		data = data[2:]
		return v, true
	}
	point := func() (geom.Point, bool) {
		x, ok1 := coord()
		y, ok2 := coord()
		return geom.Point{X: x, Y: y}, ok1 && ok2
	}
	for len(data) > 0 && len(p.Ops) < 256 {
		op := data[0] % 4
		data = data[1:]
		switch geom.Op(op) {
		case geom.MoveTo, geom.LineTo:
			pt, ok := point()
			if !ok {
				return p
			}
			if geom.Op(op) == geom.MoveTo {
				p.MoveTo(pt)
			} else {
				p.LineTo(pt)
			}
		case geom.CubicTo:
			a, ok1 := point()
			b, ok2 := point()
			c, ok3 := point()
			if !ok1 || !ok2 || !ok3 {
				return p
			}
			p.CubicTo(a, b, c)
		case geom.Close:
			p.Close()
		}
	}
	return p
}

// FuzzOutline asserts that Outline never panics, and that a successful
// outline holds finite points, only straight steps, and stays within the
// pen's reach of the path's bounds.
func FuzzOutline(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 0, 1, 0, 1, 0, 1}, 2.0, uint8(1), uint8(0), 10.0, 0.0, 0.0, 1.0, 0.0)
	f.Add([]byte{0, 0, 0, 0, 0, 2, 16, 0, 16, 0, 32, 0, 0, 0, 0, 3}, 4.0, uint8(2), uint8(1), 1.0, 2.0, 1.0, 0.5, 3.0)
	f.Add([]byte{0, 0, 0}, 0.0, uint8(0), uint8(2), 10.0, 0.0, 5.0, 2.0, 1.0)
	f.Fuzz(func(t *testing.T, data []byte, width float64, cp, jn uint8, limit, dashOn, dashOff, sx, shear float64) {
		path := fuzzPath(data)
		s := Style{
			Width:      width,
			Cap:        Cap(cp % 4),
			Join:       Join(jn % 4),
			MiterLimit: limit,
		}
		if dashOn != 0 || dashOff != 0 {
			s.Dash = []float64{dashOn, dashOff}
		}
		ctm := geom.Matrix{sx, shear, 0, 1, 0, 0}
		out, err := Outline(path, s, ctm, 0.5)
		if err != nil {
			if !errors.Is(err, ErrInvalidStyle) && !errors.Is(err, ErrNonFinite) && !errors.Is(err, ErrTooComplex) {
				t.Fatalf("unexpected error %v", err)
			}
			return
		}
		for _, op := range out.Ops {
			if op == geom.CubicTo {
				t.Fatal("outline holds a cubic")
			}
		}
		for _, p := range out.Pts {
			if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
				t.Fatalf("non-finite outline point %v", p)
			}
		}
		b, ok := path.Transform(ctm).Bounds()
		if !ok || len(out.Pts) == 0 {
			return
		}
		reach := s.Width * maxStretch(ctm) * math.Sqrt2
		if s.Width == 0 {
			reach = hairlineWidth
		}
		// A miter tip reaches limit·width/2 from its corner; an unknown join
		// strokes as a miter.
		if s.Join != RoundJoin && s.Join != BevelJoin {
			limit := s.MiterLimit
			if math.IsNaN(limit) {
				limit = DefaultMiterLimit
			}
			reach *= max(1, limit)
		}
		reach += 1
		for _, p := range out.Pts {
			if p.X < b.Min.X-reach || p.X > b.Max.X+reach || p.Y < b.Min.Y-reach || p.Y > b.Max.Y+reach {
				t.Fatalf("outline point %v outside %v by more than %v", p, b, reach)
			}
		}
	})
}
