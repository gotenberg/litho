package svg

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// Transforms become typeset groups rather than being applied to the
// points: a group transforms a gradient, a dash pattern, a stroke's width
// and a glyph run exactly as the SVG user space does (SVG 1.1, 7.4), which
// moving the points alone would not. Consecutive shapes under one
// transform share one group, and a group opened for opacity or a clip
// carries the transform in force, so the nesting stays shallow.

// container collects the fragments of one typeset group.
type container struct {
	out []typeset.Fragment
	// last is the transform group appended last, lastM its transform, so
	// consecutive shapes under one transform share it.
	last  *typeset.Group
	lastM typeset.Matrix
}

// sink is where an element paints: a container, the transform from the
// element's user space to the container's coordinates, the groups open
// above it and the opacity folded into its colours.
type sink struct {
	c       *container
	pending typeset.Matrix
	nest    int
	alpha   float64
}

// with returns the sink of a child whose transform is local.
func (s sink) with(local typeset.Matrix) sink {
	s.pending = mul(s.pending, local)
	return s
}

// renderer draws one Document at one viewport size.
type renderer struct {
	// ctx is the context of the one rendering the renderer serves: it
	// lives within [Document.render] and never past it.
	ctx context.Context
	d   *Document
	// vw and vh are the viewport percentages resolve against, in user
	// units.
	vw, vh float64
	frags  int
	uses   int
	active map[*html.Node]bool
	full   bool
	count  int
	// bboxSteps counts the elements bounding boxes visited, runes the
	// text characters collected.
	bboxSteps int
	runes     int
	// outlines holds the glyph outlines of each face by key, nil for a
	// face whose outlines cannot be read; glyphsOutlined counts the glyphs
	// converted to paths.
	outlines       map[string]*outlineFont
	glyphsOutlined int
	// masking holds the mask elements whose content is being drawn, so a
	// mask reached again from its own content is dropped; blends reports
	// a group drawn under a blend mode.
	masking map[*html.Node]bool
	blends  bool
	// filterPixels counts the pixels of the filtered elements drawn.
	filterPixels effect.Page
}

func newRenderer(ctx context.Context, d *Document, vw, vh float64) *renderer {
	return &renderer{ctx: ctx, d: d, vw: vw, vh: vh, active: map[*html.Node]bool{}}
}

// lc returns the length context of an element styled st.
func (r *renderer) lc(st *style) lengthCtx {
	return lengthCtx{fontSize: st.fontSize, vw: r.vw, vh: r.vh}
}

// length reads the length attribute key of n in user units, def when it
// is absent or invalid.
func (r *renderer) length(n *html.Node, key string, st *style, a axis, def float64) float64 {
	l, ok := parseLength(attrOf(n, key))
	if !ok {
		return def
	}
	return l.resolve(r.lc(st), a)
}

// root draws the root element and returns its fragments in user units.
func (r *renderer) root() []typeset.Fragment {
	c := &container{}
	st := r.d.computeStyle(r.d.root, initialStyle(r.d.opts.Context))
	if !st.displayed {
		return nil
	}
	s := sink{c: c, pending: identity, alpha: 1}
	inner, done := r.open(s, r.d.root, st, nil)
	r.children(r.d.root, st, inner, 0)
	done()
	if r.blends && len(c.out) > 0 {
		// What blends inside the drawing blends with the drawing alone:
		// an SVG root is an isolated group (CSS Compositing 1, 3.3).
		return []typeset.Fragment{&typeset.Group{Opacity: 1, Transparency: &typeset.TransparencyGroup{Isolated: true}, Content: c.out}}
	}
	return c.out
}

// stop reports whether drawing must end: the context is done, or a cap is
// reached.
func (r *renderer) stop() bool {
	if r.full {
		return true
	}
	r.count++
	if r.count%256 == 0 && r.ctx.Err() != nil {
		r.full = true
	}
	if r.count > maxElements {
		r.d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("the drawing instantiates more than %d elements; the rest were left out", maxElements))
		r.full = true
	}
	return r.full
}

// children draws the element children of n, styled parent.
func (r *renderer) children(n *html.Node, parent *style, s sink, depth int) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Namespace == "svg" {
			r.element(c, parent, s, depth+1)
		}
	}
}

// containers are the elements that draw their children (SVG 1.1, 1.6).
var containers = map[string]bool{"g": true, "a": true, "svg": true, "switch": true}

