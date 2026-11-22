package css

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// maxTransformFuncs bounds the functions of one transform list: 32 is far
// more than any real stylesheet chains. Anti-DoS guard, not spec-mandated.
const maxTransformFuncs = 32

// zIndexProp defines z-index: auto or an integer (CSS 2.1, 9.9.1).
func zIndexProp() propDef {
	return propDef{
		name: "z-index",
		parse: func(v []token, _ *declCtx) (any, bool) {
			if k, ok := keyword(v); ok {
				return ZIndex{Auto: true}, k == "auto"
			}
			t, ok := single(v)
			if !ok {
				return nil, false
			}
			n, ok := parseInteger(t)
			return ZIndex{Level: n}, ok
		},
		compute: func(_ *styler, st *Style, v any) {
			if z, ok := v.(ZIndex); ok {
				st.ZIndex = z
			}
		},
		copyFrom: field(func(s *Style) *ZIndex { return &s.ZIndex }),
	}
}

// tfSpec is a specified transform function: its lengths not yet computed.
type tfSpec struct {
	kind TransformKind
	x, y length
	// z is the z offset of a translation, or the distance of a
	// perspective(), when hasZ is set.
	z    length
	hasZ bool
	v    [6]float64
	m    *[16]float64
}

// computed returns the computed transform function of f.
func (f tfSpec) computed(s *styler) TransformFunc {
	out := TransformFunc{Kind: f.kind, V: f.v, M: f.m}
	switch f.kind { //nolint:exhaustive // the other functions hold numbers alone
	case TransformTranslate:
		out.X, out.Y = f.x.dim(s.lc, false), f.y.dim(s.lc, false)
		if f.hasZ {
			if z := f.z.dim(s.lc, false); z.Kind == Points {
				out.V[0] = z.Value
			}
		}
	case TransformPerspective:
		out.V[0] = minPerspective
		if d := f.z.dim(s.lc, true); d.Kind == Points {
			out.V[0] = max(d.Value, minPerspective)
		}
	}
	return out
}

// transformProp defines transform: none or a list of transform
// functions (CSS Transforms 1, 5 and 13; CSS Transforms 2, 12).
func transformProp() propDef {
	return propDef{
		name:  "transform",
		parse: parseTransform,
		compute: func(s *styler, st *Style, v any) {
			specs, ok := v.([]tfSpec)
			if !ok {
				return
			}
			if len(specs) == 0 {
				st.Transform = nil
				return
			}
			out := make([]TransformFunc, len(specs))
			for i, f := range specs {
				out[i] = f.computed(s)
			}
			st.Transform = out
		},
		copyFrom: field(func(s *Style) *[]TransformFunc { return &s.Transform }),
	}
}

// parseTransform parses a <transform-list> or none.
func parseTransform(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok {
		return []tfSpec{}, k == "none"
	}
	l := significant(v)
	if len(l) == 0 || len(l) > maxTransformFuncs {
		return nil, false
	}
	out := make([]tfSpec, 0, len(l))
	for _, t := range l {
		f, ok := transformFunc(t)
		if !ok {
			return nil, false
		}
		out = append(out, f)
	}
	return out, true
}

