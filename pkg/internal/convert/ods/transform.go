package ods

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// mul returns m after n: n applied first.
func mul(m, n typeset.Matrix) typeset.Matrix {
	return typeset.Matrix{
		A: float64(m.A*n.A) + float64(m.C*n.B), B: float64(m.B*n.A) + float64(m.D*n.B),
		C: float64(m.A*n.C) + float64(m.C*n.D), D: float64(m.B*n.C) + float64(m.D*n.D),
		E: float64(m.A*n.E) + float64(m.C*n.F) + m.E, F: float64(m.B*n.E) + float64(m.D*n.F) + m.F,
	}
}

// apply maps the point (x, y) by m.
func apply(m typeset.Matrix, x, y float64) (float64, float64) {
	return float64(m.A*x) + float64(m.C*y) + m.E, float64(m.B*x) + float64(m.D*y) + m.F
}

// translation returns the transform moving by (x, y).
func translation(x, y float64) typeset.Matrix { return typeset.Matrix{A: 1, D: 1, E: x, F: y} }

// maxTransformOps bounds the operations of one draw:transform read: 16.
// Anti-DoS guard, not spec-mandated: Calc writes a skew, a rotation and a
// translation at most.
const maxTransformOps = 16

// maxTransformNumber bounds the magnitude of a draw:transform number read:
// 1e9. Anti-DoS guard, not spec-mandated: it keeps the composition of
// [maxTransformOps] operations finite, and a sheet's coordinates stay
// under a million points.
const maxTransformNumber = 1e9

// parseTransform parses a draw:transform (ODF 1.4, Part 3, 19.228): a list
// of rotate, scale, translate, skewX, skewY and matrix operations, applied
// in the order they are listed, as Calc applies them. A rotate angle is
// in radians, counterclockwise on the page, as ODF 1.4 states and Calc
// writes and reads it; ODF 1.3 said degrees. translate and the matrix's
// translation are ODF lengths. It returns the composed transform and
// whether it parsed.
//
// # Corpus evidence
//
//   - probe, odp: a positive skew angle leans a shape's top right of its
//     bottom on the page, y pointing down, as the counterclockwise rotate
//     turns it, in the pinned LibreOffice's Impress, whose drawing layer
//     Calc shares.
func parseTransform(s string) (typeset.Matrix, bool) {
	m := typeset.Identity
	rest := strings.TrimSpace(s)
	for n := 0; rest != ""; n++ {
		if n >= maxTransformOps {
			return m, false
		}
		open := strings.IndexByte(rest, '(')
		end := strings.IndexByte(rest, ')')
		if open <= 0 || end < open {
			return m, false
		}
		name := strings.TrimSpace(rest[:open])
		args := strings.FieldsFunc(rest[open+1:end], func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest[end+1:]), ","))
		num := func(i int, length bool) (float64, bool) {
			if i >= len(args) {
				return 0, false
			}
			if length {
				if v, ok := odf.Length(args[i]); ok {
					return v, true
				}
			}
			v, err := strconv.ParseFloat(args[i], 64)
			if err != nil || !numeric.Finite(v) || math.Abs(v) > maxTransformNumber {
				return 0, false
			}
			return v, true
		}
		var op typeset.Matrix
		switch name {
		case "rotate":
			a, ok := num(0, false)
			if !ok {
				return m, false
			}
			sin, cos := detmath.Sincos(a)
			op = typeset.Matrix{A: cos, B: -sin, C: sin, D: cos}
		case "translate":
			x, ok := num(0, true)
			if !ok {
				return m, false
			}
			y, _ := num(1, true)
			op = translation(x, y)
		case "scale":
			x, ok := num(0, false)
			if !ok {
				return m, false
			}
			y, okY := num(1, false)
			if !okY {
				y = x
			}
			op = typeset.Matrix{A: x, D: y}
		case "skewX", "skewY":
			a, ok := num(0, false)
			if !ok {
				return m, false
			}
			// A positive angle leans a shape's top right of its bottom on
			// the page, y pointing down, as the counterclockwise rotate
			// turns it.
			if name == "skewX" {
				op = typeset.Matrix{A: 1, C: -detmath.Tan(a), D: 1}
			} else {
				op = typeset.Matrix{A: 1, B: -detmath.Tan(a), D: 1}
			}
		case "matrix":
			var v [6]float64
			for i := range v {
				x, ok := num(i, i >= 4)
				if !ok {
					return m, false
				}
				v[i] = x
			}
			op = typeset.Matrix{A: v[0], B: v[1], C: v[2], D: v[3], E: v[4], F: v[5]}
		default:
			return m, false
		}
		m = mul(op, m)
	}
	return m, true
}
