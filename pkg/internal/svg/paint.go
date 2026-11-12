package svg

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// maxHrefChain caps the gradients one gradient's href chain visits.
// Anti-DoS guard, not spec-mandated: SVG 1.1, 13.2.2 makes the chain
// acyclic, and real documents chain one or two.
const maxHrefChain = 16

// fillOf returns the fill of a shape styled st whose bounding box is box,
// nil for none (SVG 1.1, 11.3).
func (r *renderer) fillOf(st *style, box bbox) *compose.Fill {
	if st.fillOpacity <= 0 {
		return nil
	}
	p := st.fill
	if p.kind == paintURL {
		sh, fb, ok := r.shading(p, st, box)
		if ok {
			if sh == nil {
				return nil
			}
			f := &compose.Fill{Rule: st.fillRule, Shading: sh}
			return withAlpha(f, st.fillOpacity)
		}
		if fb == nil {
			return nil
		}
		p = *fb
	}
	c, ok := r.colorOf(p, st)
	if !ok {
		return nil
	}
	c.A *= st.fillOpacity
	if c.A <= 0 {
		return nil
	}
	return &compose.Fill{Color: c, Rule: st.fillRule}
}

// colorOf returns the flat colour of a paint: a colour, or currentColor
// resolved to st's color. It reports false for none.
func (r *renderer) colorOf(p paint, st *style) (compose.Color, bool) {
	switch p.kind {
	case paintColor:
		return p.color, p.color.A > 0
	case paintCurrent:
		return st.color, st.color.A > 0
	case paintNone, paintURL:
	}
	return compose.Color{}, false
}

// flatPaint returns one colour for a paint the composition model cannot
// draw with a shading, a stroke's or a glyph run's: a gradient becomes the
// colour of its middle, with a warning.
func (r *renderer) flatPaint(p paint, st *style, box bbox, what string) (compose.Color, bool) {
	if p.kind != paintURL {
		return r.colorOf(p, st)
	}
	sh, fb, ok := r.shading(p, st, box)
	if ok {
		if sh == nil {
			return compose.Color{}, false
		}
		r.d.warn("unsupported-svg-paint", fmt.Sprintf("a gradient %s is drawn in the gradient's middle colour", what))
		return middleColor(sh.Stops), true
	}
	if fb == nil {
		return compose.Color{}, false
	}
	return r.colorOf(*fb, st)
}

// middleColor returns the colour a stop list reaches at offset 0.5.
func middleColor(stops []compose.ColorStop) compose.Color {
	for i := 1; i < len(stops); i++ {
		a, b := stops[i-1], stops[i]
		if b.Offset >= 0.5 {
			t := 0.5
			if b.Offset > a.Offset {
				t = (0.5 - a.Offset) / (b.Offset - a.Offset)
			}
			t = min(max(t, 0), 1)
			return compose.Color{
				R: a.Color.R + t*(b.Color.R-a.Color.R), G: a.Color.G + t*(b.Color.G-a.Color.G),
				B: a.Color.B + t*(b.Color.B-a.Color.B), A: a.Color.A + t*(b.Color.A-a.Color.A),
			}
		}
	}
	return stops[len(stops)-1].Color
}

// gradientAttrs are the attributes a gradient inherits through its href
// chain (SVG 1.1, 13.2.2 and 13.2.3).
var gradientAttrs = []string{
	"gradientUnits", "gradientTransform", "spreadMethod",
	"x1", "y1", "x2", "y2", "cx", "cy", "r", "fx", "fy", "fr",
}

// gradient is a resolved gradient element: its own attributes merged with
// those its href chain gives, and its stops.
type gradient struct {
	el     *html.Node
	radial bool
	attrs  map[string]string
	stops  []*html.Node
}

