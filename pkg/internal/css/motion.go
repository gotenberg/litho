package css

import "math"

// MotionPath holds the longhands of CSS Motion Path 1, 2 of an element:
// offset-path, offset-distance, offset-rotate, offset-anchor and
// offset-position. None is inherited. A box with a path takes one more
// transform, between its individual transform properties and its
// transform list (CSS Transforms 2, 6): a translation that puts its
// anchor point on the path at its distance, then a rotation.
type MotionPath struct {
	Path OffsetPath
	// Distance is offset-distance, Points or Percent of the path's
	// length, or Calc; the zero value is 0.
	Distance Dimension
	// Rotate is offset-rotate.
	Rotate OffsetRotate
	// AnchorSet reports an offset-anchor other than auto: Anchor then
	// holds its horizontal and vertical offsets in the border box. auto
	// takes the transform origin (2.5).
	AnchorSet bool
	Anchor    [2]Dimension
	// Position is offset-position's keyword; Start holds its offsets in
	// the containing block when it is OffsetAt.
	Position OffsetPosition
	Start    [2]Dimension
}

// OffsetPathKind is what an offset-path names (CSS Motion Path 1, 2.1).
type OffsetPathKind uint8

// The offset paths.
const (
	// OffsetNone is none: the box has no offset transform.
	OffsetNone OffsetPathKind = iota
	// OffsetShape is a <basic-shape>, path() among them.
	OffsetShape
	// OffsetRay is ray().
	OffsetRay
	// OffsetURL is a reference to an SVG shape element.
	OffsetURL
	// OffsetBox is a <coord-box> alone: the box's edge, rounded as its
	// border-radius rounds it.
	OffsetBox
)

// OffsetPath is a computed offset-path.
type OffsetPath struct {
	Kind OffsetPathKind
	// Shape is the basic shape of OffsetShape.
	Shape *Shape
	// Ray is the ray of OffsetRay.
	Ray *Ray
	// URL is the resolved reference of OffsetURL.
	URL string
	// Box is the <coord-box> of the containing block the path is laid in,
	// border-box when left out: fill-box computes to content-box,
	// stroke-box and view-box to border-box, as for an element with a CSS
	// layout box.
	Box GeometryBox
}

// RaySize is the <ray-size> of a ray() (CSS Motion Path 1, 3.1).
type RaySize uint8

// The ray sizes; closest-side when left out.
const (
	RayClosestSide RaySize = iota
	RayClosestCorner
	RayFarthestSide
	RayFarthestCorner
	RaySides
)

// Ray is a computed ray().
type Ray struct {
	// Angle is the direction in radians, clockwise from up.
	Angle   float64
	Size    RaySize
	Contain bool
	// AtSet reports an at <position>, held in At as offsets in the
	// containing block.
	AtSet bool
	At    [2]Dimension
}

// OffsetRotate is a computed offset-rotate (CSS Motion Path 1, 2.4).
type OffsetRotate struct {
	// Fixed reports a value without auto: the box turns by Angle alone.
	// Otherwise it turns with the path's direction, plus Angle; reverse
	// is auto plus a half turn.
	Fixed bool
	// Angle is in radians, clockwise.
	Angle float64
}

// OffsetPosition is the keyword of offset-position (CSS Motion Path 1,
// 2.3).
type OffsetPosition uint8

// The offset-position values.
const (
	// OffsetNormal gives the box no offset starting position.
	OffsetNormal OffsetPosition = iota
	// OffsetAuto starts at the top-left corner of the box.
	OffsetAuto
	// OffsetAt starts at a <position> in the containing block.
	OffsetAt
)

// initial reports a MotionPath every longhand of which is initial.
func (m *MotionPath) initial() bool {
	return *m == MotionPath{}
}

// Motion returns the element's motion path longhands, nil when every one
// is initial. The result is shared: never modify it.
func (s *Style) Motion() *MotionPath { return s.MotionPath }

// HasOffsetPath reports an offset-path other than none, which gives the
// box an offset transform and all the effects of a transform (CSS Motion
// Path 1, 2.1).
func (s *Style) HasOffsetPath() bool {
	return s.MotionPath != nil && s.MotionPath.Path.Kind != OffsetNone
}

