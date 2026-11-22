package css

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// This file reads the 3D transform functions and the properties of 3D
// rendering (CSS Transforms 2, 7, 8, 9, 11 and 12). A page is flat: the
// layout multiplies the 4x4 matrices of a box's functions, the
// perspective its parent gives it, and projects the result onto the
// page, where a transform that keeps the box's plane parallel to the
// page is drawn exactly.

// Space3D holds the 3D rendering longhands of an element: perspective,
// perspective-origin, transform-style and backface-visibility. None is
// inherited.
type Space3D struct {
	// Perspective is the distance of the perspective in points that
	// perspective gives the element's children, 0 for none (CSS
	// Transforms 2, 7).
	Perspective float64
	// OriginSet reports a perspective-origin other than the initial 50%
	// 50%: Origin then holds its horizontal and vertical offsets in the
	// border box (CSS Transforms 2, 8).
	OriginSet bool
	Origin    [2]Dimension
	// Preserve3D is transform-style: preserve-3d (CSS Transforms 2, 9).
	Preserve3D bool
	// BackfaceHidden is backface-visibility: hidden (CSS Transforms 2,
	// 11).
	BackfaceHidden bool
}

// initial reports a Space3D every longhand of which is initial.
func (s *Space3D) initial() bool { return *s == Space3D{} }

// Space returns the element's 3D rendering longhands, nil when every one
// is initial. The result is shared: never modify it.
func (s *Style) Space() *Space3D { return s.Space3D }

// HasPerspective reports a perspective other than none, which makes the
// element a stacking context and the containing block of its absolute
// and fixed descendants (CSS Transforms 2, 7).
func (s *Style) HasPerspective() bool { return s.Space3D != nil && s.Space3D.Perspective > 0 }

// minPerspective is the least perspective distance drawn, 1px in points:
// shorter ones are drawn at it (CSS Transforms 2, 7 and 12).
const minPerspective = Pt

// space3DProps returns the longhands of 3D rendering.
func space3DProps() map[propID]propDef {
	return map[propID]propDef{
		pPerspective: space3DLonghand("perspective", parsePerspective, func(s *styler, m *Space3D, v any) {
			if l, ok := v.(length); ok {
				d := l.dim(s.lc, true)
				m.Perspective = 0
				if d.Kind == Points {
					m.Perspective = max(d.Value, minPerspective)
				}
				return
			}
			m.Perspective = 0
		}, func(dst, src *Space3D) { dst.Perspective = src.Perspective }),
		pPerspectiveOrigin: space3DLonghand("perspective-origin", func(v []token, _ *declCtx) (any, bool) {
			l := significant(v)
			if len(l) < 1 || len(l) > 2 {
				return nil, false
			}
			p, ok := parsePosition(l)
			return p, ok
		}, func(s *styler, m *Space3D, v any) {
			if p, ok := v.([2]length); ok {
				m.Origin = computePosition(s, p)
				m.OriginSet = m.Origin != [2]Dimension{{Kind: Percent, Value: 0.5}, {Kind: Percent, Value: 0.5}}
				if !m.OriginSet {
					m.Origin = [2]Dimension{}
				}
			}
		}, func(dst, src *Space3D) { dst.OriginSet, dst.Origin = src.OriginSet, src.Origin }),
		pTransformStyle: space3DLonghand("transform-style", keywordParser("flat", "preserve-3d"), func(_ *styler, m *Space3D, v any) {
			k, _ := v.(string)
			m.Preserve3D = k == "preserve-3d"
		}, func(dst, src *Space3D) { dst.Preserve3D = src.Preserve3D }),
		pBackfaceVisibility: space3DLonghand("backface-visibility", keywordParser("visible", "hidden"), func(_ *styler, m *Space3D, v any) {
			k, _ := v.(string)
			m.BackfaceHidden = k == "hidden"
		}, func(dst, src *Space3D) { dst.BackfaceHidden = src.BackfaceHidden }),
	}
}

