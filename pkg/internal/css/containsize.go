package css

// IntrinsicSize holds contain-intrinsic-width and contain-intrinsic-height
// (CSS Sizing 4, 5.1): the size a box under size containment takes in
// place of its content's, in points, or a negative value for none.
//
// auto <length> computes to the length and auto none to none: auto names
// the size a browser last laid the box out at, which a printed document
// does not have (CSS Sizing 4, 5.2), and Chromium prints such a box at the
// length.
type IntrinsicSize struct {
	Width, Height float64
}

// containIntrinsicProps returns contain-intrinsic-width and
// contain-intrinsic-height, not inherited. Both rarely set, they share
// one pointer of [Style], nil for none and none, which a style that sets
// neither never allocates.
func containIntrinsicProps() map[propID]propDef {
	side := func(name string, axis int) propDef {
		return propDef{
			name:  name,
			parse: parseContainIntrinsic,
			compute: func(s *styler, st *Style, v any) {
				if l, ok := v.(intrinsicLength); ok {
					w := -1.0
					if l.set {
						w = max(l.l.points(s.lc), 0)
					}
					setIntrinsic(st, axis, w)
				}
			},
			copyFrom: func(dst, src *Style) { setIntrinsic(dst, axis, intrinsicOf(src, axis)) },
		}
	}
	return map[propID]propDef{
		pContainIntrinsicWidth:  side("contain-intrinsic-width", 0),
		pContainIntrinsicHeight: side("contain-intrinsic-height", 1),
	}
}

// intrinsicOf returns the contain-intrinsic-width (axis 0) or -height
// (axis 1) of st, negative for none.
func intrinsicOf(st *Style, axis int) float64 {
	switch {
	case st.ContainIntrinsic == nil:
		return -1
	case axis == 0:
		return st.ContainIntrinsic.Width
	}
	return st.ContainIntrinsic.Height
}

// setIntrinsic sets the contain-intrinsic-width (axis 0) or -height (axis
// 1) of st to v, negative for none, keeping the other: it allocates only
// for a value that differs and is not none on both axes. The shared value
// is never mutated.
func setIntrinsic(st *Style, axis int, v float64) {
	if intrinsicOf(st, axis) == v || v < 0 && intrinsicOf(st, axis) < 0 {
		return
	}
	n := IntrinsicSize{Width: intrinsicOf(st, 0), Height: intrinsicOf(st, 1)}
	if axis == 0 {
		n.Width = v
	} else {
		n.Height = v
	}
	if n.Width < 0 && n.Height < 0 {
		st.ContainIntrinsic = nil
		return
	}
	st.ContainIntrinsic = &IntrinsicSize{Width: n.Width, Height: n.Height}
}

// intrinsicLength is a specified contain-intrinsic-width or -height: a
// length, or none when set is false.
type intrinsicLength struct {
	l   length
	set bool
}

// parseContainIntrinsic parses contain-intrinsic-width or -height (CSS
// Sizing 4, 5.1): auto? [ none | <length [0,∞]> ].
func parseContainIntrinsic(v []token, _ *declCtx) (any, bool) {
	if len(v) == 2 && v[0].typ == tIdent && eqFold(v[0].value, "auto") {
		v = v[1:]
	}
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.typ == tIdent {
		return intrinsicLength{}, eqFold(t.value, "none")
	}
	l, ok := parseLength(t, nonNeg)
	return intrinsicLength{l: l, set: true}, ok
}

// containIntrinsicShorthand is contain-intrinsic-size (CSS Sizing 4,
// 5.1): one value for both sizes, or the width then the height, each
// auto? [ none | <length> ].
func containIntrinsicShorthand() shorthandDef {
	return shorthandDef{
		longhands: []propID{pContainIntrinsicWidth, pContainIntrinsicHeight},
		expand: func(v []token, c *declCtx) ([]entry, bool) {
			var parts [][]token
			for i := 0; i < len(v); i++ {
				n := 1
				if v[i].typ == tIdent && eqFold(v[i].value, "auto") && i+1 < len(v) {
					n = 2
				}
				parts = append(parts, v[i:i+n])
				i += n - 1
			}
			if len(parts) < 1 || len(parts) > 2 {
				return nil, false
			}
			w, ok := parseContainIntrinsic(parts[0], c)
			if !ok {
				return nil, false
			}
			h := w
			if len(parts) == 2 {
				if h, ok = parseContainIntrinsic(parts[1], c); !ok {
					return nil, false
				}
			}
			return []entry{{pContainIntrinsicWidth, w}, {pContainIntrinsicHeight, h}}, true
		},
	}
}