// shapes are the basic shapes and path (SVG 1.1, 8 and 9).
var shapes = map[string]bool{
	"rect": true, "circle": true, "ellipse": true, "line": true, "polyline": true,
	"polygon": true, "path": true,
}

// unsupported names the elements left out with a warning, and why.
var unsupported = map[string]string{
	"foreignObject": "foreign objects are not drawn",
	"textPath":      "text on a path is not drawn",
	"video":         "video is not drawn",
	"audio":         "audio is not drawn",
	"iframe":        "embedded documents are not drawn",
	"canvas":        "canvas is not drawn",
}

// element draws the element n, a child of an element styled parent. Any
// element the renderer does not draw is skipped with its subtree, as SVG
// 1.1, 1.6 and 19.1 ask of unknown elements and of definitions such as
// defs, symbol, gradients and clipPath.
func (r *renderer) element(n *html.Node, parent *style, s sink, depth int) {
	name := n.Data
	if msg, ok := unsupported[name]; ok {
		r.d.warn(warn.ContentUnsupported, warn.Omitted, msg)
		return
	}
	if !containers[name] && !shapes[name] && name != "use" && name != "text" && name != "image" {
		return
	}
	if depth > maxDepth {
		r.d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("elements nested deeper than %d were left out", maxDepth))
		return
	}
	if r.stop() || !passes(n) {
		return
	}
	st := r.d.computeStyle(n, parent)
	if !st.displayed {
		return
	}
	r.unsupportedProps(n, st)
	local := identity
	if t, ok := attrOK(n, "transform"); ok {
		if m, good := parseTransform(t); good {
			local = m
		}
	}
	s = s.with(local)
	switch {
	case name == "svg":
		r.nestedSVG(n, st, s, depth)
	case name == "use":
		r.use(n, st, s, depth)
	case name == "switch":
		inner, done := r.open(s, n, st, nil)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Namespace == "svg" && passes(c) {
				r.element(c, st, inner, depth+1)
				break
			}
		}
		done()
	case containers[name]:
		inner, done := r.open(s, n, st, nil)
		r.children(n, st, inner, depth)
		done()
	case shapes[name]:
		r.shape(n, st, s)
	case name == "text":
		r.text(n, st, s)
	case name == "image":
		r.image(n, st, s)
	}
}

// unsupportedProps warns of the properties of st the renderer does not
// draw: masks that name no mask element, plus-lighter, filters and
// markers (SVG 1.1, 11.6, 14.4 and 15).
func (r *renderer) unsupportedProps(n *html.Node, st *style) {
	if st.maskBad {
		r.d.warn(warn.ContentUnsupported, warn.Approximated, "a mask that is not a reference to a mask element of the drawing is not drawn; the content is drawn unmasked")
	}
	if st.plusLighter {
		// PDF has no additive blend mode (ISO 32000-1, 11.3.5).
		r.d.warn(warn.ContentUnsupported, warn.Approximated, "mix-blend-mode plus-lighter is drawn as normal")
	}
	if st.filterBad {
		r.d.warn(warn.ContentUnsupported, warn.Approximated, "a filter that is not a reference to a filter element is not drawn; the content is drawn unfiltered")
	}
	if st.marker && (n.Data == "path" || n.Data == "line" || n.Data == "polyline" || n.Data == "polygon") {
		r.d.warn(warn.ContentUnsupported, warn.Omitted, "markers are not drawn")
	}
}

// passes evaluates the conditional processing attributes of n (SVG 1.1,
// 5.8): requiredExtensions names no extension litho supports, so any
// value fails; requiredFeatures is always true, as SVG 2 made it; and
// systemLanguage passes when it lists English, the language litho
// assumes.
func passes(n *html.Node) bool {
	if _, ok := attrOK(n, "requiredExtensions"); ok {
		return false
	}
	if v, ok := attrOK(n, "systemLanguage"); ok {
		for l := range strings.SplitSeq(v, ",") {
			l = strings.ToLower(strings.TrimSpace(l))
			if l == "en" || strings.HasPrefix(l, "en-") {
				return true
			}
		}
		return false
	}
	return true
}

