package css

import "math"

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
	v    [6]float64
}

// transformProp defines transform: none or a list of 2D transform
// functions (CSS Transforms 1, 5 and 13). A 3D function makes the value
// invalid.
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
				out[i] = TransformFunc{Kind: f.kind, V: f.v}
				if f.kind == TransformTranslate {
					out[i].X, out[i].Y = f.x.dim(s.lc, false), f.y.dim(s.lc, false)
				}
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

// transformFunc parses one 2D transform function (CSS Transforms 1, 13).
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
		sx, ok := num(0)
		sy := sx
		if n == 2 && ok {
			sy, ok = num(1)
		}
		return tfSpec{kind: TransformScale, v: [6]float64{sx, sy}}, ok
	case "scalex":
		sx, ok := oneArg(n, func() (float64, bool) { return num(0) })
		return tfSpec{kind: TransformScale, v: [6]float64{sx, 1}}, ok
	case "scaley":
		sy, ok := oneArg(n, func() (float64, bool) { return num(0) })
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
	return tfSpec{}, false
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
