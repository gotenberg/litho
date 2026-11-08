package odp

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// matrix is an affine transform of page points, y pointing down:
// (x, y) maps to (a x + c y + e, b x + d y + f).
type matrix struct{ a, b, c, d, e, f float64 }

// identity is the transform that moves nothing.
var identity = matrix{a: 1, d: 1}

// translation returns the transform moving by (x, y).
func translation(x, y float64) matrix { return matrix{a: 1, d: 1, e: x, f: y} }

// mul returns m after n: n applied first.
func (m matrix) mul(n matrix) matrix {
	return matrix{
		a: m.a*n.a + m.c*n.b, b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d, d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e, f: m.b*n.e + m.d*n.f + m.f,
	}
}

// apply maps the point (x, y).
func (m matrix) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

// place returns the placement of a w by h box at the origin mapped by m:
// the box scaled by m's axis lengths about the mapped centre, the angle
// m turns the x axis clockwise, and a vertical flip where m mirrors.
func (m matrix) place(w, h float64) placement {
	cx, cy := m.apply(w/2, h/2)
	sx := math.Hypot(m.a, m.b)
	det := m.a*m.d - m.b*m.c
	sy := 0.0
	if sx > 0 {
		sy = det / sx
	}
	rot := math.Atan2(m.b, m.a) * 180 / math.Pi
	if rot < 0 {
		rot += 360
	}
	if math.Abs(rot) < 1e-9 || math.Abs(rot-360) < 1e-9 {
		rot = 0
	}
	bw, bh := w*sx, h*math.Abs(sy)
	return placement{box: compose.Rect{X: cx - bw/2, Y: cy - bh/2, Width: bw, Height: bh}, rotation: rot, flipV: sy < 0}
}

// maxTransformOps bounds the operations of one draw:transform read: 16.
// Anti-DoS guard, not spec-mandated: Impress writes a rotation, a skew
// and a translation at most.
const maxTransformOps = 16

// parseTransform parses a draw:transform (ODF 1.3, Part 3, 19.228): a
// list of rotate, scale, translate, skewX, skewY and matrix operations,
// applied in the order they are listed, as Impress applies them. A
// rotate angle is in radians, counterclockwise on the page; translate
// and the matrix's translation are ODF lengths. It returns the composed
// transform, whether it skews, and whether it parsed.
func parseTransform(s string) (matrix, bool, bool) {
	m := identity
	skew := false
	rest := strings.TrimSpace(s)
	for n := 0; rest != ""; n++ {
		if n >= maxTransformOps {
			return m, skew, false
		}
		open := strings.IndexByte(rest, '(')
		end := strings.IndexByte(rest, ')')
		if open <= 0 || end < open {
			return m, skew, false
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
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e9 {
				return 0, false
			}
			return v, true
		}
		var op matrix
		switch name {
		case "rotate":
			a, ok := num(0, false)
			if !ok {
				return m, skew, false
			}
			sin, cos := math.Sincos(a)
			op = matrix{a: cos, b: -sin, c: sin, d: cos}
		case "translate":
			x, ok := num(0, true)
			if !ok {
				return m, skew, false
			}
			y, _ := num(1, true)
			op = translation(x, y)
		case "scale":
			x, ok := num(0, false)
			if !ok {
				return m, skew, false
			}
			y, okY := num(1, false)
			if !okY {
				y = x
			}
			op = matrix{a: x, d: y}
		case "skewX", "skewY":
			a, ok := num(0, false)
			if !ok {
				return m, skew, false
			}
			skew = skew || math.Abs(a) > 1e-9
			// A positive angle leans a shape's top to the right of its
			// bottom on the page, y pointing down (measured on the pinned
			// LibreOffice: skewX (0.5) on a 4 cm square moves its bottom
			// edge tan 0.5 times its height, 2.2 cm, left of its top edge), as the counterclockwise
			// rotate turns it.
			if name == "skewX" {
				op = matrix{a: 1, c: -math.Tan(a), d: 1}
			} else {
				op = matrix{a: 1, b: -math.Tan(a), d: 1}
			}
		case "matrix":
			var v [6]float64
			for i := range v {
				x, ok := num(i, i >= 4)
				if !ok {
					return m, skew, false
				}
				v[i] = x
			}
			op = matrix{a: v[0], b: v[1], c: v[2], d: v[3], e: v[4], f: v[5]}
			skew = skew || math.Abs(v[0]*v[2]+v[1]*v[3]) > 1e-9
		default:
			return m, skew, false
		}
		m = op.mul(m)
	}
	return m, skew, true
}