// open starts the typeset group of an element styled st, for its
// opacity, its clip-path, its mask, its blend mode and its isolation, and
// returns the sink of its content and the function that closes the group.
// Without any of them it opens nothing. The bounding box, for an
// objectBoundingBox clip or mask, comes from box when given, else from
// the element's geometry.
func (r *renderer) open(s sink, n *html.Node, st *style, box *bbox) (sink, func()) {
	op := st.opacity
	var regions []clipRegion
	if st.clipPath != "" {
		var empty bool
		regions, empty = r.clipPath(n, st.clipPath, box, nil)
		if empty {
			// An empty clip path clips everything away (SVG 1.1, 14.3.5).
			op = 0
		}
	}
	fx := effects{blend: st.blend, isolate: st.isolate}
	if st.mask != "" && op > 0 {
		m, found := r.mask(n, st, st.mask, box, 0)
		switch {
		case !found:
			// CSS Masking 1, 6.1 makes a mask that names no mask element
			// a layer of transparent black, which masks everything out;
			// Chromium draws the element unmasked, and so does litho.
		case len(m.Content) == 0:
			op = 0
		default:
			fx.mask = m
		}
	}
	if op <= 0 {
		s.alpha = 0
		return s, func() {}
	}
	if fx.blend != typeset.BlendNormal {
		r.blends = true
	}
	if len(regions) > 1 {
		if inner, done, ok := r.openUnion(s, op, regions, fx); ok {
			return inner, done
		}
		regions = regions[:1]
	}
	if op >= 1 && len(regions) == 0 && !fx.any() && st.filter == "" {
		return s, func() {}
	}
	var first *typeset.Path
	var clips []*typeset.Path
	if len(regions) > 0 {
		first, clips = regions[0][0], regions[0][1:]
	}
	inner, done := r.openGroup(s, op, first, fx)
	// Clips that intersect, a clip path's own or its child's, nest.
	for _, c := range clips {
		var d2 func()
		inner, d2 = r.openGroup(inner, 1, c, effects{})
		outer := done
		done = func() { d2(); outer() }
	}
	if id := st.filter; id != "" {
		// The filter applies to the element's rendering before its clip,
		// mask and opacity (Filter Effects 1, 6; CSS Masking 1, 4): its
		// content collects apart and becomes the filter's result.
		fc := &container{}
		outer, closeOuter := inner, done
		inner = sink{c: fc, pending: identity, nest: outer.nest, alpha: outer.alpha}
		done = func() {
			outer.c.out = append(outer.c.out, r.filtered(n, st, id, box, fc.out)...)
			outer.c.last = nil
			closeOuter()
		}
	}
	return inner, done
}

// effects are the compositing of an element's group beyond its opacity
// and its clip: its blend mode, whether it is isolated, and its mask.
type effects struct {
	blend   typeset.BlendMode
	isolate bool
	mask    *typeset.SoftMask
}

// any reports effects that need a group.
func (fx effects) any() bool {
	return fx.blend != typeset.BlendNormal || fx.isolate || fx.mask != nil
}

// openUnion opens the groups of content of opacity op clipped to the
// union of regions: the content is painted once in each region, which a
// single clipping path cannot unite. Where regions overlap, translucent
// content is painted twice. It reports false, opening nothing, with a
// warning, when the groups would nest past the cap.
func (r *renderer) openUnion(s sink, op float64, regions []clipRegion, fx effects) (sink, func(), bool) {
	depth := 0
	for _, reg := range regions {
		depth = max(depth, len(reg))
	}
	if op < 1 || fx.any() {
		depth++
	}
	if s.nest+depth > maxGroupNesting {
		r.d.warn(warn.LimitReached, warn.Approximated, fmt.Sprintf("opacity and clipping nested deeper than %d levels are approximated", maxGroupNesting))
		return s, nil, false
	}
	c := &container{}
	inner := sink{c: c, pending: identity, nest: s.nest + depth, alpha: s.alpha}
	return inner, func() {
		if len(c.out) == 0 {
			return
		}
		var out []typeset.Fragment
		for _, reg := range regions {
			content := c.out
			for i, r := range slices.Backward(reg) {
				m := identity
				if i == 0 && op >= 1 && !fx.any() {
					m = s.pending
				}
				content = []typeset.Fragment{&typeset.Group{Transform: m, Clip: r, Opacity: 1, Content: content}}
			}
			out = append(out, content...)
		}
		if op < 1 || fx.any() {
			out = []typeset.Fragment{composite(&typeset.Group{Transform: s.pending, Opacity: op, Content: out}, fx)}
		}
		s.c.out = append(s.c.out, out...)
		s.c.last = nil
	}, true
}

