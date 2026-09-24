package icc

import (
	"encoding/binary"
	"fmt"
	"math"
)

// maxCurveEntries bounds a sampled curve. It is an anti-DoS guard, not
// spec-mandated: the count is a 32-bit field, and a 16-bit curve gains
// nothing past 65536 entries.
const maxCurveEntries = 1 << 16

// curve maps a value on 0 to 1 to a value on 0 to 1.
type curve interface {
	eval(x float64) float64
}

// sampled is a curve given by equally spaced samples, interpolated linearly.
type sampled []float64

func (s sampled) eval(x float64) float64 {
	switch len(s) {
	case 0:
		return x
	case 1:
		return s[0]
	}
	x = clamp01(x) * float64(len(s)-1)
	i := min(int(x), len(s)-2)
	f := x - float64(i)
	return s[i] + f*(s[i+1]-s[i])
}

// gamma is the curve x^g.
type gamma float64

func (g gamma) eval(x float64) float64 {
	return math.Pow(clamp01(x), float64(g))
}

// parametric is a parametricCurveType function (ICC.1:2010, Table 65).
type parametric struct {
	kind                int
	g, a, b, c, d, e, f float64
}

func (p parametric) eval(x float64) float64 {
	x = clamp01(x)
	pow := func(v float64) float64 {
		if v <= 0 {
			return 0
		}
		return math.Pow(v, p.g)
	}
	var y float64
	switch p.kind {
	case 0:
		y = pow(x)
	case 1:
		if p.a != 0 && x >= -p.b/p.a {
			y = pow(p.a*x + p.b)
		}
	case 2:
		y = p.c
		if p.a != 0 && x >= -p.b/p.a {
			y = pow(p.a*x+p.b) + p.c
		}
	case 3:
		y = p.c * x
		if x >= p.d {
			y = pow(p.a*x + p.b)
		}
	default:
		y = p.c*x + p.f
		if x >= p.d {
			y = pow(p.a*x+p.b) + p.e
		}
	}
	return clamp01(y)
}

// readCurve parses a curveType or parametricCurveType element (ICC.1:2010,
// 10.5 and 10.18), and returns it with the bytes it spans.
func readCurve(b []byte) (curve, int, error) {
	if len(b) < 12 {
		return nil, 0, fmt.Errorf("%w: curve of %d bytes", ErrMalformed, len(b))
	}
	switch string(b[:4]) {
	case "curv":
		n := binary.BigEndian.Uint32(b[8:])
		if n > maxCurveEntries || 12+2*int(n) > len(b) {
			return nil, 0, fmt.Errorf("%w: curve of %d entries", ErrMalformed, n)
		}
		size := 12 + 2*int(n)
		switch n {
		case 0:
			return sampled(nil), size, nil
		case 1:
			// A u8Fixed8Number exponent.
			return gamma(float64(binary.BigEndian.Uint16(b[12:])) / 256), size, nil
		}
		s := make(sampled, n)
		for i := range s {
			s[i] = float64(binary.BigEndian.Uint16(b[12+2*i:])) / 65535
		}
		return s, size, nil
	case "para":
		kind := int(binary.BigEndian.Uint16(b[8:]))
		counts := [5]int{1, 3, 4, 5, 7}
		if kind > 4 {
			return nil, 0, fmt.Errorf("%w: parametric curve of type %d", ErrUnsupported, kind)
		}
		size := 12 + 4*counts[kind]
		if size > len(b) {
			return nil, 0, fmt.Errorf("%w: parametric curve of %d bytes", ErrMalformed, len(b))
		}
		var v [7]float64
		for i := range counts[kind] {
			v[i] = s15(b[12+4*i:])
		}
		return parametric{kind, v[0], v[1], v[2], v[3], v[4], v[5], v[6]}, size, nil
	}
	return nil, 0, fmt.Errorf("%w: curve of type %q", ErrUnsupported, b[:4])
}

// clamp01 clamps x to 0 to 1.
func clamp01(x float64) float64 {
	return min(max(x, 0), 1)
}