// resolveGradient follows a paint server reference to a gradient, merging
// the attributes and stops of its href chain. It reports false when the
// reference does not name a gradient.
func (r *renderer) resolveGradient(id string) (*gradient, bool) {
	el := r.d.ids[id]
	if el == nil || el.Data != "linearGradient" && el.Data != "radialGradient" {
		if el != nil && el.Data == "pattern" {
			r.d.warn("unsupported-svg-paint", "patterns are not drawn; their fallback colour is")
		}
		return nil, false
	}
	g := &gradient{el: el, radial: el.Data == "radialGradient", attrs: map[string]string{}}
	seen := map[*html.Node]bool{}
	for cur := el; cur != nil && !seen[cur] && len(seen) < maxHrefChain; {
		seen[cur] = true
		if cur.Data == "linearGradient" || cur.Data == "radialGradient" {
			for _, k := range gradientAttrs {
				if _, done := g.attrs[k]; done {
					continue
				}
				// The geometry attributes only carry over between
				// gradients of the same kind.
				geom := k != "gradientUnits" && k != "gradientTransform" && k != "spreadMethod"
				if geom && (cur.Data == "radialGradient") != g.radial {
					continue
				}
				if v, ok := attrOK(cur, k); ok {
					g.attrs[k] = v
				}
			}
			if g.stops == nil {
				for c := cur.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && c.Namespace == "svg" && c.Data == "stop" {
						if len(g.stops) >= maxGradientStops {
							r.d.warn("svg-limit", fmt.Sprintf("gradient stops past %d were left out", maxGradientStops))
							break
						}
						g.stops = append(g.stops, c)
					}
				}
			}
		}
		ref, ok := strings.CutPrefix(strings.TrimSpace(href(cur)), "#")
		if !ok {
			break
		}
		cur = r.d.ids[ref]
	}
	return g, true
}

// shading returns the shading of the gradient paint p for a shape styled
// st whose bounding box is box (SVG 1.1, 13.2). It reports ok false when
// the reference names no gradient, with the fallback to use instead; ok
// true with a nil shading when the gradient paints nothing.
func (r *renderer) shading(p paint, st *style, box bbox) (*compose.Shading, *paint, bool) {
	g, ok := r.resolveGradient(p.url)
	if !ok {
		return nil, p.fallback, false
	}
	stops := r.stops(g)
	if len(stops) == 0 {
		// No stop paints nothing (SVG 1.1, 13.2.4).
		return nil, nil, true
	}
	units := g.attrs["gradientUnits"]
	bboxUnits := units != "userSpaceOnUse"
	m := identity
	if t, ok := g.attrs["gradientTransform"]; ok {
		if gm, good := parseTransform(t); good {
			m = gm
		}
	}
	lc := r.lc(st)
	if bboxUnits {
		w, h := box.x1-box.x0, box.y1-box.y0
		if !box.ok || !positive(w) || !positive(h) {
			// A bounding box without area cannot carry a gradient in its
			// units: the gradient is not drawn (SVG 1.1, 7.11).
			return nil, nil, true
		}
		m = mul(compose.Matrix{A: w, D: h, E: box.x0, F: box.y0}, m)
		lc = lengthCtx{fontSize: st.fontSize, vw: 1, vh: 1}
	}
	if !invertible(m) {
		return nil, nil, true
	}
	coord := func(k string, a axis, def string) float64 {
		v, ok := g.attrs[k]
		if !ok {
			v = def
		}
		l, good := parseLength(v)
		if !good {
			l, _ = parseLength(def)
		}
		if bboxUnits && l.u == unitPercent {
			return l.v / 100
		}
		return l.resolve(lc, a)
	}
	if sm := g.attrs["spreadMethod"]; sm == "reflect" || sm == "repeat" {
		r.d.warn("unsupported-svg-paint", "gradient spreadMethod "+sm+" is drawn as pad")
	}
	sh := &compose.Shading{Stops: stops, Transform: m}
	last := stops[len(stops)-1].Color
	solid := func() (*compose.Shading, *paint, bool) {
		// A degenerate gradient paints the last stop's colour (SVG 1.1,
		// 13.2.2 and 13.2.3).
		c := last
		if c.A <= 0 {
			return nil, nil, true
		}
		return &compose.Shading{
			Kind: compose.Axial, From: compose.Point{}, To: compose.Point{X: 1},
			Stops:     []compose.ColorStop{{Offset: 0, Color: c}, {Offset: 1, Color: c}},
			Transform: identity,
		}, nil, true
	}
	if !g.radial {
		sh.Kind = compose.Axial
		sh.From = compose.Point{X: coord("x1", axisX, "0%"), Y: coord("y1", axisY, "0%")}
		sh.To = compose.Point{X: coord("x2", axisX, "100%"), Y: coord("y2", axisY, "0%")}
		if sh.From == sh.To || len(stops) == 1 {
			return solid()
		}
		return sh, nil, true
	}
	cx, cy := coord("cx", axisX, "50%"), coord("cy", axisY, "50%")
	rad := coord("r", axisDiag, "50%")
	fx, fy := cx, cy
	if _, ok := g.attrs["fx"]; ok {
		fx = coord("fx", axisX, "50%")
	}
	if _, ok := g.attrs["fy"]; ok {
		fy = coord("fy", axisY, "50%")
	}
	fr := max(coord("fr", axisDiag, "0%"), 0)
	if !positive(rad) || len(stops) == 1 {
		return solid()
	}
	// SVG 1.1, 13.2.3: a focal point outside the end circle moves onto it.
	// It stays just inside, where the cone the two circles make is still
	// a full disc.
	if d := math.Hypot(fx-cx, fy-cy); d > rad*0.999 {
		k := rad * 0.999 / d
		fx, fy = cx+(fx-cx)*k, cy+(fy-cy)*k
	}
	if fx == cx && fy == cy && fr == rad {
		return solid()
	}
	sh.Kind = compose.Radial
	sh.From, sh.FromRadius = compose.Point{X: fx, Y: fy}, fr
	sh.To, sh.ToRadius = compose.Point{X: cx, Y: cy}, rad
	return sh, nil, true
}