// coordBoxes are the <coord-box> keywords (CSS Motion Path 1, 2.1): the
// <paint-box>es and view-box.
var coordBoxes = map[string]GeometryBox{
	"border-box": GeoBorder, "padding-box": GeoPadding, "content-box": GeoContent,
	"fill-box": GeoContent, "stroke-box": GeoBorder, "view-box": GeoBorder,
}

// raySizes are the <ray-size> keywords.
var raySizes = map[string]RaySize{
	"closest-side": RayClosestSide, "closest-corner": RayClosestCorner,
	"farthest-side": RayFarthestSide, "farthest-corner": RayFarthestCorner, "sides": RaySides,
}

// offsetPathSpec is a specified offset-path.
type offsetPathSpec struct {
	kind  OffsetPathKind
	shape *shapeSpec
	ray   *raySpec
	url   string
	box   GeometryBox
}

// raySpec is a specified ray().
type raySpec struct {
	angle   float64
	size    RaySize
	contain bool
	atSet   bool
	at      [2]length
}

// rotateSpec is a specified offset-rotate.
type rotateSpec OffsetRotate

// positionSpec is a specified offset-anchor or offset-position.
type positionSpec struct {
	kind OffsetPosition
	at   [2]length
}

// parseOffsetPath parses offset-path: none | <offset-path> || <coord-box>
// (CSS Motion Path 1, 2.1), <offset-path> being ray(), a <url> or a
// <basic-shape>.
func parseOffsetPath(v []token, c *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return offsetPathSpec{}, true
	}
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	out := offsetPathSpec{kind: OffsetBox}
	var path, boxed bool
	for _, t := range v {
		if t.typ == tIdent {
			b, ok := coordBoxes[lower(t.value)]
			if !ok || boxed {
				return nil, false
			}
			out.box, boxed = b, true
			continue
		}
		if path {
			return nil, false
		}
		path = true
		if u, ok := parseURL(t, c.baseURL); ok {
			out.kind, out.url = OffsetURL, u
			continue
		}
		if t.typ == tFunction && lower(t.value) == "ray" {
			r, ok := parseRay(significant(t.children))
			if !ok {
				return nil, false
			}
			out.kind, out.ray = OffsetRay, r
			continue
		}
		s, ok := parseBasicShape(t)
		if !ok {
			return nil, false
		}
		out.kind, out.shape = OffsetShape, s
	}
	return out, true
}

// parseRay parses the arguments of ray(): <angle> && <ray-size>? &&
// contain? && [at <position>]? (CSS Motion Path 1, 3.1).
func parseRay(args []token) (*raySpec, bool) {
	r := &raySpec{}
	var angle, size bool
	for i := 0; i < len(args); i++ {
		t := args[i]
		if t.typ == tIdent {
			k := lower(t.value)
			switch {
			case k == "contain" && !r.contain:
				r.contain = true
				continue
			case k == "at" && !r.atSet:
				j := i + 1
				for j < len(args) && j-i <= 4 && isPositionToken(args[j]) {
					j++
				}
				p, ok := parsePosition(args[i+1 : j])
				if !ok {
					return nil, false
				}
				r.at, r.atSet = p, true
				i = j - 1
				continue
			}
			s, ok := raySizes[k]
			if !ok || size {
				return nil, false
			}
			r.size, size = s, true
			continue
		}
		a, ok := angleOf(t)
		if !ok || angle {
			return nil, false
		}
		r.angle, angle = a, true
	}
	return r, angle
}

// angleOf parses an <angle> into radians: a dimension of an angle unit,
// or a unitless zero.
func angleOf(t token) (float64, bool) {
	if t.typ == tNumber && t.num != 0 {
		return 0, false
	}
	d, ok := parseAngle(t)
	if !ok {
		return 0, false
	}
	return float64(d*math.Pi) / 180, true
}