// transformFunc parses one transform function (CSS Transforms 1, 13; CSS
// Transforms 2, 12).
func transformFunc(t token) (tfSpec, bool) {
	if t.typ != tFunction {
		return tfSpec{}, false
	}
	var args [][]token
	for _, a := range splitCommas(t.children) {
		a = significant(a)
		if len(a) != 1 {
			return tfSpec{}, false
		}
		args = append(args, a)
	}
	n := len(args)
	num := func(i int) (float64, bool) { return parseNumber(args[i][0]) }
	// A scale takes a number or a percentage (CSS Transforms 2, 12).
	factor := func(i int) (float64, bool) { return scaleFactor(args[i][0]) }
	ang := func(i int) (float64, bool) {
		d, ok := parseAngle(args[i][0])
		// A bare number is an angle only when it is zero (CSS Values 4,
		// 7.1).
		if args[i][0].typ == tNumber && args[i][0].num != 0 {
			return 0, false
		}
		return d * math.Pi / 180, ok
	}
	lenAt := func(i int) (length, bool) { return parseLength(args[i][0], allowPct) }
	switch lower(t.value) {
	case "matrix":
		if n != 6 {
			return tfSpec{}, false
		}
		f := tfSpec{kind: TransformMatrix}
		for i := range 6 {
			x, ok := num(i)
			if !ok {
				return tfSpec{}, false
			}
			f.v[i] = x
		}
		// The translation is in CSS pixels (CSS Transforms 1, 13.1).
		f.v[4], f.v[5] = clampLen(f.v[4]*Pt), clampLen(f.v[5]*Pt)
		return f, true
	case "translate":
		if n < 1 || n > 2 {
			return tfSpec{}, false
		}
		x, ok := lenAt(0)
		y := length{hasLen: true}
		if n == 2 && ok {
			y, ok = lenAt(1)
		}
		return tfSpec{kind: TransformTranslate, x: x, y: y}, ok
	case "translatex":
		x, ok := oneArg(n, func() (length, bool) { return lenAt(0) })
		return tfSpec{kind: TransformTranslate, x: x, y: length{hasLen: true}}, ok
	case "translatey":
		y, ok := oneArg(n, func() (length, bool) { return lenAt(0) })
		return tfSpec{kind: TransformTranslate, x: length{hasLen: true}, y: y}, ok
	case "scale":
		if n < 1 || n > 2 {
			return tfSpec{}, false
		}
		sx, ok := factor(0)
		sy := sx
		if n == 2 && ok {
			sy, ok = factor(1)
		}
		return tfSpec{kind: TransformScale, v: [6]float64{sx, sy}}, ok
	case "scalex":
		sx, ok := oneArg(n, func() (float64, bool) { return factor(0) })
		return tfSpec{kind: TransformScale, v: [6]float64{sx, 1}}, ok
	case "scaley":
		sy, ok := oneArg(n, func() (float64, bool) { return factor(0) })
		return tfSpec{kind: TransformScale, v: [6]float64{1, sy}}, ok
	case "rotate":
		a, ok := oneArg(n, func() (float64, bool) { return ang(0) })
		return tfSpec{kind: TransformRotate, v: [6]float64{a}}, ok
	case "skew":
		if n < 1 || n > 2 {
			return tfSpec{}, false
		}
		ax, ok := ang(0)
		ay := 0.0
		if n == 2 && ok {
			ay, ok = ang(1)
		}
		return tfSpec{kind: TransformSkew, v: [6]float64{ax, ay}}, ok
	case "skewx":
		ax, ok := oneArg(n, func() (float64, bool) { return ang(0) })
		return tfSpec{kind: TransformSkew, v: [6]float64{ax, 0}}, ok
	case "skewy":
		ay, ok := oneArg(n, func() (float64, bool) { return ang(0) })
		return tfSpec{kind: TransformSkew, v: [6]float64{0, ay}}, ok
	}
	return transformFunc3D(lower(t.value), args)
}

// oneArg returns parse's value when a function has exactly one argument.
func oneArg[T any](n int, parse func() (T, bool)) (T, bool) {
	var zero T
	if n != 1 {
		return zero, false
	}
	return parse()
}

// transformOriginProp defines transform-origin: a position of one or two
// values, then an optional z offset that a 2D transform ignores (CSS
// Transforms 1, 6).
func transformOriginProp() propDef {
	return propDef{
		name: "transform-origin",
		parse: func(v []token, _ *declCtx) (any, bool) {
			l := significant(v)
			if len(l) == 3 {
				if _, ok := parseLength(l[2], 0); !ok {
					return nil, false
				}
				l = l[:2]
			}
			if len(l) < 1 || len(l) > 2 {
				return nil, false
			}
			p, ok := parsePosition(l)
			return p, ok
		},
		compute: func(s *styler, st *Style, v any) {
			if p, ok := v.([2]length); ok {
				st.TransformOrigin = [2]Dimension{p[0].dim(s.lc, false), p[1].dim(s.lc, false)}
			}
		},
		copyFrom: field(func(s *Style) *[2]Dimension { return &s.TransformOrigin }),
	}
}

// scaleFactor parses a scale factor: a <number> or a <percentage>, 100%
// being 1 (CSS Transforms 2, 5.3 and 12).
func scaleFactor(t token) (float64, bool) {
	if t.typ == tPercentage {
		return t.num / 100, numeric.Finite(t.num)
	}
	return parseNumber(t)
}

