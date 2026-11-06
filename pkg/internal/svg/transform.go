package svg

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// maxTransforms caps the functions of one transform list. Anti-DoS guard,
// not spec-mandated: real transform lists hold one to three.
const maxTransforms = 256

// parseTransform parses a transform list (SVG 1.1, 7.6): matrix,
// translate, scale, rotate, skewX and skewY, separated by white space or
// commas, applied left to right as nested transforms. It reports false
// for a list in error, which the caller ignores, as browsers do.
func parseTransform(s string) (compose.Matrix, bool) {
	sc := scanner{s: s}
	m := identity
	n := 0
	sc.skipWSP()
	for !sc.done() {
		n++
		if n > maxTransforms {
			return identity, false
		}
		j := sc.i
		for j < len(sc.s) && (sc.s[j] >= 'a' && sc.s[j] <= 'z' || sc.s[j] >= 'A' && sc.s[j] <= 'Z') {
			j++
		}
		name := sc.s[sc.i:j]
		sc.i = j
		sc.skipWSP()
		if sc.peek() != '(' {
			return identity, false
		}
		sc.i++
		var args []float64
		for {
			sc.skipWSP()
			if sc.peek() == ')' {
				sc.i++
				break
			}
			v, ok := sc.number()
			if !ok || len(args) >= 6 {
				return identity, false
			}
			args = append(args, v)
			sc.skipSep()
		}
		t, ok := transformFunc(name, args)
		if !ok {
			return identity, false
		}
		m = mul(m, t)
		sc.skipSep()
	}
	return m, true
}

// transformFunc returns the matrix of one transform function (SVG 1.1,
// 7.6), false for a wrong name or argument count.
func transformFunc(name string, a []float64) (compose.Matrix, bool) {
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }
	switch name {
	case "matrix":
		if len(a) == 6 {
			return compose.Matrix{A: a[0], B: a[1], C: a[2], D: a[3], E: a[4], F: a[5]}, true
		}
	case "translate":
		switch len(a) {
		case 1:
			return translate(a[0], 0), true
		case 2:
			return translate(a[0], a[1]), true
		}
	case "scale":
		switch len(a) {
		case 1:
			return scale(a[0], a[0]), true
		case 2:
			return scale(a[0], a[1]), true
		}
	case "rotate":
		if len(a) != 1 && len(a) != 3 {
			return identity, false
		}
		c, s := math.Cos(rad(a[0])), math.Sin(rad(a[0]))
		r := compose.Matrix{A: c, B: s, C: -s, D: c}
		if len(a) == 3 {
			r = mul(translate(a[1], a[2]), mul(r, translate(-a[1], -a[2])))
		}
		return r, true
	case "skewX":
		if len(a) == 1 {
			return compose.Matrix{A: 1, C: math.Tan(rad(a[0])), D: 1}, true
		}
	case "skewY":
		if len(a) == 1 {
			return compose.Matrix{A: 1, B: math.Tan(rad(a[0])), D: 1}, true
		}
	}
	return identity, false
}
