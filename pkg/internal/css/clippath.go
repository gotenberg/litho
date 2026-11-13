package css

// ClipPath is the clip-path of an element (CSS Masking 1, 5.1): a
// reference to an SVG clipPath element, or a basic shape, a reference box
// or both. The zero value is none.
type ClipPath struct {
	// URL is the resolved reference to a clipPath element, "" for none.
	URL string
	// Shape is the basic shape, nil for none. A reference box without a
	// shape clips to the box itself.
	Shape *Shape
	// Box is the reference box of Shape, or the box that clips when Shape
	// is nil and Boxed is set.
	Box GeometryBox
	// Boxed reports a value that names a basic shape or a box.
	Boxed bool
}

// None reports a clip-path of none.
func (c ClipPath) None() bool { return c.URL == "" && !c.Boxed }

// GeometryBox is the reference box of a basic shape (CSS Masking 1, 5.1;
// CSS Shapes 1, 4.1). fill-box computes to content-box and stroke-box and
// view-box to border-box, as they do for an element with a CSS layout box.
type GeometryBox uint8

// The reference boxes.
const (
	GeoBorder GeometryBox = iota
	GeoPadding
	GeoContent
	GeoMargin
)

// geometryBoxes are the <geometry-box> keywords.
var geometryBoxes = map[string]GeometryBox{
	"border-box": GeoBorder, "padding-box": GeoPadding, "content-box": GeoContent, "margin-box": GeoMargin,
	"fill-box": GeoContent, "stroke-box": GeoBorder, "view-box": GeoBorder,
}

// ShapeKind is the function of a basic shape (CSS Shapes 1, 3.1).
type ShapeKind uint8

// The basic shapes. rect() and xywh() compute to an inset (CSS Shapes 1,
// 3.1, computed values of basic shapes).
const (
	ShapeInset ShapeKind = iota
	ShapeCircle
	ShapeEllipse
	ShapePolygon
	ShapePath
	// ShapeShape is a shape(), held as the SVG path commands it draws
	// (CSS Shapes 1, 3.2.1).
	ShapeShape
)

// ShapeRadius is the radius of a circle or an ellipse along one axis.
type ShapeRadius struct {
	// Side is RadiusClosest, RadiusFarthest or, for an explicit length,
	// RadiusLength.
	Side RadiusSide
	// Length is the explicit radius, Points or Percent or Calc.
	Length Dimension
}

// RadiusSide names how a [ShapeRadius] is sized.
type RadiusSide uint8

// The radius sizes. A circle's or an ellipse's radius is closest-side
// unless the value says otherwise (CSS Shapes 1, 3.1).
const (
	RadiusClosest RadiusSide = iota
	RadiusFarthest
	RadiusLength
	// RadiusClosestCorner and RadiusFarthestCorner reach the nearest and
	// the farthest corner of the reference box (CSS Shapes 1, 3.1,
	// <radial-extent>).
	RadiusClosestCorner
	RadiusFarthestCorner
)

// Shape is a computed <basic-shape> (CSS Shapes 1, 3.1).
type Shape struct {
	Kind ShapeKind
	// Inset holds the top, right, bottom and left offsets of an inset
	// from the reference box's edges, and Radii its rounded corners,
	// indexed by [Corner].
	Inset [4]Dimension
	Radii [4]Radius
	// Radius holds the radius of a circle in its first entry, and the
	// horizontal and vertical radii of an ellipse; Center the centre's
	// horizontal and vertical offsets from the reference box's top left.
	Radius [2]ShapeRadius
	Center [2]Dimension
	// EvenOdd is the fill rule of a polygon or a path: even-odd when set,
	// nonzero otherwise.
	EvenOdd bool
	// Points are the vertices of a polygon, each its horizontal then
	// vertical offset from the reference box's top left.
	Points [][2]Dimension
	// Round is the radius that rounds each vertex of a polygon, in
	// points, 0 for sharp vertices (CSS Shapes 1, 3.1, polygon()).
	Round float64
	// Path is the SVG path data of a path, in pixels from the reference
	// box's top left.
	Path string
	// Commands are the commands of a shape(), the first a move to its
	// from position.
	Commands []ShapeCommand
}

// maxPolygonPoints caps the vertices of a polygon() shape. Anti-DoS guard,
// not spec-mandated: a hand-written or generated polygon holds a few
// hundred vertices; past the cap the value is invalid.
const maxPolygonPoints = 10000