// individualProp defines one of the individual transform properties:
// none or the one function parse reads, stored at f (CSS Transforms 2, 5).
func individualProp(name string, parse func([]token) (tfSpec, bool), f func(*Style) *[]TransformFunc) propDef {
	return propDef{
		name: name,
		parse: func(v []token, _ *declCtx) (any, bool) {
			if k, ok := keyword(v); ok {
				return []tfSpec{}, k == "none"
			}
			l := significant(v)
			if len(l) == 0 || len(l) > 4 {
				return nil, false
			}
			sp, ok := parse(l)
			return []tfSpec{sp}, ok
		},
		compute: func(s *styler, st *Style, v any) {
			specs, ok := v.([]tfSpec)
			if !ok {
				return
			}
			if len(specs) == 0 {
				*f(st) = nil
				return
			}
			*f(st) = []TransformFunc{specs[0].computed(s)}
		},
		copyFrom: field(f),
	}
}

// translateProp defines translate: none | <length-percentage>
// [<length-percentage> <length>?]? (CSS Transforms 2, 5.1).
func translateProp() propDef {
	return individualProp("translate", func(l []token) (tfSpec, bool) {
		if len(l) > 3 {
			return tfSpec{}, false
		}
		x, ok := parseLength(l[0], allowPct)
		y := length{hasLen: true}
		if ok && len(l) > 1 {
			y, ok = parseLength(l[1], allowPct)
		}
		sp := tfSpec{kind: TransformTranslate, x: x, y: y}
		if ok && len(l) > 2 {
			sp.z, ok = parseLength(l[2], 0)
			sp.hasZ = true
		}
		return sp, ok
	}, func(s *Style) *[]TransformFunc { return &s.Translate })
}

// scaleProp defines scale: none | [<number> | <percentage>]{1,3} (CSS
// Transforms 2, 5.3): one factor scales both axes; a third scales z, as
// scale3d() does.
func scaleProp() propDef {
	return individualProp("scale", func(l []token) (tfSpec, bool) {
		if len(l) > 3 {
			return tfSpec{}, false
		}
		var v [3]float64
		for i, t := range l {
			f, ok := scaleFactor(t)
			if !ok {
				return tfSpec{}, false
			}
			v[i] = f
		}
		if len(l) == 1 {
			v[1] = v[0]
		}
		if len(l) == 3 {
			return scale3DSpec(v[0], v[1], v[2]), true
		}
		return tfSpec{kind: TransformScale, v: [6]float64{v[0], v[1]}}, true
	}, func(s *Style) *[]TransformFunc { return &s.Scale })
}

// rotateProp defines rotate: none | <angle> | [x | y | z | <number>{3}]
// && <angle> (CSS Transforms 2, 5.2), rotate3d() about its axis.
func rotateProp() propDef {
	return individualProp("rotate", parseRotate, func(s *Style) *[]TransformFunc { return &s.Rotate })
}

// parseRotate parses the value of rotate other than none.
func parseRotate(l []token) (tfSpec, bool) {
	angle := func(t token) (float64, bool) {
		if t.typ == tNumber && t.num != 0 {
			return 0, false
		}
		d, ok := parseAngle(t)
		return d * math.Pi / 180, ok
	}
	var axis []token
	a, ok := angle(l[0])
	if ok {
		axis = l[1:]
	} else {
		a, ok = angle(l[len(l)-1])
		axis = l[:len(l)-1]
	}
	if !ok {
		return tfSpec{}, false
	}
	x, y, z := 0.0, 0.0, 1.0
	switch len(axis) {
	case 0:
	case 1:
		k, isKw := keyword(axis)
		switch {
		case !isKw:
			return tfSpec{}, false
		case k == "x":
			x, y, z = 1, 0, 0
		case k == "y":
			x, y, z = 0, 1, 0
		case k == "z":
		default:
			return tfSpec{}, false
		}
	case 3:
		var v [3]float64
		for i, t := range axis {
			if t.typ != tNumber || !numeric.Finite(t.num) {
				return tfSpec{}, false
			}
			v[i] = t.num
		}
		x, y, z = v[0], v[1], v[2]
	default:
		return tfSpec{}, false
	}
	return rotate3DSpec(x, y, z, a), true
}

// HasTransform reports whether st transforms its box: a transform list, an
// individual transform property or an offset-path other than none (CSS
// Transforms 2, 6; CSS Motion Path 1, 2.1).
func (st *Style) HasTransform() bool {
	return len(st.Transform) > 0 || len(st.Translate) > 0 || len(st.Rotate) > 0 || len(st.Scale) > 0 || st.HasOffsetPath()
}

// TransformLists returns the transform functions of st in the order they
// apply: translate, rotate, scale, then the transform list (CSS
// Transforms 2, 6).
func (st *Style) TransformLists() [4][]TransformFunc {
	return [4][]TransformFunc{st.Translate, st.Rotate, st.Scale, st.Transform}
}
