package css

// ShapeOutside holds shape-outside, shape-margin and shape-image-threshold
// (CSS Shapes 1, 6): the float area the lines beside a float wrap around.
// The three are rarely set and share one pointer of [Style], nil for none,
// 0 and 0, which a style that sets none of them never allocates. The shared
// value is never mutated.
type ShapeOutside struct {
	// Shape is the basic shape, nil for a box or an image alone.
	Shape *Shape
	// Box is the reference box of Shape, or the box whose edges, with their
	// corner curvature, make the float area when Shape is nil and Image
	// unset. It defaults to the margin box (CSS Shapes 1, 6.1).
	Box GeometryBox
	// Set reports a value other than none: a basic shape, a box or an
	// image.
	Set bool
	// Image is the image whose alpha channel makes the float area (CSS
	// Shapes 1, 4), sized and placed as a replaced element filling the
	// content box; empty unless the value is an image.
	Image MaskImage
	// Margin is shape-margin: Points, or Percent of the containing block's
	// inline size, or Calc (CSS Shapes 1, 6.3); the zero value is 0.
	Margin Dimension
	// Threshold is shape-image-threshold, within [0, 1] (CSS Shapes 1,
	// 6.2).
	Threshold float64
}

// HasImage reports a shape-outside that is an image.
func (s *ShapeOutside) HasImage() bool { return s.Image.URL != "" || s.Image.Gradient != nil }

// shapeOutsideSpec is a specified shape-outside.
type shapeOutsideSpec struct {
	shape *shapeSpec
	box   GeometryBox
	set   bool
	image imageSpec
}

// shapeBoxes are the <shape-box> keywords: <visual-box> and margin-box
// (CSS Shapes 1, 5). The <geometry-box> keywords of SVG are not among
// them.
var shapeBoxes = map[string]GeometryBox{
	"margin-box": GeoMargin, "border-box": GeoBorder, "padding-box": GeoPadding, "content-box": GeoContent,
}

// parseShapeOutside parses shape-outside: none, a <basic-shape> and a
// <shape-box> in either order, each optional, or an <image> (CSS Shapes
// 1, 6.1).
func parseShapeOutside(v []token, c *declCtx) (any, bool) {
	if len(v) == 1 {
		if v[0].isIdent("none") {
			return shapeOutsideSpec{}, true
		}
		if v[0].typ != tIdent && (v[0].typ != tFunction || !isBasicShapeFunction(v[0])) {
			im, ok := bgImageValue(v[0], c, "shape-outside")
			if !ok {
				return nil, false
			}
			if im.url == "" && im.grad == nil {
				// An image a browser does not draw computes to none.
				return shapeOutsideSpec{}, true
			}
			return shapeOutsideSpec{image: im, set: true, box: GeoContent}, true
		}
	}
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	out := shapeOutsideSpec{box: GeoMargin, set: true}
	boxed := false
	for _, t := range v {
		if t.typ == tIdent {
			b, ok := shapeBoxes[lower(t.value)]
			if !ok || boxed {
				return nil, false
			}
			out.box, boxed = b, true
			continue
		}
		if out.shape != nil {
			return nil, false
		}
		s, ok := parseBasicShape(t)
		if !ok {
			return nil, false
		}
		out.shape = s
	}
	return out, true
}

// isBasicShapeFunction reports a function token of a <basic-shape>.
func isBasicShapeFunction(t token) bool {
	switch lower(t.value) {
	case "inset", "rect", "xywh", "circle", "ellipse", "polygon", "path", "shape":
		return true
	}
	return false
}

// shapeOf returns st's shape-outside fields, the zero value for none.
func shapeOf(st *Style) ShapeOutside {
	if st.ShapeOutside == nil {
		return ShapeOutside{}
	}
	return *st.ShapeOutside
}

// setShape sets st's shape-outside fields to n, keeping the pointer nil
// for none, 0 and 0.
func setShape(st *Style, n ShapeOutside) {
	if !n.Set && n.Margin == (Dimension{}) && n.Threshold == 0 {
		st.ShapeOutside = nil
		return
	}
	// A copy made here alone keeps n off the heap on the path above,
	// which every style takes.
	p := new(ShapeOutside)
	*p = n
	st.ShapeOutside = p
}

// shapeOutsideProps returns shape-outside, shape-margin and
// shape-image-threshold, none of them inherited (CSS Shapes 1, 6).
func shapeOutsideProps() map[propID]propDef {
	return map[propID]propDef{
		pShapeOutside: {name: "shape-outside", parse: parseShapeOutside, compute: func(s *styler, st *Style, v any) {
			spec, ok := v.(shapeOutsideSpec)
			if !ok {
				return
			}
			n := shapeOf(st)
			n.Shape, n.Box, n.Set, n.Image = nil, spec.box, spec.set, MaskImage{URL: spec.image.url}
			if spec.shape != nil {
				n.Shape = spec.shape.compute(s.lc)
			}
			if spec.image.grad != nil {
				n.Image.Gradient = spec.image.grad.compute(s.lc, st.Color)
			}
			if st.ShapeOutside != nil && *st.ShapeOutside == n {
				return
			}
			setShape(st, n)
		}, copyFrom: func(dst, src *Style) {
			n, from := shapeOf(dst), shapeOf(src)
			n.Shape, n.Box, n.Set, n.Image = from.Shape, from.Box, from.Set, from.Image
			setShape(dst, n)
		}},
		pShapeMargin: {name: "shape-margin", parse: func(v []token, _ *declCtx) (any, bool) {
			t, ok := single(v)
			if !ok {
				return nil, false
			}
			return parseLength(t, allowPct|allowMixed|nonNeg)
		}, compute: func(s *styler, st *Style, v any) {
			l, ok := v.(length)
			if !ok {
				return
			}
			n := shapeOf(st)
			n.Margin = l.dim(s.lc, true)
			if n.Margin == Pts(0) {
				// 0 keeps the zero value, so a style that resets
				// shape-margin allocates nothing.
				n.Margin = Dimension{}
			}
			setShape(st, n)
		}, copyFrom: func(dst, src *Style) {
			n := shapeOf(dst)
			n.Margin = shapeOf(src).Margin
			setShape(dst, n)
		}},
		pShapeImageThreshold: {name: "shape-image-threshold", parse: parseOpacity, compute: func(_ *styler, st *Style, v any) {
			f, ok := v.(float64)
			if !ok {
				return
			}
			n := shapeOf(st)
			n.Threshold = f
			setShape(st, n)
		}, copyFrom: func(dst, src *Style) {
			n := shapeOf(dst)
			n.Threshold = shapeOf(src).Threshold
			setShape(dst, n)
		}},
	}
}