// stops returns the colour stops of g (SVG 1.1, 13.2.4): each offset a
// number or a percentage clamped to [0, 1] and to at least the previous
// one, each colour its stop-color and stop-opacity, padded so that the
// list starts at 0 and ends at 1 as the composition model asks. A list of
// fully transparent stops paints nothing and yields nil.
func (r *renderer) stops(g *gradient) []compose.ColorStop {
	var out []compose.ColorStop
	prev := 0.0
	for _, n := range g.stops {
		ss := r.d.styleOf(n, 0)
		off := 0.0
		if v, ok := attrOK(n, "offset"); ok {
			if a, good := parseAlpha(v); good {
				off = a
			}
		}
		off = max(off, prev)
		prev = off
		c, ok := r.colorOf(ss.stopColor, ss)
		if !ok {
			c = compose.Color{R: ss.stopColor.color.R, G: ss.stopColor.color.G, B: ss.stopColor.color.B}
		}
		c.A *= ss.stopAlpha
		out = append(out, compose.ColorStop{Offset: off, Color: c})
	}
	if len(out) == 0 || !anyOpaque(out) {
		return nil
	}
	if out[0].Offset > 0 {
		out = append([]compose.ColorStop{{Offset: 0, Color: out[0].Color}}, out...)
	}
	if out[len(out)-1].Offset < 1 {
		out = append(out, compose.ColorStop{Offset: 1, Color: out[len(out)-1].Color})
	}
	return out
}

// anyOpaque reports whether a stop is not fully transparent.
func anyOpaque(stops []compose.ColorStop) bool {
	for _, s := range stops {
		if s.Color.A > 0 {
			return true
		}
	}
	return false
}

// maxClipChain caps the clip paths one clip-path reference chains through
// the clip-path of a clip path or of its child. Anti-DoS guard, not
// spec-mandated: SVG 1.1, 14.3.5 forbids circular references, which this
// also stops, and real drawings chain one or two.
const maxClipChain = 8

// maxClipRegions caps the regions one clip-path reference unites, each
// painting the clipped content once. Anti-DoS guard, not spec-mandated:
// a clip path of shapes that each clip themselves, or that mix clip
// rules, holds a handful.
const maxClipRegions = 8

// clipRegion is one region of a clip path: the intersection of its paths,
// which nest as clipping groups.
type clipRegion []*compose.Path

// clipMember is one child of a clip path: its outline in the clip path's
// coordinates, its clip rule, and the clip path it names itself, with the
// element, the transform and the bounding box that clip path resolves in.
type clipMember struct {
	ops  []compose.PathOp
	rule compose.FillRule
	clip string
	el   *html.Node
	m    compose.Matrix
	box  bbox
}