// openGroup opens a typeset group of opacity op clipped to clip, in
// the coordinates of the sink's element, and returns the sink of its
// content and the function that closes it. Past the nesting cap it opens
// nothing: the opacity folds into the colours and the clip is dropped,
// with a warning.
func (r *renderer) openGroup(s sink, op float64, clip *typeset.Path, fx effects) (sink, func()) {
	if s.nest >= maxGroupNesting {
		r.d.warn(warn.LimitReached, warn.Approximated, fmt.Sprintf("opacity, clipping, masks and blending nested deeper than %d levels are approximated", maxGroupNesting))
		s.alpha *= op
		return s, func() {}
	}
	g := &typeset.Group{Transform: s.pending, Clip: clip, Opacity: op}
	c := &container{}
	inner := sink{c: c, pending: identity, nest: s.nest + 1, alpha: s.alpha}
	return inner, func() {
		if len(c.out) == 0 {
			return
		}
		g.Content = c.out
		s.c.out = append(s.c.out, composite(g, fx))
		s.c.last = nil
	}
}

// composite returns g under the effects fx, made one isolated
// transparency group when it is masked or isolated, or when its opacity
// is below 1 or it blends and its content paints more than one object:
// group opacity and blending apply to the group's result as a whole, so
// overlapping children do not show through each other (SVG 2, 3.2; CSS
// Color 4, 4.1; CSS Compositing 1, 3.2). One object composites alike
// under its own alpha and blend mode and costs no form XObject.
func composite(g *typeset.Group, fx effects) *typeset.Group {
	g.Blend = fx.blend
	if fx.isolate || fx.mask != nil || (g.Opacity < 1 || g.Blend != typeset.BlendNormal) && !oneObject(g.Content) {
		g.Transparency = &typeset.TransparencyGroup{Isolated: true, Mask: fx.mask}
	}
	return g
}

// oneObject reports content that paints one object: a path that fills or
// strokes, or an image, alone or in groups that move or clip it and
// composite it plainly. Constant alpha applied to it equals that alpha
// applied to a group of it. A path filled and stroked paints its stroke
// over its fill, and the glyphs of a text run can overlap: viewers
// composite each on its own.
//
// # Corpus evidence
//
//   - probe, html: poppler shows the overlap of the fill and the stroke of
//     one B operator at alpha 0.5 as (127, 63, 191), blue over red over
//     white, and two overlapping glyphs of one text object at alpha 0.5
//     as (63, 63, 255): neither as one object.
func oneObject(content []typeset.Fragment) bool {
	// TODO: one copy with oneObject in pkg/internal/layout/layout.go; typeset could hold it.
	for len(content) == 1 {
		g, ok := content[0].(*typeset.Group)
		if !ok {
			switch v := content[0].(type) {
			case *typeset.Path:
				return v.Fill == nil || v.Stroke == nil
			case *typeset.Image:
				return true
			}
			return false
		}
		if g.Transparency != nil || g.Blend != typeset.BlendNormal {
			return false
		}
		content = g.Content
	}
	return false
}

// emit adds a leaf fragment to the sink in its container's coordinates:
// moved there exactly when the transform in force allows (see
// [transformLeaf]), else under a group for that transform.
func (r *renderer) emit(s sink, f typeset.Fragment) {
	if r.frags >= maxFragments {
		r.d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("the drawing produced more than %d fragments; the rest were left out", maxFragments))
		r.full = true
		return
	}
	r.frags++
	c := s.c
	if !isIdentity(s.pending) {
		if !invertible(s.pending) {
			return
		}
		if t, ok := transformLeaf(f, s.pending); ok {
			f = t
		} else {
			if c.last != nil && c.lastM == s.pending && len(c.out) > 0 && c.out[len(c.out)-1] == typeset.Fragment(c.last) {
				c.last.Content = append(c.last.Content, f)
				return
			}
			g := &typeset.Group{Transform: s.pending, Opacity: 1, Content: []typeset.Fragment{f}}
			c.out = append(c.out, g)
			c.last, c.lastM = g, s.pending
			return
		}
	}
	c.out = append(c.out, f)
	c.last = nil
}