// keywordParser parses one of the keywords kws, returned as written in
// lower case.
func keywordParser(kws ...string) parseFn {
	return func(v []token, _ *declCtx) (any, bool) {
		k, ok := keyword(v)
		if !ok {
			return nil, false
		}
		for _, w := range kws {
			if k == w {
				return k, true
			}
		}
		return nil, false
	}
}

// parsePerspective parses perspective: none | <length [0,∞]> (CSS
// Transforms 2, 7); none is the zero length value.
func parsePerspective(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok {
		return nil, k == "none"
	}
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	l, ok := parseLength(t, nonNeg)
	return l, ok
}

// space3DLonghand defines a non-inherited longhand held in
// [Style.Space3D]: set stores a computed value into a copy, cp copies the
// field.
func space3DLonghand(name string, parse parseFn, set func(*styler, *Space3D, any), cp func(dst, src *Space3D)) propDef {
	return propDef{
		name:  name,
		parse: parse,
		compute: func(s *styler, st *Style, v any) {
			setSpace3D(st, func(m *Space3D) { set(s, m, v) })
		},
		copyFrom: func(dst, src *Style) {
			if dst.Space3D == src.Space3D {
				return
			}
			from := src.Space3D
			if from == nil {
				from = &Space3D{}
			}
			setSpace3D(dst, func(m *Space3D) { cp(m, from) })
		},
	}
}

// setSpace3D applies f to a copy of st's 3D rendering longhands, storing
// nil when every one ends initial. The shared value is never mutated.
func setSpace3D(st *Style, f func(*Space3D)) {
	n := Space3D{}
	if st.Space3D != nil {
		n = *st.Space3D
	}
	f(&n)
	if n.initial() {
		st.Space3D = nil
		return
	}
	st.Space3D = &n
}

// transformFunc3D parses the 3D transform function named name whose
// arguments are args, each one token (CSS Transforms 2, 12): the lengths
// of translate3d() and translateZ() stay specified, the others compute to
// a matrix here. It reports false for a name that is no 3D function or
// arguments that do not fit it.
func transformFunc3D(name string, args [][]token) (tfSpec, bool) {
	n := len(args)
	num := func(i int) (float64, bool) { return parseNumber(args[i][0]) }
	factor := func(i int) (float64, bool) { return scaleFactor(args[i][0]) }
	ang := func(i int) (float64, bool) {
		if args[i][0].typ == tNumber && args[i][0].num != 0 {
			return 0, false
		}
		d, ok := parseAngle(args[i][0])
		return d * math.Pi / 180, ok
	}
	switch name {
	case "translate3d":
		if n != 3 {
			return tfSpec{}, false
		}
		x, ok1 := parseLength(args[0][0], allowPct)
		y, ok2 := parseLength(args[1][0], allowPct)
		z, ok3 := parseLength(args[2][0], 0)
		return tfSpec{kind: TransformTranslate, x: x, y: y, z: z, hasZ: true}, ok1 && ok2 && ok3
	case "translatez":
		if n != 1 {
			return tfSpec{}, false
		}
		z, ok := parseLength(args[0][0], 0)
		return tfSpec{kind: TransformTranslate, x: length{hasLen: true}, y: length{hasLen: true}, z: z, hasZ: true}, ok
	case "scale3d":
		if n != 3 {
			return tfSpec{}, false
		}
		var v [3]float64
		for i := range v {
			f, ok := factor(i)
			if !ok {
				return tfSpec{}, false
			}
			v[i] = f
		}
		return scale3DSpec(v[0], v[1], v[2]), true
	case "scalez":
		if n != 1 {
			return tfSpec{}, false
		}
		f, ok := factor(0)
		return scale3DSpec(1, 1, f), ok
	case "rotate3d":
		if n != 4 {
			return tfSpec{}, false
		}
		var v [3]float64
		for i := range v {
			f, ok := num(i)
			if !ok {
				return tfSpec{}, false
			}
			v[i] = f
		}
		a, ok := ang(3)
		if !ok {
			return tfSpec{}, false
		}
		return rotate3DSpec(v[0], v[1], v[2], a), true
	case "rotatex", "rotatey", "rotatez":
		if n != 1 {
			return tfSpec{}, false
		}
		a, ok := ang(0)
		if !ok {
			return tfSpec{}, false
		}
		switch name {
		case "rotatex":
			return rotate3DSpec(1, 0, 0, a), true
		case "rotatey":
			return rotate3DSpec(0, 1, 0, a), true
		}
		return tfSpec{kind: TransformRotate, v: [6]float64{a}}, true
	case "perspective":
		if n != 1 {
			return tfSpec{}, false
		}
		if k, ok := keyword(args[0]); ok {
			// perspective(none) is the identity.
			return tfSpec{kind: TransformScale, v: [6]float64{1, 1}}, k == "none"
		}
		l, ok := parseLength(args[0][0], nonNeg)
		return tfSpec{kind: TransformPerspective, z: l, hasZ: true}, ok
	case "matrix3d":
		if n != 16 {
			return tfSpec{}, false
		}
		// matrix3d() lists the matrix column by column; M holds it row by
		// row, for column vectors. Its translation is in CSS pixels and
		// its perspective row in reciprocal CSS pixels.
		var m [16]float64
		for i := range 16 {
			f, ok := num(i)
			if !ok {
				return tfSpec{}, false
			}
			m[(i%4)*4+i/4] = f
		}
		for r := range 3 {
			m[r*4+3] = clampLen(m[r*4+3] * Pt)
		}
		for c := range 3 {
			m[12+c] /= Pt
		}
		return tfSpec{kind: TransformMatrix3D, m: &m}, true
	}
	return tfSpec{}, false
}