// clipPath returns the clip path id names for the element n (SVG 1.1,
// 14.3.5), in n's user space, as regions whose union it is: each child
// shape's or text's outline under its transform and the clip path's, in
// bounding box units when clipPathUnits asks, intersected with the clip
// path that child names; the regions then intersect the clip path the
// clip path itself names. Children without a clip path of their own and
// with the nonzero rule merge into one region, the nonzero union of
// their outlines; a child with the evenodd rule or its own clip path
// stands alone, since a single path cannot unite them. Glyphs clip with
// the nonzero rule, as they fill.
//
// It reports empty for a clip path that has nothing to clip with, which
// hides n; a reference that does not name a clipPath, or that leads back
// into the chain, returns no region and clips nothing. seen holds the clip
// paths of the chain so far.
func (r *renderer) clipPath(n *html.Node, id string, box *bbox, seen []*html.Node) ([]clipRegion, bool) {
	cp := r.d.ids[id]
	if cp == nil || cp.Data != "clipPath" {
		return nil, false
	}
	if slices.Contains(seen, cp) || len(seen) >= maxClipChain {
		// A reference back into the chain is an error (SVG 1.1, 14.3.5);
		// the link is dropped and the rest of the chain clips, as resvg
		// draws it.
		return nil, false
	}
	seen = append(seen, cp)
	cs := r.d.styleOf(cp, 0)
	m := identity
	if t, ok := attrOK(cp, "transform"); ok {
		if tm, good := parseTransform(t); good {
			m = tm
		}
	}
	if attrOf(cp, "clipPathUnits") == "objectBoundingBox" {
		var b bbox
		if box != nil {
			b = *box
		} else {
			b = r.bboxOf(n, cs, identity, 0)
		}
		if !b.hasArea() {
			return nil, true
		}
		m = mul(compose.Matrix{A: b.x1 - b.x0, D: b.y1 - b.y0, E: b.x0, F: b.y0}, m)
	}
	var members []clipMember
	for c := cp.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Namespace != "svg" {
			continue
		}
		if mb, ok := r.clipChild(c, m); ok {
			members = append(members, mb)
		}
	}
	var regions []clipRegion
	var merged []compose.PathOp
	plain := 0
	for _, mb := range members {
		if mb.clip == "" && mb.rule == compose.NonZero {
			merged = append(merged, mb.ops...)
			plain++
		}
	}
	if plain > 0 {
		regions = append(regions, clipRegion{{Ops: merged, Fill: &compose.Fill{Rule: compose.NonZero}}})
	}
	for _, mb := range members {
		if mb.clip == "" && mb.rule == compose.NonZero {
			continue
		}
		own := &compose.Path{Ops: mb.ops, Fill: &compose.Fill{Rule: mb.rule}}
		if mb.clip == "" {
			regions = append(regions, clipRegion{own})
			continue
		}
		// The child's clip path is in the child's user space.
		sub, empty := r.clipPath(mb.el, mb.clip, &mb.box, seen)
		if empty {
			continue
		}
		if len(sub) == 0 {
			regions = append(regions, clipRegion{own})
			continue
		}
		for _, sr := range sub {
			reg := clipRegion{own}
			for _, p := range sr {
				reg = append(reg, &compose.Path{Ops: transformOps(p.Ops, mb.m), Fill: p.Fill})
			}
			regions = append(regions, reg)
		}
	}
	if len(regions) == 0 {
		return nil, true
	}
	if cs.clipPath != "" {
		sub, empty := r.clipPath(n, cs.clipPath, box, seen)
		if empty {
			return nil, true
		}
		if len(sub) > 0 {
			var both []clipRegion
			for _, a := range regions {
				for _, b := range sub {
					both = append(both, append(slices.Clone(a), b...))
				}
			}
			regions = both
		}
	}
	if len(regions) > maxClipRegions {
		r.d.warn("svg-limit", fmt.Sprintf("a clip path uniting more than %d regions clips to the first %d", maxClipRegions, maxClipRegions))
		regions = regions[:maxClipRegions]
	}
	return regions, false
}