// parseOffsetRotate parses offset-rotate: [ auto | reverse ] || <angle>
// (CSS Motion Path 1, 2.4).
func parseOffsetRotate(v []token, _ *declCtx) (any, bool) {
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	out := rotateSpec{Fixed: true}
	var kw, angle bool
	for _, t := range v {
		if t.typ == tIdent {
			k := lower(t.value)
			if kw || k != "auto" && k != "reverse" {
				return nil, false
			}
			kw, out.Fixed = true, false
			if k == "reverse" {
				out.Angle += math.Pi
			}
			continue
		}
		a, ok := angleOf(t)
		if !ok || angle {
			return nil, false
		}
		angle = true
		out.Angle += a
	}
	return out, true
}

// parseOffsetDistance parses offset-distance: <length-percentage> (CSS
// Motion Path 1, 2.2).
func parseOffsetDistance(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	return parseLength(t, allowPct|allowMixed)
}

// parseOffsetAnchor parses offset-anchor: auto | <position> (CSS Motion
// Path 1, 2.5).
func parseOffsetAnchor(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "auto" {
		return positionSpec{}, true
	}
	p, ok := parsePosition(v)
	return positionSpec{kind: OffsetAt, at: p}, ok
}

// parseOffsetPosition parses offset-position: normal | auto | <position>
// (CSS Motion Path 1, 2.3).
func parseOffsetPosition(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok {
		switch k {
		case "normal":
			return positionSpec{}, true
		case "auto":
			return positionSpec{kind: OffsetAuto}, true
		}
	}
	p, ok := parsePosition(v)
	return positionSpec{kind: OffsetAt, at: p}, ok
}

// computePosition returns the computed offsets of a position.
func computePosition(s *styler, p [2]length) [2]Dimension {
	return [2]Dimension{p[0].dim(s.lc, false), p[1].dim(s.lc, false)}
}

// motionProps returns the longhands of CSS Motion Path 1.
func motionProps() map[propID]propDef {
	return map[propID]propDef{
		pOffsetPath: motionLonghand("offset-path", parseOffsetPath, func(s *styler, m *MotionPath, v any) {
			p, ok := v.(offsetPathSpec)
			if !ok {
				return
			}
			m.Path = OffsetPath{Kind: p.kind, URL: p.url, Box: p.box}
			if p.shape != nil {
				m.Path.Shape = p.shape.compute(s.lc)
			}
			if r := p.ray; r != nil {
				m.Path.Ray = &Ray{Angle: r.angle, Size: r.size, Contain: r.contain, AtSet: r.atSet}
				if r.atSet {
					m.Path.Ray.At = computePosition(s, r.at)
				}
			}
		}, func(dst, src *MotionPath) { dst.Path = src.Path }),
		pOffsetDistance: motionLonghand("offset-distance", parseOffsetDistance, func(s *styler, m *MotionPath, v any) {
			if l, ok := v.(length); ok {
				m.Distance = l.dim(s.lc, false)
				if m.Distance == Pts(0) {
					m.Distance = Dimension{}
				}
			}
		}, func(dst, src *MotionPath) { dst.Distance = src.Distance }),
		pOffsetRotate: motionLonghand("offset-rotate", parseOffsetRotate, func(_ *styler, m *MotionPath, v any) {
			if r, ok := v.(rotateSpec); ok {
				m.Rotate = OffsetRotate(r)
			}
		}, func(dst, src *MotionPath) { dst.Rotate = src.Rotate }),
		pOffsetAnchor: motionLonghand("offset-anchor", parseOffsetAnchor, func(s *styler, m *MotionPath, v any) {
			if p, ok := v.(positionSpec); ok {
				m.AnchorSet, m.Anchor = p.kind == OffsetAt, [2]Dimension{}
				if m.AnchorSet {
					m.Anchor = computePosition(s, p.at)
				}
			}
		}, func(dst, src *MotionPath) { dst.AnchorSet, dst.Anchor = src.AnchorSet, src.Anchor }),
		pOffsetPosition: motionLonghand("offset-position", parseOffsetPosition, func(s *styler, m *MotionPath, v any) {
			if p, ok := v.(positionSpec); ok {
				m.Position, m.Start = p.kind, [2]Dimension{}
				if p.kind == OffsetAt {
					m.Start = computePosition(s, p.at)
				}
			}
		}, func(dst, src *MotionPath) { dst.Position, dst.Start = src.Position, src.Start }),
	}
}