// scale3DSpec returns scale3d(sx, sy, sz), a 2D scale when sz is 1.
func scale3DSpec(sx, sy, sz float64) tfSpec {
	if sz == 1 {
		return tfSpec{kind: TransformScale, v: [6]float64{sx, sy}}
	}
	return tfSpec{kind: TransformMatrix3D, m: &[16]float64{
		sx, 0, 0, 0,
		0, sy, 0, 0,
		0, 0, sz, 0,
		0, 0, 0, 1,
	}}
}

// rotate3DSpec returns rotate3d(x, y, z, a), a in radians (CSS Transforms
// 2, 12): a rotation about the z axis alone is a 2D rotation, a zero axis
// the identity.
func rotate3DSpec(x, y, z, a float64) tfSpec {
	l := math.Sqrt(float64(x*x) + float64(y*y) + float64(z*z))
	if !numeric.Finite(l) || l == 0 {
		return tfSpec{kind: TransformScale, v: [6]float64{1, 1}}
	}
	x, y, z = x/l, y/l, z/l
	if x == 0 && y == 0 {
		return tfSpec{kind: TransformRotate, v: [6]float64{a * z}}
	}
	s, c := detmath.Sin(a/2), detmath.Cos(a/2)
	sc, sq := float64(s*c), float64(s*s)
	return tfSpec{kind: TransformMatrix3D, m: &[16]float64{
		1 - float64(2*float64(float64(y*y)+float64(z*z))*sq), 2 * (float64(float64(x*y)*sq) - float64(z*sc)), 2 * (float64(float64(x*z)*sq) + float64(y*sc)), 0,
		2 * (float64(float64(x*y)*sq) + float64(z*sc)), 1 - float64(2*float64(float64(x*x)+float64(z*z))*sq), 2 * (float64(float64(y*z)*sq) - float64(x*sc)), 0,
		2 * (float64(float64(x*z)*sq) - float64(y*sc)), 2 * (float64(float64(y*z)*sq) + float64(x*sc)), 1 - float64(2*float64(float64(x*x)+float64(y*y))*sq), 0,
		0, 0, 0, 1,
	}}
}