// clipChild returns the child c of a clip path whose transform is m as a
// member of its union: a shape, a use referencing a shape directly (SVG
// 1.1, 14.3.5) or a text element. It reports false for anything else, or
// for a child that is not displayed, not visible or empty.
func (r *renderer) clipChild(c *html.Node, m compose.Matrix) (clipMember, bool) {
	el := c
	local := identity
	if el.Data == "use" {
		ref, ok := strings.CutPrefix(strings.TrimSpace(href(el)), "#")
		t := r.d.ids[ref]
		if !ok || t == nil || !shapes[t.Data] && t.Data != "text" {
			return clipMember{}, false
		}
		us := r.d.styleOf(el, 0)
		ux := r.length(el, "x", us, axisX, 0)
		uy := r.length(el, "y", us, axisY, 0)
		if tr, ok := attrOK(el, "transform"); ok {
			if tm, good := parseTransform(tr); good {
				local = tm
			}
		}
		local = mul(local, translate(ux, uy))
		el = t
	}
	if !shapes[el.Data] && el.Data != "text" {
		return clipMember{}, false
	}
	es := r.d.styleOf(el, 0)
	if !es.displayed {
		return clipMember{}, false
	}
	if tr, ok := attrOK(el, "transform"); ok {
		if tm, good := parseTransform(tr); good {
			local = mul(local, tm)
		}
	}
	var g []compose.PathOp
	rule := es.clipRule
	if el.Data == "text" {
		g = r.textOutline(el, es)
		rule = compose.NonZero
	} else if es.visible {
		g = r.geometry(el, es)
	}
	if len(g) == 0 {
		return clipMember{}, false
	}
	cm := mul(m, local)
	return clipMember{ops: transformOps(g, cm), rule: rule, clip: es.clipPath, el: el, m: cm, box: opsBBox(g)}, true
}

// bboxOf returns the bounding box of the element n styled st, in the
// coordinates m maps its user space to (SVG 1.1, 7.11): its outline for a
// shape, the union of its children's for a container. Text and images
// count by their positions alone.
func (r *renderer) bboxOf(n *html.Node, st *style, m compose.Matrix, depth int) bbox {
	var b bbox
	r.bboxSteps++
	if depth > maxDepth || r.bboxSteps > maxBBoxSteps {
		return b
	}
	switch {
	case shapes[n.Data]:
		for _, op := range transformOps(r.geometry(n, st), m) {
			for _, p := range op.P[:pointsOf(op.Kind)] {
				b.add(p)
			}
		}
	case n.Data == "image":
		x, y := r.length(n, "x", st, axisX, 0), r.length(n, "y", st, axisY, 0)
		w, h := r.length(n, "width", st, axisX, 0), r.length(n, "height", st, axisY, 0)
		b.add(apply(m, compose.Point{X: x, Y: y}))
		b.add(apply(m, compose.Point{X: x + w, Y: y + h}))
	case containers[n.Data] || n.Data == "use":
		kids := n
		km := m
		if n.Data == "use" {
			id, _ := strings.CutPrefix(strings.TrimSpace(href(n)), "#")
			t := r.d.ids[id]
			if t == nil || isAncestor(t, n) {
				return b
			}
			km = mul(m, translate(r.length(n, "x", st, axisX, 0), r.length(n, "y", st, axisY, 0)))
			if t.Data != "symbol" && t.Data != "svg" {
				return r.childBBox(t, st, km, depth)
			}
			kids = t
		}
		for c := kids.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Namespace == "svg" {
				cb := r.childBBox(c, st, km, depth)
				if cb.ok {
					b.add(compose.Point{X: cb.x0, Y: cb.y0})
					b.add(compose.Point{X: cb.x1, Y: cb.y1})
				}
			}
		}
	}
	return b
}

// childBBox returns the bounding box of c, a child of an element styled
// parent, under its own transform.
func (r *renderer) childBBox(c *html.Node, parent *style, m compose.Matrix, depth int) bbox {
	cs := r.d.computeStyle(c, parent)
	if !cs.displayed {
		return bbox{}
	}
	if t, ok := attrOK(c, "transform"); ok {
		if tm, good := parseTransform(t); good {
			m = mul(m, tm)
		}
	}
	return r.bboxOf(c, cs, m, depth+1)
}

// pointsOf returns how many points an operator of kind k uses.
func pointsOf(k compose.OpKind) int {
	switch k {
	case compose.MoveTo, compose.LineTo:
		return 1
	case compose.CurveTo:
		return 3
	case compose.ClosePath:
	}
	return 0
}