// maxShapePathData caps the length of the data of a path() shape, in
// bytes. Anti-DoS guard, not spec-mandated: the path data of an icon or a
// drawn outline takes a few kilobytes.
const maxShapePathData = 1 << 20

// shapeSpec is a specified basic shape: lengths not yet computed.
type shapeSpec struct {
	kind    ShapeKind
	inset   [4]length
	rh, rv  [4]length
	radius  [2]radiusArg
	center  [2]length
	evenOdd bool
	points  [][2]length
	round   length
	path    string
	cmds    []shapeCmdSpec
}

// radiusArg is a specified <shape-radius>.
type radiusArg struct {
	side RadiusSide
	l    length
}

// clipPathSpec is a specified clip-path.
type clipPathSpec struct {
	url   string
	shape *shapeSpec
	box   GeometryBox
	boxed bool
}

// parseClipPath parses clip-path: none, a <url>, or a <basic-shape> and a
// <geometry-box> in either order, each optional (CSS Masking 1, 5.1).
func parseClipPath(v []token, c *declCtx) (any, bool) {
	if len(v) == 1 {
		if v[0].isIdent("none") {
			return clipPathSpec{}, true
		}
		if u, ok := parseURL(v[0], c.baseURL); ok {
			return clipPathSpec{url: u}, true
		}
	}
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	var out clipPathSpec
	for _, t := range v {
		if t.typ == tIdent {
			b, ok := geometryBoxes[lower(t.value)]
			if !ok || out.boxed && out.shape == nil {
				return nil, false
			}
			out.box, out.boxed = b, true
			continue
		}
		if out.shape != nil {
			return nil, false
		}
		s, ok := parseBasicShape(t)
		if !ok {
			return nil, false
		}
		out.shape, out.boxed = s, true
	}
	return out, true
}

// parseBasicShape parses a <basic-shape> function (CSS Shapes 1, 3.1).
func parseBasicShape(t token) (*shapeSpec, bool) {
	if t.typ != tFunction {
		return nil, false
	}
	args := significant(t.children)
	switch lower(t.value) {
	case "inset":
		return parseInset(args)
	case "rect":
		return parseRectShape(args)
	case "xywh":
		return parseXYWH(args)
	case "circle":
		return parseEllipse(args, true)
	case "ellipse":
		return parseEllipse(args, false)
	case "polygon":
		return parsePolygon(args)
	case "path":
		return parsePathShape(args)
	case "shape":
		return parseShapeFunction(args)
	}
	return nil, false
}

// splitRound splits the arguments of inset(), rect() and xywh() at round,
// returning the corner radii that follow it, every corner zero without
// one.
func splitRound(args []token) ([]token, [4]length, [4]length, bool) {
	var h, v [4]length
	for i := range args {
		if !args[i].isIdent("round") {
			continue
		}
		h, v, ok := parseRadii(args[i+1:])
		return args[:i], h, v, ok
	}
	return args, h, v, true
}

// parseInset parses the arguments of inset(): one to four offsets as
// margin takes them, then an optional round and border radii.
func parseInset(args []token) (*shapeSpec, bool) {
	args, rh, rv, ok := splitRound(args)
	if !ok || len(args) < 1 || len(args) > 4 {
		return nil, false
	}
	vals := make([]length, len(args))
	for i, a := range args {
		if vals[i], ok = parseLength(a, allowPct|allowMixed); !ok {
			return nil, false
		}
	}
	s := &shapeSpec{kind: ShapeInset, rh: rh, rv: rv}
	for i, k := range sideValues[len(args)-1] {
		s.inset[i] = vals[k]
	}
	return s, true
}

// parseRectShape parses the arguments of rect(): the top, right, bottom
// and left edges from the reference box's top or left edge, each a length
// or auto, which is the box's own edge; then an optional round. It
// computes to the inset of the same rectangle.
func parseRectShape(args []token) (*shapeSpec, bool) {
	args, rh, rv, ok := splitRound(args)
	if !ok || len(args) != 4 {
		return nil, false
	}
	var e [4]length
	for i, a := range args {
		switch {
		case a.isIdent("auto") && (Side(i) == Top || Side(i) == Left):
			e[i] = pctLength(0)
		case a.isIdent("auto"):
			e[i] = pctLength(100)
		default:
			if e[i], ok = parseLength(a, allowPct|allowMixed); !ok || e[i].expr != nil {
				return nil, false
			}
		}
	}
	s := &shapeSpec{kind: ShapeInset, rh: rh, rv: rv}
	s.inset = [4]length{e[Top], pctLength(100).add(e[Right].scale(-1)), pctLength(100).add(e[Bottom].scale(-1)), e[Left]}
	return s, true
}

