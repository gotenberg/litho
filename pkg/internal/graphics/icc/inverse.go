package icc

import (
	"math"
	"sort"
)

// inverseBisections is how many halvings [numericInverse] takes: 2^-40 is
// far below the 2^-16 step of the 16-bit values a profile stores.
const inverseBisections = 40

// invertCurve returns the inverse of a tone curve, which an output transform
// through a matrix and tone curves (ICC.1:2010, F.3) applies to each linear
// channel. A gamma and a parametric function invert in closed form, and a
// sampled curve, read as the piecewise linear function its samples define,
// by a search for the segment that holds the value. A curve that neither
// rises nor falls throughout inverts numerically, to some input that gives
// the value.
func invertCurve(c curve) curve {
	switch c := c.(type) {
	case nil:
		return nil
	case gamma:
		if c > 0 {
			return gamma(1 / c)
		}
	case parametric:
		if inv, ok := invertParametric(c); ok {
			return inv
		}
	case sampled:
		if len(c) == 0 {
			return c
		}
		if inv, ok := invertSampled(c); ok {
			return inv
		}
	}
	return numericInverse{c}
}

// parametricInverse is the closed-form inverse of a parametricCurveType
// function (ICC.1:2010, Table 65): below the value at its break point, the
// inverse of the linear segment, above it the inverse of the power segment,
// and the break point itself for a value a discontinuous function jumps over.
type parametricInverse struct {
	p parametric
	// yd is the power segment's value at the break point d, and yl the
	// linear segment's, which a discontinuous function makes differ.
	yd, yl float64
}

// invertParametric builds the inverse of p when its power segment rises: a
// non-zero exponent and slope, and for the linear segment of types 3 and 4 a
// rising slope too.
func invertParametric(p parametric) (curve, bool) {
	if p.g <= 0 || p.kind > 0 && p.a <= 0 {
		return nil, false
	}
	inv := parametricInverse{p: p}
	switch p.kind {
	case 1:
		inv.yd = 0
	case 2:
		inv.yd = p.c
	case 3, 4:
		if p.c <= 0 && p.d > 0 {
			return nil, false
		}
		inv.yd = parametric{kind: p.kind, g: p.g, a: p.a, b: p.b, e: p.e, d: 0}.eval(p.d)
		inv.yl = p.c*p.d + p.f
	}
	return inv, true
}

func (q parametricInverse) eval(y float64) float64 {
	p := q.p
	y = clamp01(y)
	root := func(v float64) float64 {
		if v <= 0 {
			return 0
		}
		return math.Pow(v, 1/p.g)
	}
	var x float64
	switch p.kind {
	case 0:
		x = root(y)
	case 1:
		x = (root(y) - p.b) / p.a
	case 2:
		x = (root(y-p.c) - p.b) / p.a
	default:
		switch {
		case p.d <= 0 || y >= q.yd && y >= q.yl:
			x = (root(y-p.e) - p.b) / p.a
		case y >= q.yl && y < q.yd:
			// The function jumps over y at d, the input that comes nearest.
			x = p.d
		default:
			x = (y - p.f) / p.c
		}
	}
	return clamp01(x)
}

// sampledInverse is the inverse of a monotonic sampled curve: the samples
// hold the curve's values at equally spaced inputs, so the inverse of a value
// is the input where the segment that spans it reaches it.
type sampledInverse struct {
	s       sampled
	falling bool
}

// invertSampled builds the inverse of s when its samples never fall, or never
// rise, and differ at the ends.
func invertSampled(s sampled) (curve, bool) {
	if len(s) < 2 || s[0] == s[len(s)-1] {
		return nil, false
	}
	falling := s[len(s)-1] < s[0]
	for i := 1; i < len(s); i++ {
		if falling && s[i] > s[i-1] || !falling && s[i] < s[i-1] {
			return nil, false
		}
	}
	return sampledInverse{s, falling}, true
}

func (q sampledInverse) eval(y float64) float64 {
	s, n := q.s, len(q.s)
	y = clamp01(y)
	// at finds the first sample at or past y in the curve's direction.
	at := sort.Search(n, func(i int) bool {
		if q.falling {
			return s[i] <= y
		}
		return s[i] >= y
	})
	switch at {
	case 0:
		return 0
	case n:
		return 1
	}
	lo, hi := s[at-1], s[at]
	f := 0.0
	if hi != lo {
		f = (y - lo) / (hi - lo)
	}
	return (float64(at-1) + f) / float64(n-1)
}

// numericInverse inverts a curve by bisection over its input, for a curve no
// closed form inverts. The bisection follows the direction of the curve's
// ends, so it returns an input that gives the value wherever the curve
// crosses it once.
type numericInverse struct {
	c curve
}

func (q numericInverse) eval(y float64) float64 {
	y = clamp01(y)
	lo, hi := 0.0, 1.0
	rising := q.c.eval(1) >= q.c.eval(0)
	for range inverseBisections {
		mid := (lo + hi) / 2
		if (q.c.eval(mid) < y) == rising {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}