// motionLonghand defines a non-inherited longhand held in
// [Style.MotionPath]: set stores a computed value into a copy, cp copies
// the field.
func motionLonghand(name string, parse parseFn, set func(*styler, *MotionPath, any), cp func(dst, src *MotionPath)) propDef {
	return propDef{
		name:  name,
		parse: parse,
		compute: func(s *styler, st *Style, v any) {
			setMotion(st, func(m *MotionPath) { set(s, m, v) })
		},
		copyFrom: func(dst, src *Style) {
			if dst.MotionPath == src.MotionPath {
				return
			}
			from := src.MotionPath
			if from == nil {
				from = &MotionPath{}
			}
			setMotion(dst, func(m *MotionPath) { cp(m, from) })
		},
	}
}

// setMotion applies f to a copy of st's motion path longhands, storing nil
// when every one ends initial. The shared value is never mutated.
func setMotion(st *Style, f func(*MotionPath)) {
	n := MotionPath{}
	if st.MotionPath != nil {
		n = *st.MotionPath
	}
	f(&n)
	if n.initial() {
		st.MotionPath = nil
		return
	}
	st.MotionPath = &n
}

// expandOffset expands offset: [ <'offset-position'>? [ <'offset-path'>
// [ <'offset-distance'> || <'offset-rotate'> ]? ]? ]! [ / <'offset-anchor'>
// ]? (CSS Motion Path 1, 2.6). The longhands it leaves out take their
// initial values.
func expandOffset(v []token, c *declCtx) ([]entry, bool) {
	out := []entry{
		{pOffsetPosition, wideInitial},
		{pOffsetPath, wideInitial},
		{pOffsetDistance, wideInitial},
		{pOffsetRotate, wideInitial},
		{pOffsetAnchor, wideInitial},
	}
	main, anchor := v, []token(nil)
	for i, t := range v {
		if t.isDelim('/') {
			main, anchor = v[:i], v[i+1:]
			if len(anchor) == 0 {
				return nil, false
			}
			break
		}
	}
	if anchor != nil {
		a, ok := parseOffsetAnchor(anchor, c)
		if !ok {
			return nil, false
		}
		out[4].v = a
	}
	if len(main) == 0 {
		return nil, false
	}
	i := 0
	if main[0].isIdent("auto") || main[0].isIdent("normal") {
		p, _ := parseOffsetPosition(main[:1], c)
		out[0].v, i = p, 1
	} else {
		j := 0
		for j < len(main) && j < 4 && isPositionToken(main[j]) && !main[j].isIdent("none") {
			j++
		}
		if j > 0 {
			p, ok := parseOffsetPosition(main[:j], c)
			if !ok {
				return nil, false
			}
			out[0].v, i = p, j
		}
	}
	j := i
	for j < len(main) && j-i < 2 && pathToken(main[j]) {
		j++
	}
	if j > i {
		p, ok := parseOffsetPath(main[i:j], c)
		if !ok {
			return nil, false
		}
		out[1].v = p
	} else if i < len(main) {
		return nil, false
	}
	var dist, rot []token
	for _, t := range main[j:] {
		if l, ok := parseLength(t, allowPct|allowMixed); ok && dist == nil {
			out[2].v, dist = l, []token{t}
			continue
		}
		rot = append(rot, t)
	}
	if len(rot) > 0 {
		r, ok := parseOffsetRotate(rot, c)
		if !ok {
			return nil, false
		}
		out[3].v = r
	}
	return out, true
}

// pathToken reports whether t can be part of an offset-path value: a
// function, a URL, a <coord-box> or none.
func pathToken(t token) bool {
	switch t.typ { //nolint:exhaustive // the other token types are no part of a path
	case tFunction, tURL:
		return true
	case tIdent:
		_, ok := coordBoxes[lower(t.value)]
		return ok || t.isIdent("none")
	}
	return false
}