// transformLeaf returns f with the transform m applied to its own
// coordinates, when that draws exactly what f under a group of m would: a
// path's points and its shading's transform move under any transform, its
// stroke's width and dashes scale under a similarity (rotation, uniform
// scale, translation); an image's rectangle under a positive axis-aligned
// scale; a glyph run under a positive uniform scale without rotation; a
// group absorbs m into its own transform. It reports false otherwise.
func transformLeaf(f typeset.Fragment, m typeset.Matrix) (typeset.Fragment, bool) {
	switch v := f.(type) {
	case *typeset.Group:
		g := *v
		g.Transform = mul(m, v.Transform)
		return &g, true
	case *typeset.Path:
		p := *v
		if v.Stroke != nil {
			k, ok := similarity(m)
			if !ok {
				return nil, false
			}
			st := *v.Stroke
			st.Width *= k
			st.DashPhase *= k
			if len(st.Dash) > 0 {
				st.Dash = append([]float64(nil), st.Dash...)
				for i := range st.Dash {
					st.Dash[i] *= k
				}
			}
			p.Stroke = &st
		}
		if v.Fill != nil && v.Fill.Shading != nil {
			fl := *v.Fill
			sh := *fl.Shading
			sh.Transform = mul(m, sh.Transform)
			fl.Shading = &sh
			p.Fill = &fl
		}
		p.Ops = transformOps(v.Ops, m)
		return &p, true
	case *typeset.Image:
		if m.B != 0 || m.C != 0 || !numeric.Positive(m.A) || !numeric.Positive(m.D) {
			return nil, false
		}
		im := *v
		im.Rect = typeset.Rect{X: float64(m.A*v.Rect.X) + m.E, Y: float64(m.D*v.Rect.Y) + m.F, Width: m.A * v.Rect.Width, Height: m.D * v.Rect.Height}
		return &im, true
	case *typeset.Text:
		if m.B != 0 || m.C != 0 || m.A != m.D || !numeric.Positive(m.A) || v.Size*m.A > maxFontSize {
			return nil, false
		}
		k := m.A
		t := *v
		t.Size *= k
		t.Stroke *= k
		t.Origin = apply(m, v.Origin)
		t.Glyphs = make([]typeset.Glyph, len(v.Glyphs))
		for i, g := range v.Glyphs {
			g.X *= k
			g.Y *= k
			t.Glyphs[i] = g
		}
		return &t, true
	}
	return nil, false
}

// similarity reports whether m is a similarity, a rotation or reflection
// with a uniform scale and a translation, and its scale factor.
func similarity(m typeset.Matrix) (float64, bool) {
	const eps = 1e-9
	k := detmath.Hypot(m.A, m.B)
	if !numeric.Positive(k) {
		return 0, false
	}
	rot := math.Abs(m.A-m.D) <= eps*k && math.Abs(m.B+m.C) <= eps*k
	refl := math.Abs(m.A+m.D) <= eps*k && math.Abs(m.B-m.C) <= eps*k
	return k, rot || refl
}

// nestedSVG draws an svg element inside the drawing (SVG 1.1, 7.9): a new
// viewport at x, y of width by height, 100% by default, its viewBox mapped
// onto it, its content clipped to it unless overflow is visible.
func (r *renderer) nestedSVG(n *html.Node, st *style, s sink, depth int) {
	x := r.length(n, "x", st, axisX, 0)
	y := r.length(n, "y", st, axisY, 0)
	w := r.length(n, "width", st, axisX, r.vw)
	h := r.length(n, "height", st, axisY, r.vh)
	r.viewport(n, st, s, depth, x, y, w, h, n)
}

// viewport draws the content of the element content inside a new
// viewport at (x, y) of w by h, with the viewBox and preserveAspectRatio
// of vbOf: a nested svg, or a symbol a use instantiates.
func (r *renderer) viewport(n *html.Node, st *style, s sink, depth int, x, y, w, h float64, vbOf *html.Node) {
	if !numeric.Positive(w) || !numeric.Positive(h) {
		// A zero width or height disables rendering (SVG 1.1, 7.9).
		return
	}
	cs := st
	if vbOf != n {
		cs = r.d.computeStyle(vbOf, st)
		if !cs.displayed {
			return
		}
	}
	var clip *typeset.Path
	if !cs.overflowOK {
		clip = &typeset.Path{Ops: rectOps(x, y, w, h), Fill: &typeset.Fill{}}
	}
	inner, done := r.openClip(s, n, st, clip)
	inner = inner.with(translate(x, y))
	ovw, ovh := r.vw, r.vh
	if vb := parseViewBox(attrOf(vbOf, "viewBox")); vb.ok {
		m, ok := viewBoxTransform(vb, parseAspect(attrOf(vbOf, "preserveAspectRatio")), w, h)
		if !ok {
			done()
			return
		}
		inner = inner.with(m)
		r.vw, r.vh = vb.w, vb.h
	} else {
		r.vw, r.vh = w, h
	}
	r.children(vbOf, cs, inner, depth)
	r.vw, r.vh = ovw, ovh
	done()
}

