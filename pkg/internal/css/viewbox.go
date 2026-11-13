package css

// objectViewBoxProp is object-view-box (CSS Images 5, 4.1): none or a
// <basic-shape-rect>, inset(), rect() or xywh(), in the natural size of
// the replaced element's picture; it computes to an inset. Not inherited.
func objectViewBoxProp() propDef {
	return propDef{name: "object-view-box", parse: func(v []token, _ *declCtx) (any, bool) {
		t, ok := single(v)
		if !ok {
			return nil, false
		}
		if t.isIdent("none") {
			return (*shapeSpec)(nil), true
		}
		s, ok := parseBasicShape(t)
		if !ok || s.kind != ShapeInset {
			return nil, false
		}
		return s, true
	}, compute: func(s *styler, st *Style, v any) {
		spec, ok := v.(*shapeSpec)
		if !ok {
			return
		}
		st.ObjectViewBox = nil
		if spec != nil {
			st.ObjectViewBox = spec.compute(s.lc)
		}
	}, copyFrom: field(func(s *Style) **Shape { return &s.ObjectViewBox })}
}