// parseXYWH parses the arguments of xywh(): the left and top offsets, the
// width and the height, not negative, then an optional round. It computes
// to the inset of the same rectangle.
func parseXYWH(args []token) (*shapeSpec, bool) {
	args, rh, rv, ok := splitRound(args)
	if !ok || len(args) != 4 {
		return nil, false
	}
	var x [4]length
	for i, a := range args {
		f := allowPct | allowMixed
		if i >= 2 {
			f |= nonNeg
		}
		if x[i], ok = parseLength(a, f); !ok || x[i].expr != nil {
			return nil, false
		}
	}
	s := &shapeSpec{kind: ShapeInset, rh: rh, rv: rv}
	right := pctLength(100).add(x[0].scale(-1)).add(x[2].scale(-1))
	bottom := pctLength(100).add(x[1].scale(-1)).add(x[3].scale(-1))
	s.inset = [4]length{x[1], right, bottom, x[0]}
	return s, true
}

// parseEllipse parses the arguments of circle(), one radius, or ellipse(),
// none or two, each a length, a percentage, closest-side or farthest-side,
// then an optional at and a position.
func parseEllipse(args []token, circle bool) (*shapeSpec, bool) {
	s := &shapeSpec{kind: ShapeEllipse, center: [2]length{pctLength(50), pctLength(50)}}
	if circle {
		s.kind = ShapeCircle
	}
	var radii []token
	for i := range args {
		if args[i].isIdent("at") {
			p, ok := parsePosition(args[i+1:])
			if !ok {
				return nil, false
			}
			s.center = p
			args = args[:i]
			break
		}
	}
	radii = args
	want := 2
	if circle {
		want = 1
	}
	if len(radii) != 0 && len(radii) != want {
		return nil, false
	}
	for i, t := range radii {
		switch {
		case t.isIdent("closest-side"):
			s.radius[i] = radiusArg{side: RadiusClosest}
		case t.isIdent("farthest-side"):
			s.radius[i] = radiusArg{side: RadiusFarthest}
		case t.isIdent("closest-corner") && circle:
			s.radius[i] = radiusArg{side: RadiusClosestCorner}
		case t.isIdent("farthest-corner") && circle:
			s.radius[i] = radiusArg{side: RadiusFarthestCorner}
		default:
			l, ok := parseLength(t, allowPct|allowMixed|nonNeg)
			if !ok {
				return nil, false
			}
			s.radius[i] = radiusArg{side: RadiusLength, l: l}
		}
	}
	return s, true
}

// fillRule reads an optional leading <fill-rule> and its comma.
func fillRule(parts [][]token) ([][]token, bool, bool) {
	if len(parts) > 1 && len(parts[0]) == 1 && parts[0][0].typ == tIdent {
		switch lower(parts[0][0].value) {
		case "nonzero":
			return parts[1:], false, true
		case "evenodd":
			return parts[1:], true, true
		}
		return nil, false, false
	}
	return parts, false, true
}

// parsePolygon parses the arguments of polygon(): an optional fill rule,
// then vertices, each two length-percentages.
func parsePolygon(args []token) (*shapeSpec, bool) {
	parts := splitCommas(args)
	// polygon( <'fill-rule'>? [ round <length> ]? , ... ): the round
	// clause ends the first part.
	var round length
	if len(parts) > 1 {
		if first := parts[0]; len(first) >= 2 && first[len(first)-2].isIdent("round") {
			r, ok := parseLength(first[len(first)-1], nonNeg)
			if !ok {
				return nil, false
			}
			round = r
			parts[0] = first[:len(first)-2]
			if len(parts[0]) == 0 {
				parts = parts[1:]
			}
		}
	}
	parts, evenOdd, ok := fillRule(parts)
	if !ok || len(parts) == 0 || len(parts) > maxPolygonPoints {
		return nil, false
	}
	s := &shapeSpec{kind: ShapePolygon, evenOdd: evenOdd, round: round, points: make([][2]length, len(parts))}
	for i, p := range parts {
		if len(p) != 2 {
			return nil, false
		}
		for k := range 2 {
			if s.points[i][k], ok = parseLength(p[k], allowPct|allowMixed); !ok {
				return nil, false
			}
		}
	}
	return s, true
}