// openClip opens the group of an element that clips to a viewport, with
// the element's own opacity and clip-path: the viewport's clip goes on an
// inner group when both are present.
func (r *renderer) openClip(s sink, n *html.Node, st *style, clip *typeset.Path) (sink, func()) {
	outer, done := r.open(s, n, st, nil)
	if clip == nil || outer.alpha <= 0 {
		return outer, done
	}
	inner, d2 := r.openGroup(outer, 1, clip, effects{})
	return inner, func() { d2(); done() }
}

// use draws the element a use references (SVG 1.1, 5.6), translated by x
// and y and styled as the use's child. A symbol, or an svg, becomes a
// viewport of the use's width and height, 100% by default. A reference
// that does not resolve, or that leads back to itself, draws nothing.
func (r *renderer) use(n *html.Node, st *style, s sink, depth int) {
	id, ok := strings.CutPrefix(strings.TrimSpace(href(n)), "#")
	if !ok {
		return
	}
	target := r.d.ids[id]
	if target == nil || r.active[target] || isAncestor(target, n) {
		return
	}
	r.uses++
	if r.uses > maxUseCount || len(r.active) >= maxUseDepth {
		r.d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("use elements past %d, or chained deeper than %d, were left out", maxUseCount, maxUseDepth))
		return
	}
	r.active[target] = true
	defer delete(r.active, target)
	x := r.length(n, "x", st, axisX, 0)
	y := r.length(n, "y", st, axisY, 0)
	inner, done := r.open(s, n, st, nil)
	defer done()
	switch target.Data {
	case "symbol":
		ts := r.d.computeStyle(target, st)
		if !ts.displayed {
			return
		}
		w := r.length(n, "width", st, axisX, r.vw)
		h := r.length(n, "height", st, axisY, r.vh)
		// A symbol's own transform is ignored: SVG 1.1, 5.5 gives it
		// none, and resvg and the SVG 1.1 test suite follow it.
		inner = inner.with(translate(x, y))
		r.symbol(target, ts, inner, depth+1, w, h)
	case "svg":
		w := r.length(n, "width", st, axisX, r.length(target, "width", st, axisX, r.vw))
		h := r.length(n, "height", st, axisY, r.length(target, "height", st, axisY, r.vh))
		inner = inner.with(translate(x, y))
		ts := r.d.computeStyle(target, st)
		if !ts.displayed {
			return
		}
		r.viewport(target, ts, inner, depth+1, r.length(target, "x", ts, axisX, 0), r.length(target, "y", ts, axisY, 0), w, h, target)
	default:
		r.element(target, st, inner.with(translate(x, y)), depth+1)
	}
}

// symbol draws the content of a symbol a use instantiates, styled st, in
// a viewport of w by h (SVG 1.1, 5.5).
func (r *renderer) symbol(sym *html.Node, st *style, s sink, depth int, w, h float64) {
	if !numeric.Positive(w) || !numeric.Positive(h) {
		return
	}
	var clip *typeset.Path
	if !st.overflowOK {
		clip = &typeset.Path{Ops: rectOps(0, 0, w, h), Fill: &typeset.Fill{}}
	}
	inner, done := r.openClip(s, sym, st, clip)
	ovw, ovh := r.vw, r.vh
	if vb := parseViewBox(attrOf(sym, "viewBox")); vb.ok {
		m, ok := viewBoxTransform(vb, parseAspect(attrOf(sym, "preserveAspectRatio")), w, h)
		if !ok {
			done()
			return
		}
		inner = inner.with(m)
		r.vw, r.vh = vb.w, vb.h
	}
	r.children(sym, st, inner, depth)
	r.vw, r.vh = ovw, ovh
	done()
}

// isAncestor reports whether a is n or one of its ancestors: a use inside
// what it references is a circular reference (SVG 1.1, 5.6).
func isAncestor(a, n *html.Node) bool {
	for p := n; p != nil; p = p.Parent {
		if p == a {
			return true
		}
	}
	return false
}
