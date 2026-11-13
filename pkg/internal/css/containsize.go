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
// one pointer of [Style], nil for none and none.
func containIntrinsicProps() map[propID]propDef {
	side := func(name string, set func(*IntrinsicSize, float64), get func(IntrinsicSize) float64) propDef {
		return propDef{
			name:  name,
			parse: parseContainIntrinsic,
			compute: func(s *styler, st *Style, v any) {
				l, ok := v.(intrinsicLength)
				if !ok {
					return
				}
				n := IntrinsicSize{Width: -1, Height: -1}
				if st.ContainIntrinsic != nil {
					n = *st.ContainIntrinsic
				}
				w := -1.0
				if l.set {
					w = max(l.l.points(s.lc), 0)
				}
				set(&n, w)
				st.ContainIntrinsic = nil
				if n.Width >= 0 || n.Height >= 0 {
					st.ContainIntrinsic = &n
				}
			},
			copyFrom: func(dst, src *Style) {
				v := -1.0
				if src.ContainIntrinsic != nil {
					v = get(*src.ContainIntrinsic)
				}
				n := IntrinsicSize{Width: -1, Height: -1}
				if dst.ContainIntrinsic != nil {
					n = *dst.ContainIntrinsic
				}
				set(&n, v)
				dst.ContainIntrinsic = nil
				if n.Width >= 0 || n.Height >= 0 {
					dst.ContainIntrinsic = &n
				}
			},
		}
	}
	return map[propID]propDef{
		pContainIntrinsicWidth: side("contain-intrinsic-width",
			func(n *IntrinsicSize, v float64) { n.Width = v }, func(n IntrinsicSize) float64 { return n.Width }),
		pContainIntrinsicHeight: side("contain-intrinsic-height",
			func(n *IntrinsicSize, v float64) { n.Height = v }, func(n IntrinsicSize) float64 { return n.Height }),
	}
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