// parsePathShape parses the arguments of path(): an optional fill rule,
// then the path data as a string.
func parsePathShape(args []token) (*shapeSpec, bool) {
	parts, evenOdd, ok := fillRule(splitCommas(args))
	if !ok || len(parts) != 1 || len(parts[0]) != 1 || parts[0][0].typ != tString || len(parts[0][0].value) > maxShapePathData {
		return nil, false
	}
	return &shapeSpec{kind: ShapePath, evenOdd: evenOdd, path: parts[0][0].value}, true
}

// compute returns the computed shape of s.
func (s *shapeSpec) compute(lc lenCtx) *Shape {
	out := &Shape{Kind: s.kind, EvenOdd: s.evenOdd, Path: s.path}
	for i := range 4 {
		out.Inset[i] = s.inset[i].dim(lc, false)
		out.Radii[i] = Radius{H: s.rh[i].dim(lc, true), V: s.rv[i].dim(lc, true)}
	}
	for i, r := range s.radius {
		out.Radius[i] = ShapeRadius{Side: r.side}
		if r.side == RadiusLength {
			out.Radius[i].Length = r.l.dim(lc, true)
		}
	}
	if s.kind == ShapeCircle || s.kind == ShapeEllipse {
		out.Center = [2]Dimension{s.center[0].dim(lc, false), s.center[1].dim(lc, false)}
	}
	if len(s.cmds) > 0 {
		out.Commands = make([]ShapeCommand, len(s.cmds))
		for i, c := range s.cmds {
			out.Commands[i] = c.compute(lc)
		}
	}
	if len(s.points) > 0 {
		out.Round = max(s.round.points(lc), 0)
		out.Points = make([][2]Dimension, len(s.points))
		for i, p := range s.points {
			out.Points[i] = [2]Dimension{p[0].dim(lc, false), p[1].dim(lc, false)}
		}
	}
	return out
}

// clipPathProp is the clip-path property.
func clipPathProp() propDef {
	return propDef{name: "clip-path", parse: parseClipPath, compute: func(s *styler, st *Style, v any) {
		c, ok := v.(clipPathSpec)
		if !ok {
			return
		}
		st.ClipPath = ClipPath{URL: c.url, Box: c.box, Boxed: c.boxed}
		if c.shape != nil {
			st.ClipPath.Shape = c.shape.compute(s.lc)
		}
	}, copyFrom: field(func(s *Style) *ClipPath { return &s.ClipPath })}
}

// ClipRect is the clip of an absolutely positioned element (CSS 2.2,
// 11.1.2): a rectangle whose edges are offsets from the top left of its
// border box. The zero value is auto, which clips nothing.
type ClipRect struct {
	// Set reports rect(); Edges then hold the top, right, bottom and left
	// offsets, each Points or Auto, which is the border box's own edge.
	Set   bool
	Edges [4]Dimension
}

// parseClip parses clip: auto or rect() of four lengths or auto,
// separated by commas or, as user agents accept, by spaces (CSS 2.2,
// 11.1.2).
func parseClip(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.isIdent("auto") {
		return clipSpec{}, true
	}
	if t.typ != tFunction || lower(t.value) != "rect" {
		return nil, false
	}
	args := significant(t.children)
	parts := splitCommas(args)
	if len(parts) == 1 {
		parts = make([][]token, len(args))
		for i := range args {
			parts[i] = args[i : i+1]
		}
	}
	if len(parts) != 4 {
		return nil, false
	}
	out := clipSpec{set: true}
	for i, p := range parts {
		if len(p) != 1 {
			return nil, false
		}
		if p[0].isIdent("auto") {
			out.auto[i] = true
			continue
		}
		if out.edges[i], ok = parseLength(p[0], 0); !ok {
			return nil, false
		}
	}
	return out, true
}

// clipSpec is a specified clip.
type clipSpec struct {
	set   bool
	edges [4]length
	auto  [4]bool
}

// clipProp is the clip property.
func clipProp() propDef {
	return propDef{name: "clip", parse: parseClip, compute: func(s *styler, st *Style, v any) {
		c, ok := v.(clipSpec)
		if !ok {
			return
		}
		st.Clip = ClipRect{Set: c.set}
		if c.set {
			for i := range st.Clip.Edges {
				st.Clip.Edges[i] = Dimension{Kind: Auto}
				if !c.auto[i] {
					st.Clip.Edges[i] = c.edges[i].dim(s.lc, false)
				}
			}
		}
	}, copyFrom: field(func(s *Style) *ClipRect { return &s.Clip })}
}
