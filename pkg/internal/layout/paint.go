package layout

import (
	"cmp"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// ditem is one node of the display list, in flow coordinates: a fragment,
// or a group of them under an opacity or a clip. A document paints one
// leaf per fragment, tens of thousands, held until the layout returns: a
// leaf holds no more than its place in the list, and a group's effects
// live in fx, allocated only for a group that has some.
type ditem struct {
	frag typeset.Fragment
	// y0 and y1 are the flow extent of the fragment; page, when not
	// negative, is the one page it paints on, else it paints on every page
	// it overlaps.
	y0, y1 float64
	page   int
	group  bool
	kids   []*ditem
	// fx holds the group's effects, nil for a leaf and for a group that
	// draws its content as it is: opacity 1, no clip, transform, blend,
	// mask or filter, not fixed.
	fx *groupFx
}

// noEffects is what a group with no [groupFx] draws with.
var noEffects = groupFx{opacity: 1}

// effects returns the group's effects, [noEffects] for a group without
// any; the caller reads the copy and writes through [ditem.setEffects].
func (d *ditem) effects() groupFx {
	if d.fx == nil {
		return noEffects
	}
	return *d.fx
}

// setEffects returns the group's effects to write, allocating them at
// [noEffects] for a group that had none.
func (d *ditem) setEffects() *groupFx {
	if d.fx == nil {
		fx := noEffects
		d.fx = &fx
	}
	return d.fx
}

// fixedGroup reports the group of a fixed box.
func (d *ditem) fixedGroup() bool {
	return d.fx != nil && d.fx.fixed
}

// groupFx is what a display list group draws its content through. Its
// flags come last, where they share one word.
type groupFx struct {
	// opacity, clip and transform are the group's, in flow coordinates;
	// transform is nil for none.
	opacity   float64
	clip      *typeset.Path
	transform *typeset.Matrix
	// mask is the soft mask of the group, nil for none.
	mask *typeset.SoftMask
	// filter is the filter list the group's content is drawn through as
	// one image, nil for none, box its element's border box.
	filter []css.Filter
	box    typeset.Rect
	// zoom is how much the groups around a filter group enlarge its
	// content on the page, 1 for none: its image is drawn that much finer.
	zoom float64
	// blend is the group's blend mode, isolate whether its content is an
	// isolated transparency group whatever it paints.
	blend   typeset.BlendMode
	isolate bool
	// flow reports, for a filter group, that no group around it moves it,
	// so its content is in flow coordinates.
	flow bool
	// fixed reports the group of a fixed box: its content is in the
	// coordinates of the first page and paints on every page (CSS 2.1,
	// 9.6.1).
	fixed bool
}

// openClip is a clip group open in the painter, for an overflow: hidden
// box.
type openClip struct {
	box  *box
	item *ditem
}

// frame is the painter state of one stacking context.
type frame struct {
	container *ditem
	open      []openClip
	root      *box
	// columns reports the frame of a column set of root, a multi-column
	// container: root's own clip applies outside, to the columns. set is
	// that column set.
	columns bool
	set     *colSet
}

// pendingLink is a link region in flow coordinates.
type pendingLink struct {
	lk   *link
	rect typeset.Rect
}

// painter turns the laid-out box tree into a display list in the painting
// order of CSS 2.1, Appendix E. It lives for one [engine.paint] call.
type painter struct {
	e      *engine
	top    *ditem
	frames []*frame
	links  []pendingLink
	// mono is the page a monolithic box being painted lands on, -1 for
	// none.
	mono int
	// dx and dy are the relative positioning offset in force (CSS 2.1,
	// 9.4.3).
	dx, dy float64
	// canvas is the box whose background paints the canvas.
	canvas *box
	// viewport is the box whose overflow applies to the viewport instead
	// of itself, nil for none.
	viewport *box
	// artifact reports a repeated table header or footer being painted:
	// its fragments hang from no structure element.
	artifact bool
	// capture, when not nil, collects the fragments emit would add to the
	// display list instead: a blurred text shadow draws its decorations'
	// copies into its bitmap.
	capture *[]typeset.Fragment
	// cuts reports, for each item of the line being painted, inline box
	// fragments that paint before it, nil for none: a decoration line
	// spanning several runs does not cross them (decoRun).
	cuts []bool
	// effectPixels counts the pixels of the Office text effects and the
	// blurred shadows drawn, against effect.MaxPagePixels.
	effectPixels effect.Page
	// crops caches the parts of raster images border images cut out.
	crops map[cropKey]*typeset.ImageSource
	// strip is the inline box fragment being painted, laid on the strip
	// of all its fragments for its border image; nil for none.
	strip *imageStrip
	// broken holds the leaders whose word break is painted
	// ([painter.leaderBreak]).
	broken map[*box]bool
	// inLine reports a line box being painted, lineOrder the place in
	// document order of the content it paints, its atomic inlines' and
	// their lines' included (see [painter.orderOf]).
	inLine    bool
	lineOrder int
}

// paint returns the display list of the box tree rooted at root.
func (e *engine) paint(root *box) (*ditem, []pendingLink, *box) {
	p := &painter{e: e, top: &ditem{group: true}, mono: -1}
	p.frames = []*frame{{container: p.top, root: root}}
	p.canvas = canvasBox(root)
	p.viewport = viewportBox(root)
	p.context(root)
	return p.top, p.links, p.canvas
}

// canvasBox returns the box whose background paints the canvas (CSS 2.1,
// 14.2; CSS Backgrounds 3, 2.11.2): the root's, or for a root without one,
// its first child element's, which is the body of an HTML document, unless
// either has paint containment. A root that generates no box paints none.
func canvasBox(root *box) *box {
	if root.node != nil && root.st.Display == css.DisplayNone {
		return nil
	}
	if hasBackground(root.st) {
		return root
	}
	if root.st.Contain&css.ContainPaint != 0 {
		return nil
	}
	for _, k := range root.kids {
		if k.anon || k.node == nil || k.inline || k.float != css.FloatNone {
			continue
		}
		if hasBackground(k.st) && k.st.Contain&css.ContainPaint == 0 {
			return k
		}
		break
	}
	return nil
}

// viewportBox returns the box whose overflow applies to the viewport
// (CSS Overflow 3, 3.3): the root's when it is not visible, else the first
// child element's, the body of an HTML document. That box does not clip
// its own content; the page area does.
func viewportBox(root *box) *box {
	if root.st.Overflow != css.OverflowVisible {
		return root
	}
	for _, k := range root.kids {
		if k.anon || k.node == nil || k.inline || k.float != css.FloatNone || k.abs {
			continue
		}
		if k.st.Overflow != css.OverflowVisible {
			return k
		}
		break
	}
	return nil
}

// clips reports whether the block a clips its content to its padding
// box: overflow hidden, unless its overflow went to the viewport (CSS
// 2.1, 11.1.1).
func (p *painter) clips(a *box) bool {
	return a.kind == kindBlock && a.parent != nil && (a.st.Overflow == css.OverflowHidden || a.clipExact) && a != p.viewport || clipsRect(a)
}

// hasBackground reports a style that paints a background.
func hasBackground(st *css.Style) bool {
	return st.BackgroundColor.A > 0 || st.BackgroundImage != "" || st.BackgroundGradient != nil
}

// cur returns the current frame.
func (p *painter) cur() *frame { return p.frames[len(p.frames)-1] }

// inColumns reports painting inside a column set, whose display list the
// columns cut by the extent of its items.
func (p *painter) inColumns() bool {
	for _, f := range p.frames {
		if f.columns {
			return true
		}
	}
	return false
}

// chain returns the overflow: hidden boxes that clip a fragment of b in the
// current stacking context, outermost first; content reports a fragment of
// b's content, which b's own clip covers.
func (p *painter) chain(b *box, content bool) []*box {
	root := p.cur().root
	if !content && b == root {
		return nil
	}
	var out []*box
	start := b
	if !content && b != nil {
		start = b.parent
		if b.abs {
			// An overflow clip applies to the descendants whose containing
			// block is the box or lies inside it (CSS 2.1, 11.1.1).
			start = b.cbBox
		}
	}
	for a := start; a != nil; {
		if p.clips(a) && (a != root || !p.cur().columns) {
			out = append(out, a)
		}
		if a == root {
			break
		}
		if a.abs {
			a = a.cbBox
		} else {
			a = a.parent
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// sync opens and closes clip groups so the current container clips as
// chain asks, and returns that container.
func (p *painter) sync(chain []*box) *ditem {
	f := p.cur()
	k := 0
	for k < len(f.open) && k < len(chain) && f.open[k].box == chain[k] {
		k++
	}
	f.open = f.open[:k]
	for _, b := range chain[k:] {
		parent := f.container
		if len(f.open) > 0 {
			parent = f.open[len(f.open)-1].item
		}
		g := &ditem{group: true, fx: &groupFx{opacity: 1, clip: p.clipPath(b)}}
		if b.monoCol {
			// The positioned boxes a monolithic box in columns clips move
			// to its column with it, inside its clip.
			ay, oy := ancestorOffsetY(b)
			g.page, g.y0 = pageMono, b.y+ay+oy
		}
		parent.kids = append(parent.kids, g)
		f.open = append(f.open, openClip{box: b, item: g})
	}
	if len(f.open) > 0 {
		return f.open[len(f.open)-1].item
	}
	return f.container
}

// clipPath returns the padding box of b, its inner radii rounding it, the
// clip overflow: hidden applies (CSS 2.1, 11.1.1; CSS Backgrounds 3, 5.3).
// A box that is not a scroll container clips at the edge and past it by
// the margin its overflow-clip-margin names, the corners rounded by the
// edge's radii grown by that margin (CSS Overflow 4, 3.2).
func (p *painter) clipPath(b *box) *typeset.Path {
	// The clip stays where b's own relative offsets put it, whichever
	// relatively positioned descendant asks for it.
	ax, ay := ancestorOffset(b)
	ox, oy := offset(b)
	r := typeset.Rect{X: b.x + ax + ox, Y: b.y + ay + oy, Width: b.borderW(), Height: b.borderH()}
	if clipsRect(b) {
		c := clipRectOf(b, r)
		if b.st.Overflow == css.OverflowHidden {
			// One clip a box: where overflow and clip both apply, the
			// clip keeps within the border box, its corners square.
			c, _ = intersect(c, r) // an empty intersection clips everything, which the empty rectangle does
		}
		return &typeset.Path{Ops: rectOps(c), Fill: &typeset.Fill{}}
	}
	radii := radiiOf(b, r.Width, r.Height)
	cm := b.st.OverflowClipMargin
	if b.st.ScrollContainer() {
		cm = css.ClipMargin{}
	}
	pr, pradii := r, radii
	switch cm.Box {
	case css.ClipPaddingBox:
		pr, pradii = insetRect(r, radii, b.bd)
	case css.ClipContentBox:
		var d [4]float64
		for i := range d {
			d[i] = b.bd[i] + b.pd[i]
		}
		pr, pradii = insetRect(r, radii, d)
	case css.ClipBorderBox:
	}
	if m := cm.Margin; m > 0 {
		pr = typeset.Rect{X: pr.X - m, Y: pr.Y - m, Width: pr.Width + 2*m, Height: pr.Height + 2*m}
		for c := range pradii {
			if pradii[c] != [2]float64{} {
				pradii[c] = [2]float64{pradii[c][0] + m, pradii[c][1] + m}
			}
		}
	}
	return &typeset.Path{Ops: roundedRect(pr, pradii), Fill: &typeset.Fill{Rule: typeset.NonZero}}
}

// emit adds a fragment of box b to the display list. content reports a
// fragment of b's content, which b's own clip covers.
func (p *painter) emit(b *box, content bool, frag typeset.Fragment, y0, y1 float64, page int) {
	if p.capture != nil {
		*p.capture = append(*p.capture, frag)
		return
	}
	frag = ordered(frag, p.orderOf(b), 0)
	c := p.sync(p.chain(b, content))
	if p.mono >= 0 {
		page = p.mono
	}
	if l := leaderAncestor(b); l != nil && !p.artifact {
		if t, ok := frag.(*typeset.Text); ok {
			p.leaderBreak(l, t, c, y0, y1, page)
		}
	}
	if p.artifact || inLeader(b) {
		frag = cloneFragment(frag)
		switch v := frag.(type) {
		case *typeset.Text:
			v.Parent = nil
		case *typeset.Image:
			v.Parent = nil
		case *typeset.Path:
			v.Parent = nil
		case *typeset.Group:
			v.Content = adopt(v.Content, nil, 0)
		}
	}
	c.kids = append(c.kids, &ditem{frag: frag, y0: y0, y1: y1, page: page})
}

// orderOf returns the place in document order of what b paints: the
// place of the first box of the line being painted, in document order,
// for all the content of that line, so that the order of a line's
// content as bidirectional text draws it (Unicode Bidirectional
// Algorithm, L2) is kept; else b's own.
func (p *painter) orderOf(b *box) int {
	if p.inLine {
		return p.lineOrder
	}
	return b.seq
}

// ordered returns frag with the Order of its fragments that hang from a
// structure element set to seq, the place of what paints it in document
// order ([painter.orderOf]), copied where it changes: a float, a
// positioned box and the content of a stacking context paint out of
// document order (CSS 2.1, Appendix E), and the structure tree reads an
// element's content in document order, the logical order (ISO 32000-1,
// 14.8.2.3.1). depth is the group nesting walked so far, bounded by
// [MaxVectorDepth].
func ordered(frag typeset.Fragment, seq, depth int) typeset.Fragment {
	switch v := frag.(type) {
	case *typeset.Text:
		if v.Parent != nil && v.Order != seq {
			c := *v
			c.Order = seq
			return &c
		}
	case *typeset.Image:
		if v.Parent != nil && v.Order != seq {
			c := *v
			c.Order = seq
			return &c
		}
	case *typeset.Path:
		if v.Parent != nil && v.Order != seq {
			c := *v
			c.Order = seq
			return &c
		}
	case *typeset.Group:
		if depth >= MaxVectorDepth {
			return frag
		}
		var content []typeset.Fragment
		for i, k := range v.Content {
			o := ordered(k, seq, depth+1)
			if o != k && content == nil {
				content = slices.Clone(v.Content)
			}
			if content != nil {
				content[i] = o
			}
		}
		if content != nil {
			c := *v
			c.Content = content
			return &c
		}
	}
	return frag
}

// borderRect returns b's border box with the relative offset in force.
func (p *painter) borderRect(b *box) typeset.Rect {
	return typeset.Rect{X: b.x + p.dx, Y: b.y + p.dy, Width: b.borderW(), Height: b.borderH()}
}

// offset returns the relative positioning offset of b (CSS 2.1, 9.4.3),
// with that of the inline boxes it was split out of (CSS 2.1, 9.2.1.1: the
// translation of such an inline box also affects the block-level box it
// contains).
func offset(b *box) (float64, float64) {
	dx, dy := ownOffset(b)
	if b.host != nil {
		hx, hy := inlineOffset(b.host)
		dx, dy = dx+hx, dy+hy
	}
	return dx, dy
}

// ownOffset returns the relative positioning offset of b's own style. A
// row group, a row or a cell moves within its table, with the cells that
// originate in it; a column or a column group, to which position does not
// apply, does not move (CSS Position 3, 3.3).
func ownOffset(b *box) (float64, float64) {
	if b.st.Position != css.Relative || b.kind == kindText || b.kind == kindBreak || b.tpart == partColumn || b.tpart == partColGroup {
		return 0, 0
	}
	cbw, cbh := 0.0, 0.0
	cb := relContainer(b)
	if cb != nil {
		cbw, cbh = cb.w, cb.h
	}
	top, bottom := b.st.Top, b.st.Bottom
	if !b.tpart.internal() && cb != nil && contentHeight(cb) {
		if top.HasPercent() {
			top = css.Dimension{}
		}
		if bottom.HasPercent() {
			bottom = css.Dimension{}
		}
	}
	if b.tpart.internal() {
		// A table part's percentage top or bottom resolves against the
		// height its parent specifies, not the one its rows give it, and
		// behaves as auto when the parent specifies none, as Chromium
		// lays it out; CSS Position 3, 3.1 resolves it against the
		// containing block's height.
		if b.parent != nil && b.parent.st.Height.Kind == css.Points {
			cbh = b.parent.st.Height.Value
		} else {
			if top.HasPercent() {
				top = css.Dimension{}
			}
			if bottom.HasPercent() {
				bottom = css.Dimension{}
			}
		}
	}
	dx, dy := 0.0, 0.0
	switch {
	case b.st.Left.Kind != css.Auto:
		dx = resolve(b.st.Left, cbw)
	case b.st.Right.Kind != css.Auto:
		dx = -resolve(b.st.Right, cbw)
	}
	switch {
	case top.Kind != css.Auto:
		dy = resolve(top, cbh)
	case bottom.Kind != css.Auto:
		dy = -resolve(bottom, cbh)
	}
	return dx, dy
}

// relContainer returns the box whose content box the percentage insets
// of the relatively positioned box b resolve against: its nearest block
// container ancestor (CSS 2.1, 10.1), passing over inline boxes and over
// anonymous block boxes, which percentages ignore (9.2.1.1). A table
// part takes its parent.
func relContainer(b *box) *box {
	a := b.parent
	if b.tpart.internal() {
		return a
	}
	for a != nil && (a.anon || a.kind == kindInline) {
		a = a.parent
	}
	return a
}

// contentHeight reports a containing block cb whose height is not
// specified explicitly but depends on its content: an auto height on an
// in-flow block that no flex or grid container sizes and that is no
// table part. A percentage top or bottom of a relatively positioned box
// in it computes to auto (CSS 2.1, 9.3.2).
//
// # Corpus evidence
//
//   - wpt: css-position/position-relative-006 and -007 (top: -10000% and
//     calc(10px + 10%) in a block of min-height 100px) and -001 (top and
//     left 100% on an inline in a 100px block, with a block inside it)
//     draw as their references in Chromium 156.
func contentHeight(cb *box) bool {
	if cb.st.Height.Kind != css.Auto || cb.abs || cb.tpart != partNone || cb.kind != kindBlock {
		return false
	}
	if p := cb.parent; p != nil && (p.flex != nil || p.grid != nil) {
		return false
	}
	return !ratioSized(cb)
}

// ratioSized reports a box whose height its preferred aspect ratio gives.
func ratioSized(b *box) bool {
	r, _ := prefRatio(b)
	return r > 0
}

// ancestorOffset returns the relative positioning offsets b's position
// takes from its ancestors: those of its containing block chain, none for a
// fixed box on the page.
func ancestorOffset(b *box) (float64, float64) {
	dx, dy := 0.0, 0.0
	a := b.parent
	if b.abs {
		a = b.cbBox
	}
	for a != nil {
		ox, oy := offset(a)
		dx += ox
		dy += oy
		if a.abs {
			a = a.cbBox
		} else {
			a = a.parent
		}
	}
	return dx, dy
}

// floating paints the float, multi-column container or monolithic box f
// as one unit. A float inside relatively positioned inline boxes moves
// with them, as their descendant (CSS 2.1, 9.4.3). The positioned
// descendants of a float paint from the stacking context around it, which
// [painter.collect] lists them in (CSS 2.1, Appendix E, step 5).
func (p *painter) floating(f *box) {
	if f.parent != nil && f.parent.kind == kindInline {
		dx, dy := inlineOffset(f.parent)
		p.dx += dx
		p.dy += dy
		defer func() { p.dx -= dx; p.dy -= dy }()
	}
	p.unit(f, f.float == css.FloatNone || f.mc != nil)
}

// context paints b and its descendants as one unit: a stacking context, a
// float or an atomic inline (CSS 2.1, Appendix E), its positioned
// descendants in its own layers.
func (p *painter) context(b *box) { p.unit(b, true) }

// unit paints b and its descendants. own reports a box that paints the
// positioned descendants it holds: a positioned box with z-index auto does
// not, its stacking context lifts them (CSS 2.1, 9.9.1).
func (p *painter) unit(b *box, own bool) {
	if p.e.err != nil {
		return
	}
	dx, dy := offset(b)
	p.dx += dx
	p.dy += dy
	defer func() { p.dx -= dx; p.dy -= dy }()
	pushed := 0
	push := func(g *ditem) {
		c := p.sync(p.chain(b, false))
		if pushed > 0 {
			c = p.cur().container
		}
		c.kids = append(c.kids, g)
		p.frames = append(p.frames, &frame{container: g, root: b})
		pushed++
	}
	defer func() { p.frames = p.frames[:len(p.frames)-pushed] }()
	if b.monoCol {
		// A monolithic box in columns moves to its column whole; the
		// positioned boxes it holds paint from its stacking context.
		push(&ditem{group: true, page: pageMono, y0: b.y + p.dy})
		own = own && b.isStackingContext()
	}
	if b.fixed {
		savedMono := p.mono
		p.mono = -1
		defer func() { p.mono = savedMono }()
		push(&ditem{group: true, fx: &groupFx{opacity: 1, fixed: true}})
	}
	if b.pageAnchored {
		// A page-anchored box paints on its page alone, unclipped by the
		// page area: a group of one page with a transform paints up to
		// the page's edges.
		savedMono := p.mono
		p.mono = b.anchorPage
		defer func() { p.mono = savedMono }()
		id := typeset.Identity
		push(&ditem{group: true, page: b.anchorPage, fx: &groupFx{opacity: 1, transform: &id}})
	}
	m, has, visible := transformOf(b, p.borderRect(b))
	if !visible {
		return
	}
	op := 1.0
	if b.parent != nil && b.st.Opacity < 1 {
		op = min(max(clamp(b.st.Opacity), 0), 1)
		if op <= 0 {
			return
		}
	}
	blend, isolate := p.compositing(b)
	var clips []*typeset.Path
	var mask *typeset.SoftMask
	if b.parent != nil && b.isStackingContext() {
		var visible bool
		if clips, visible = p.clipPathOf(b); !visible {
			return
		}
		mask = p.maskOf(b)
	}
	if clipsRect(b) {
		c := clipRectOf(b, p.borderRect(b))
		if c.Width <= 0 || c.Height <= 0 {
			return
		}
		clips = append(clips, &typeset.Path{Ops: rectOps(c), Fill: &typeset.Fill{}})
	}
	if op < 1 || has || blend != typeset.BlendNormal || isolate || len(clips) > 0 || mask != nil {
		fx := &groupFx{opacity: op, blend: blend, isolate: isolate, mask: mask}
		if has {
			fx.transform = &m
		}
		if len(clips) > 0 {
			fx.clip = clips[0]
		}
		g := &ditem{group: true, fx: fx}
		push(g)
		for _, c := range clips[min(len(clips), 1):] {
			push(&ditem{group: true, fx: &groupFx{opacity: 1, clip: c}})
		}
	}
	if len(b.st.BackdropFilter) > 0 {
		p.backdropFilter(b)
	}
	if len(b.st.Filter) > 0 {
		// The filter applies to the element's rendering before its clip,
		// mask and opacity (Filter Effects 1, 6; CSS Masking 1, 4): the
		// innermost group.
		push(&ditem{group: true, fx: &groupFx{opacity: 1, filter: b.st.Filter, box: p.borderRect(b), flow: p.inFlow(), zoom: p.zoom()}})
	}
	if p.clips(b) && pushed == 0 {
		// The box's own clip is relative to the frame of its context: a
		// frame of its own keeps the chains short.
		push(&ditem{group: true})
	}
	if b.mc != nil {
		p.multicol(b)
		return
	}
	if b.tpart.internal() {
		p.tablePart(b)
		return
	}
	var blocks, floats, layers []*box
	p.collect(b, &blocks, &floats, &layers)
	var neg, zero, pos []*box
	if own {
		neg, zero, pos = p.stack(layers)
	}
	if b == p.canvas || b.parent == nil {
		// The root's background, or the one it takes from its first
		// child, paints the canvas instead (CSS 2.1, 14.2).
		p.borders(b)
	} else {
		p.decorations(b)
	}
	p.linkBox(b)
	for _, c := range neg {
		p.lifted(c)
	}
	for _, k := range blocks {
		p.linkBox(k)
		if k == p.canvas {
			p.borders(k)
			continue
		}
		p.decorations(k)
	}
	for _, f := range floats {
		p.floating(f)
	}
	p.content(b)
	for _, k := range blocks {
		p.content(k)
	}
	// The outlines of the context's own content paint with it, before
	// its positioned descendants, as Chromium paints them: CSS 2.1,
	// Appendix E lets an implementation draw them in the steps above step
	// 10. Each box's outline comes after those of its descendants.
	p.outlines(blocks)
	p.outline(b)
	for _, c := range zero {
		p.lifted(c)
	}
	for _, c := range pos {
		p.lifted(c)
	}
}

// inFlow reports that no open group moves its content off flow
// coordinates: none is transformed or fixed.
func (p *painter) inFlow() bool {
	for _, f := range p.frames {
		if fx := f.container.effects(); fx.transform != nil || fx.fixed {
			return false
		}
	}
	return true
}

// zoom returns how much the open groups' transforms enlarge their
// content: the product of each one's larger axis scale, within [1/64, 64].
func (p *painter) zoom() float64 {
	z := 1.0
	for _, f := range p.frames {
		if m := f.container.effects().transform; m != nil {
			z = float64(z * max(detmath.Hypot(m.A, m.B), detmath.Hypot(m.C, m.D)))
		}
	}
	if !(z > 0) {
		return 1
	}
	return min(max(z, 1.0/64), 64)
}

// stack sorts the layered descendants of a stacking context into its
// negative, zero and positive layers (CSS 2.1, Appendix E, steps 3, 8 and
// 9): a positioned box with z-index auto joins the zero layer and its own
// layered descendants join the context's.
func (p *painter) stack(layers []*box) (neg, zero, pos []*box) {
	var walk func([]*box, int)
	walk = func(ls []*box, depth int) {
		for _, c := range ls {
			switch z := c.zLevel(); {
			case c.isStackingContext() && z < 0:
				neg = append(neg, c)
			case c.isStackingContext() && z > 0:
				pos = append(pos, c)
			default:
				zero = append(zero, c)
			}
			if c.isStackingContext() || c.mc != nil || depth >= maxTreeDepth {
				continue
			}
			var blocks, floats, sub []*box
			if c.tpart.internal() {
				// The positioned boxes of a table part's cells paint with
				// their cells.
				p.collectParts(c, &sub)
			} else {
				p.collect(c, &blocks, &floats, &sub)
			}
			walk(sub, depth+1)
		}
	}
	walk(layers, 0)
	slices.SortStableFunc(neg, func(a, b *box) int { return cmp.Compare(a.zLevel(), b.zLevel()) })
	slices.SortStableFunc(pos, func(a, b *box) int { return cmp.Compare(a.zLevel(), b.zLevel()) })
	return neg, zero, pos
}

// lifted paints the layered box c from its stacking context, with the
// relative offsets its ancestors give it.
func (p *painter) lifted(c *box) {
	sdx, sdy := p.dx, p.dy
	p.dx, p.dy = ancestorOffset(c)
	p.unit(c, c.isStackingContext())
	p.dx, p.dy = sdx, sdy
}

// outlines paints the outlines of blocks, the in-flow blocks inside a box
// listed in tree order: each after those of its descendants, siblings in
// tree order. The box itself paints its own after them.
//
// CSS 2.1, Appendix E, step 10, and CSS UI 4, 5 leave the order of
// outlines open. Chromium paints a box's descendants' outlines before its
// own, on screen and in print, so where outlines overlap the ancestor's
// shows.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws a 4px black outline over the 4px
//     blue outline of a child of the same width, in a block and in a
//     multi-column container, the spanner's included.
//   - wpt: css-multicol/multicol-span-all-004, -007, -008, -009, -011 and
//     multicol-span-all-list-item-001 and -002 draw the container's
//     outline under its spanners' in test and reference alike when the
//     order is reversed, so they pass either way.
func (p *painter) outlines(blocks []*box) {
	if slices.ContainsFunc(blocks, func(k *box) bool { return k.tpart == partTable }) {
		var all []*box
		for _, k := range blocks {
			all = append(all, k)
			if k.tpart == partTable {
				all = tableOutlines(k, all, 0)
			}
		}
		blocks = all
	}
	if !slices.ContainsFunc(blocks, hasOutline) {
		return
	}
	var open []*box
	for _, k := range blocks {
		for len(open) > 0 && !insideOf(k, open[len(open)-1]) {
			p.outline(open[len(open)-1])
			open = open[:len(open)-1]
		}
		open = append(open, k)
	}
	for i := len(open) - 1; i >= 0; i-- {
		p.outline(open[i])
	}
}

// tableOutlines appends to out, in tree order, the row groups, rows and
// cells of the table part b that draw an outline and are not positioned,
// which paint their own: the outline applies to every element (CSS UI 4,
// 5.1), a table's parts included, as Chromium draws them.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws a 10px outline round a row group
//     and round a row of a table of separated borders, and a 6px one
//     round a cell, a relatively positioned cell and a row of a
//     collapsed table.
func tableOutlines(b *box, out []*box, depth int) []*box {
	if depth > maxTreeDepth {
		return out
	}
	for _, k := range b.kids {
		if !k.tpart.internal() || k.layered() || !k.laidOut {
			continue
		}
		if hasOutline(k) {
			out = append(out, k)
		}
		if k.tpart < partCell {
			out = tableOutlines(k, out, depth+1)
		}
	}
	return out
}

// hasOutline reports a box whose style asks for an outline.
func hasOutline(b *box) bool {
	o := b.st.Outline
	return o.Width > 0 && o.Style != css.BorderNone && o.Style != css.BorderHidden
}

// insideOf reports that k is a descendant of a.
func insideOf(k, a *box) bool {
	for c := k.parent; c != nil; c = c.parent {
		if c == a {
			return true
		}
	}
	return false
}

// collect lists, in tree order, the in-flow block-level descendants of b,
// the floats and the layered boxes among them, without entering floats,
// atomic inlines or layered boxes, which paint as units.
func (p *painter) collect(b *box, blocks, floats, layers *[]*box) {
	for _, k := range b.kids {
		switch {
		case !k.laidOut && k.kind != kindText && k.kind != kindInline && k.kind != kindBreak:
		case k.kind == kindInline:
			p.collect(k, blocks, floats, layers)
		case k.inline:
			// An atomic inline paints with its line.
		case k.rubyAnn:
			// A ruby annotation paints after the line of its column.
		case k.isItem() && !k.layered():
			// A flex or grid item paints as an inline block does, with
			// its container's content (CSS Flexbox 1, 4.3; CSS Grid 1,
			// 6.3).
		case k.layered():
			*layers = append(*layers, k)
		case k.float != css.FloatNone && k.mc == nil:
			// A float paints as a unit, but its positioned descendants
			// take part in the enclosing stacking context (CSS 2.1,
			// Appendix E, step 5).
			*floats = append(*floats, k)
			var bl, fl []*box
			p.collect(k, &bl, &fl, layers)
		case k.float != css.FloatNone || k.mc != nil:
			// A multi-column container paints its columns as a unit.
			*floats = append(*floats, k)
		case k.monoCol:
			// A monolithic box in columns paints as a unit in its column;
			// its positioned descendants stay in the stacking context's
			// layers, and follow the columns on their own.
			*floats = append(*floats, k)
			var bl, fl []*box
			p.collect(k, &bl, &fl, layers)
		case k.tpart == partTable:
			// A table paints its rows and cells itself (CSS 2.1, 17.5.1),
			// but for the positioned parts among them.
			*blocks = append(*blocks, k)
			p.collectParts(k, layers)
		case k.tpart.internal():
			p.collectParts(k, layers)
		default:
			*blocks = append(*blocks, k)
			if k.kind == kindBlock {
				p.collect(k, blocks, floats, layers)
			}
		}
	}
}

// collectParts lists the positioned row groups, rows and cells of the
// table part b, in tree order, without entering them or the cells.
func (p *painter) collectParts(b *box, layers *[]*box) {
	for _, k := range b.kids {
		switch {
		case !k.tpart.internal():
		case k.layered():
			*layers = append(*layers, k)
		case k.tpart < partCell:
			p.collectParts(k, layers)
		}
	}
}

// decorations paints the background and borders of b.
func (p *painter) decorations(b *box) {
	switch {
	case b.tpart == partTable:
		p.tableDecorations(b)
		return
	case b.tpart.internal():
		return
	}
	if b.hidden || b.kind == kindInline || b.kind == kindText {
		return
	}
	frags, columns := p.fragments(b)
	for _, r := range frags {
		paint := func() {
			radii := radiiOf(b, r.Width, r.Height)
			p.shadows(b, b.st, r, radii, b.bd, false)
			p.background(b, b.st, r, radii, b.bd, b.pd)
			p.shadows(b, b.st, r, radii, b.bd, true)
			p.border(b, b.st, r, radii, b.bd, [4]bool{true, true, true, true})
		}
		if columns {
			p.paintWhole(b, paint)
		} else {
			paint()
		}
	}
}

// borders paints the shadows and borders of b, whose background paints
// the canvas instead.
func (p *painter) borders(b *box) {
	if b.hidden || b.kind == kindInline || b.kind == kindText {
		return
	}
	r := p.borderRect(b)
	radii := radiiOf(b, r.Width, r.Height)
	p.shadows(b, b.st, r, radii, b.bd, false)
	p.shadows(b, b.st, r, radii, b.bd, true)
	p.border(b, b.st, r, radii, b.bd, [4]bool{true, true, true, true})
}

// content paints the inline content of b, its replaced content and its
// outside marker (CSS 2.1, Appendix E, step 7).
func (p *painter) content(b *box) {
	if b.tpart == partTable {
		p.tableContent(b)
		return
	}
	if b.kind == kindReplaced {
		p.replaced(b)
		return
	}
	if b.flex != nil || b.grid != nil {
		// Flex and grid items paint like inline blocks, in
		// order-modified document order, the order of the kids (CSS
		// Flexbox 1, 4.3; CSS Grid 1, 6.3).
		for _, k := range b.kids {
			if k.isItem() && k.laidOut && !k.layered() {
				p.context(k)
			}
		}
		return
	}
	p.marker(b)
	for i, l := range b.lines {
		var fl *firstLine
		if i == 0 {
			fl = p.e.firstLines[b]
		}
		p.line(l, fl)
	}
	p.chainContent(b)
}

// replaced paints the picture of the replaced box b in its content box,
// sized by object-fit and placed by object-position (CSS Images 3, 5.5
// and 5.6), the part outside the content box clipped.
func (p *painter) replaced(b *box) {
	if b.symbol && !b.hidden && b.w > 0 {
		r := typeset.Rect{X: b.contentX() + p.dx, Y: b.contentY() + p.dy, Width: b.w, Height: b.w}
		if path := symbolPath(b.st, r); path != nil {
			path.Parent = b.elem
			p.emit(b, false, ordered(path, p.orderOf(b), 0), r.Y, r.Y+r.Height, p.e.monoPage(r.Y, r.Height))
		}
		return
	}
	if b.hidden || b.picture == nil || b.w <= 0 || b.h <= 0 {
		return
	}
	box := typeset.Rect{X: b.contentX() + p.dx, Y: b.contentY() + p.dy, Width: b.w, Height: b.h}
	r := objectRect(b.st, objectPicture(b.st, b.picture, b.w, b.h), box)
	img := p.e.pictureFrag(b.picture, r, b.elem)
	if img == nil {
		return
	}
	img = ordered(img, p.orderOf(b), 0)
	page := p.e.monoPage(box.Y, box.Height)
	if r == box {
		p.emit(b, false, img, r.Y, r.Y+r.Height, page)
		return
	}
	if p.mono >= 0 {
		page = p.mono
	}
	g := &ditem{group: true, fx: &groupFx{opacity: 1, clip: &typeset.Path{Ops: rectOps(box), Fill: &typeset.Fill{}}}}
	g.kids = append(g.kids, &ditem{frag: img, y0: box.Y, y1: box.Y + box.Height, page: page})
	c := p.sync(p.chain(b, false))
	c.kids = append(c.kids, g)
}

// objectRect returns where the picture pic lands in the content box box,
// placed by the object-fit and object-position of st (CSS Images 3, 5.5 and
// 5.6): fill stretches it over the box, contain and cover scale it keeping
// its ratio, none keeps its intrinsic size, scale-down the smaller of none
// and contain.
func objectRect(st *css.Style, pic *Picture, box typeset.Rect) typeset.Rect {
	iw, ih := pic.Width, pic.Height
	if st.ObjectFit == css.FitFill || !numeric.Positive(iw) || !numeric.Positive(ih) {
		return box
	}
	contain := min(box.Width/iw, box.Height/ih)
	k := 1.0
	switch st.ObjectFit {
	case css.FitContain:
		k = contain
	case css.FitCover:
		k = max(box.Width/iw, box.Height/ih)
	case css.FitScaleDown:
		k = min(contain, 1)
	case css.FitNone, css.FitFill:
	}
	w, h := float64(iw*k), float64(ih*k)
	return typeset.Rect{
		X:     box.X + resolve(st.ObjectPosition[0], box.Width-w),
		Y:     box.Y + resolve(st.ObjectPosition[1], box.Height-h),
		Width: w, Height: h,
	}
}

// monoPage returns the page a monolithic piece of height h at flow
// position y paints on: the page it lands on, or -1 for every page it
// overlaps when it is taller than a page and overflows (CSS Fragmentation
// 3, 4.1).
func (e *engine) monoPage(y, h float64) int {
	p := e.pageOf(y + eps)
	if h > e.areaH(p)+eps {
		return -1
	}
	return p
}

// linkBox records the border box of a block-level or replaced box that
// starts a link as its link region.
func (p *painter) linkBox(b *box) {
	if b.link == nil || b.link.node != b.node || b.node == nil || b.inline || b.kind == kindInline {
		return
	}
	p.links = append(p.links, pendingLink{lk: b.link, rect: p.borderRect(b)})
}

// marker paints the outside marker of the list item b. The marker box is
// a child of the item (CSS Lists 3, 3.1) though it sits outside the
// item's border box, so the item's overflow clip cuts it like the rest of
// its content (CSS Overflow 3, 3): an outside marker of an item of
// overflow hidden or clip does not show, as Chromium draws it.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws no outside disc or decimal marker
//     of a list item of overflow hidden or clip, with or without columns;
//     it draws the inside marker of one.
//   - wpt: css-multicol/multicol-span-all-list-item-002 passes with the
//     marker of its overflow: hidden item drawn in test and reference
//     alike.
func (p *painter) marker(b *box) {
	m := b.outside
	if m == nil || !m.placed || b.hidden {
		return
	}
	if m.picture != nil {
		r := typeset.Rect{X: m.x + p.dx, Y: m.baseline - m.picture.Height + p.dy, Width: m.picture.Width, Height: m.picture.Height}
		if f := p.e.pictureFrag(m.picture, r, m.elem); f != nil {
			p.emit(b, true, f, r.Y, r.Y+r.Height, p.e.pageOf(m.baseline-m.ascent))
		}
		return
	}
	if m.symbol {
		p.symbolMarker(b, m)
		return
	}
	for _, t := range m.runs {
		if t.Color.A <= 0 || t.Face == nil {
			continue
		}
		c := *t
		c.Origin = typeset.Point{X: m.x + t.Origin.X + p.dx, Y: m.baseline + p.dy}
		c.Parent = m.elem
		p.emit(b, true, &c, m.baseline-m.ascent, m.baseline, p.e.pageOf(m.baseline-m.ascent+eps))
	}
}

// symbolMarker paints the symbol marker m of the list item b in the
// marker's colour: a disc fills its square's inscribed circle, a circle
// strokes it one pixel wide inside the square, a square fills the square,
// as Blink paints them.
func (p *painter) symbolMarker(b *box, m *markerBox) {
	mst := m.st
	if mst == nil {
		mst = b.st
	}
	r := m.sym
	r.X += p.dx
	r.Y += p.dy
	path := symbolPath(mst, r)
	if path == nil {
		return
	}
	path.Parent = m.elem
	p.emit(b, true, path, r.Y, r.Y+r.Height, p.e.pageOf(m.sym.Y+eps))
}

// symbolPath returns the bullet of the symbol marker of style st in the
// square r, in the marker's colour: a disc fills its square's inscribed
// circle, a circle strokes it one pixel wide inside the square, a square
// fills the square, as Blink paints them; nil when the colour paints
// nothing.
func symbolPath(st *css.Style, r typeset.Rect) *typeset.Path {
	col, ok := colorOf(st.Color)
	if !ok {
		return nil
	}
	switch st.ListStyleType { //nolint:exhaustive // a symbol marker is a disc, a circle or a square
	case css.Square:
		return &typeset.Path{Ops: rectOps(r), Fill: &typeset.Fill{Color: col}}
	case css.Circle:
		sw := min(css.Pt, r.Width/2)
		return &typeset.Path{Ops: circleOps(r.X+float64(r.Width/2), r.Y+float64(r.Height/2), (r.Width-sw)/2), Stroke: &typeset.Stroke{Color: col, Width: sw}}
	}
	return &typeset.Path{Ops: circleOps(r.X+float64(r.Width/2), r.Y+float64(r.Height/2), r.Width/2), Fill: &typeset.Fill{Color: col}}
}

// line paints one line box: the backgrounds and borders of its inline
// boxes, its text, its decorations and its atomic inlines, and records its
// link regions. fl is the first formatted line when l is one, whose
// atomic inlines sit in the copies of their parents.
func (p *painter) line(l *lineBox, fl *firstLine) {
	if !p.inLine {
		order := 0
		for _, it := range l.items {
			if s := it.box.seq; s > 0 && (order == 0 || s < order) {
				order = s
			}
		}
		p.inLine, p.lineOrder = true, order
		defer func() { p.inLine, p.lineOrder = false, 0 }()
	}
	page := p.e.pageOf(l.y + eps)
	if l.overflows {
		page = -1
	}
	if p.mono >= 0 {
		page = p.mono
	}
	// The decorations of an inline box paint in tree order with the text,
	// just before its first item: over the text of earlier siblings, as
	// Blink paints them. A box with no item on the line paints first.
	first := map[*box]int{}
	parent := func(k *box) *box {
		if fl != nil {
			if q, ok := fl.parent[k]; ok {
				return q
			}
		}
		return k.parent
	}
	for i, it := range l.items {
		for k := it.box; k != nil; k = parent(k) {
			if _, ok := first[k]; !ok {
				first[k] = i
			}
			if k.kind != kindInline && k != it.box {
				break
			}
		}
	}
	before := make([][]*inlineFrag, max(len(l.items), 1))
	for _, f := range l.frags {
		at := first[f.box] // 0 for a box with no item on the line
		before[at] = append(before[at], f)
	}
	var cuts []bool
	for i, fs := range before {
		slices.SortStableFunc(fs, func(a, b *inlineFrag) int { return a.box.depth - b.box.depth })
		if i > 0 && slices.ContainsFunc(fs, fragPaints) {
			if cuts == nil {
				cuts = make([]bool, len(before))
			}
			cuts[i] = true
		}
	}
	saved := p.cuts
	p.cuts = cuts
	defer func() { p.cuts = saved }()
	var paintFrags func(fs []*inlineFrag)
	type span struct {
		rect typeset.Rect
		set  bool
	}
	var linkOrder []*link
	linkRects := map[*link]*span{}
	p.barRules(l, page)
	p.changeBar(l, page)
	var groups []*box
	defer func() { p.enterInlines(&groups, nil) }()
	paintFrags = func(fs []*inlineFrag) {
		for _, f := range fs {
			p.enterInlines(&groups, compositedInlines(f.box))
			p.lineFrag(l, f, page)
		}
	}
	paintFrags(before[0])
	for i, it := range l.items {
		if i > 0 {
			paintFrags(before[i])
		}
		if it.box == nil {
			continue
		}
		p.enterInlines(&groups, compositedInlines(it.box))
		ox, oy := inlineOffset(it.box.parent)
		p.dx += ox
		p.dy += oy
		if it.atomic != nil {
			saved := p.mono
			p.mono = page
			p.context(it.atomic)
			p.mono = saved
		} else if it.text != nil && !it.box.hidden {
			p.text(it, l, page)
		}
		p.dx -= ox
		p.dy -= oy
		if lk := it.box.link; lk != nil && lk.node != nil && isInlineLink(lk, it.box) {
			r := typeset.Rect{X: l.x + it.x + p.dx, Y: l.y + it.baseline - it.ascent + p.dy, Width: it.w, Height: it.ascent + it.descent}
			if it.atomic != nil {
				r = p.borderRect(it.atomic)
			}
			s, ok := linkRects[lk]
			if !ok {
				s = &span{}
				linkRects[lk] = s
				linkOrder = append(linkOrder, lk)
			}
			if !s.set {
				s.rect, s.set = r, true
			} else {
				s.rect = union(s.rect, r)
			}
		}
	}
	p.annotations(l, page)
	for _, lk := range linkOrder {
		if s := linkRects[lk]; s.rect.Width > 0 && s.rect.Height > 0 {
			p.links = append(p.links, pendingLink{lk: lk, rect: s.rect})
		}
	}
}

// compositedInlines returns the inline boxes b sits in, b included, up to
// its block container, that composite their content as a group: an
// opacity below 1 or a blend mode (CSS Color 4, 4.1; CSS Compositing 1,
// 3.2), outermost first. Each fragment of such a box on a line is one
// group: what Chromium composites once per box, litho composites once per
// line, which differs only where its lines overlap.
func compositedInlines(b *box) []*box {
	var out []*box
	for k := b; k != nil; k = k.parent {
		if k.kind != kindInline {
			if k != b {
				break
			}
			continue
		}
		if inlineGroup(k) {
			out = append(out, k)
		}
	}
	slices.Reverse(out)
	return out
}

// inlineGroup reports an inline box that composites as a group.
func inlineGroup(b *box) bool {
	return b.kind == kindInline && (b.st.Opacity < 1 || b.st.MixBlendMode != css.BlendNormal)
}

// innerComposited returns the innermost inline box b sits in, b included,
// that composites as a group, nil for none.
func innerComposited(b *box) *box {
	if c := compositedInlines(b); len(c) > 0 {
		return c[len(c)-1]
	}
	return nil
}

// enterInlines closes the groups of open that want does not hold and opens
// those of want that are not open, so what paints next paints in the
// groups of want, outermost first. Each group is a frame whose root is its
// inline box: the clips outside it apply to the group.
func (p *painter) enterInlines(open *[]*box, want []*box) {
	k := 0
	for k < len(*open) && k < len(want) && (*open)[k] == want[k] {
		k++
	}
	p.frames = p.frames[:len(p.frames)-(len(*open)-k)]
	*open = (*open)[:k]
	for _, b := range want[k:] {
		c := p.sync(p.chain(b, false))
		op := min(max(clamp(b.st.Opacity), 0), 1)
		blend, isolate := p.blendOf(b), blendsInside(b)
		g := &ditem{group: true, fx: &groupFx{opacity: op, blend: blend, isolate: isolate}}
		c.kids = append(c.kids, g)
		p.frames = append(p.frames, &frame{container: g, root: b})
		*open = append(*open, b)
	}
}

// lineFrag paints the background and borders of the inline fragment f of
// line l.
func (p *painter) lineFrag(l *lineBox, f *inlineFrag, page int) {
	if f.box.hidden {
		return
	}
	ox, oy := inlineOffset(f.box)
	r := typeset.Rect{X: l.x + f.x0 + p.dx + ox, Y: l.y + f.top + p.dy + oy, Width: f.x1 - f.x0, Height: f.bottom - f.top}
	if r.Width <= 0 && r.Height <= 0 {
		return
	}
	if p.dy+oy != 0 && p.inColumns() {
		// The relative offset moves the fragment after the columns cut
		// the flow (CSS Fragmentation 3, 5.5): it paints in the column of
		// its line, where the line's text paints, however far it moves.
		g := &ditem{group: true, page: pageMono, y0: l.y + float64((f.top+f.bottom)/2)}
		c := p.sync(p.chain(f.box, false))
		c.kids = append(c.kids, g)
		p.frames = append(p.frames, &frame{container: g, root: f.box})
		defer func() { p.frames = p.frames[:len(p.frames)-1] }()
	}
	radii := radiiOf(f.box, r.Width, r.Height)
	sides := [4]bool{true, f.right, true, f.left}
	bd := f.box.bd
	pd := f.box.pd
	if !f.left {
		bd[css.Left], pd[css.Left] = 0, 0
		radii[css.TopLeft], radii[css.BottomLeft] = [2]float64{}, [2]float64{}
	}
	if !f.right {
		bd[css.Right], pd[css.Right] = 0, 0
		radii[css.TopRight], radii[css.BottomRight] = [2]float64{}, [2]float64{}
	}
	if f.box.st.BorderImage.HasImage() && (!f.left || !f.right) {
		// A border image spans the inline box's fragments as one strip.
		p.strip = p.imageStrip(f, r)
		defer func() { p.strip = nil }()
	}
	p.inlineFrag(f.box, r, radii, bd, pd, sides, page)
}

// fragPaints reports an inline box fragment that paints a background, a
// border or a shadow.
func fragPaints(f *inlineFrag) bool {
	st := f.box.st
	return hasBackground(st) || f.box.bd != [4]float64{} || len(st.BoxShadow) > 0 || st.BorderImage.HasImage()
}

// inlineOffset returns the relative positioning offsets of the inline box
// b and of the inline boxes it sits in (CSS 2.1, 9.4.3), which move its
// fragments and its content.
func inlineOffset(b *box) (float64, float64) {
	dx, dy := 0.0, 0.0
	for a := b; a != nil && a.kind == kindInline; a = a.parent {
		ox, oy := offset(a)
		dx += ox
		dy += oy
	}
	return dx, dy
}

// isInlineLink reports whether the link lk, which b sits in, starts at an
// inline-level box, so its regions follow the line fragments.
func isInlineLink(lk *link, b *box) bool {
	for a := b; a != nil; a = a.parent {
		if a.node == lk.node {
			return a.inline
		}
	}
	return false
}

// union returns the smallest rectangle holding a and b.
func union(a, b typeset.Rect) typeset.Rect {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.Width, b.X+b.Width), max(a.Y+a.Height, b.Y+b.Height)
	return typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// compositing returns the blend mode b composites its stacking context
// with and whether its content must be an isolated group: a stacking
// context is one when something inside it blends (CSS Compositing 1, 3.2
// and 3.3), as Chromium prints it, and need not be one otherwise, since
// isolation changes nothing that composites in normal mode. The root
// composites with the page alone, which ISO 32000-1, 11.4.7 already
// isolates.
//
// # Corpus evidence
//
//   - probe, html: Chromium writes a box of mix-blend-mode multiply as an
//     isolated transparency group painted under /BM /Multiply, its opacity
//     in the same graphics state; a stacking context made by isolation,
//     z-index or a transform holding it as one more isolated group; and a
//     box of isolation isolate holding nothing that blends as no group.
//   - probe, html: Chromium writes plus-lighter as an isolated group under
//     /BM /Normal, and parses no plus-darker.
func (p *painter) compositing(b *box) (typeset.BlendMode, bool) {
	if b.parent == nil || !b.isStackingContext() {
		return typeset.BlendNormal, false
	}
	return p.blendOf(b), blendsInside(b)
}

// blendOf returns the blend mode of b, a stacking context or an inline
// box that composites as a group.
func (p *painter) blendOf(b *box) typeset.BlendMode {
	m := b.st.MixBlendMode
	if m == css.BlendPlusLighter {
		// PDF has no additive blend mode (ISO 32000-1, 11.3.5).
		p.e.warnOnce(warn.CSSUnsupported, warn.Approximated, "mix-blend-mode plus-lighter is printed as normal, as Chromium prints it", -1)
		return typeset.BlendNormal
	}
	// css.BlendMode orders the sixteen modes as typeset does, after ISO
	// 32000-1, Tables 136 and 137.
	return typeset.BlendMode(m)
}

// blendsInside reports whether a box inside b blends with b's content: a
// stacking context or an inline box of a blend mode, which b's stacking
// context or b's inline group holds. The stacking contexts and the inline
// groups inside b are groups of their own, whose content blends within
// them.
func blendsInside(b *box) bool {
	for _, k := range b.kids {
		group := k.isStackingContext() || inlineGroup(k)
		if m := k.st.MixBlendMode; group && m != css.BlendNormal && m != css.BlendPlusLighter {
			return true
		}
		if !group && blendsInside(k) {
			return true
		}
	}
	return false
}

// inlineFrag paints the background and borders of one line fragment of an
// inline box (CSS 2.1, 8.6; box-decoration-break: slice).
func (p *painter) inlineFrag(b *box, r typeset.Rect, radii [4][2]float64, bd, pd [4]float64, sides [4]bool, page int) {
	saved := p.mono
	if p.mono < 0 {
		p.mono = page
	}
	p.shadows(b, b.st, r, radii, bd, false)
	p.background(b, b.st, r, radii, bd, pd)
	p.shadows(b, b.st, r, radii, bd, true)
	p.border(b, b.st, r, radii, bd, sides)
	p.mono = saved
}

// text paints the glyph run of item it of line l and its decorations
// (CSS Text Decoration 3, 2): underlines and overlines under the glyphs,
// line-throughs over them (CSS Text Decoration 3, 2.4 and CSS 2.1,
// appendix E).
func (p *painter) text(it *lineItem, l *lineBox, page int) {
	t := it.text
	if t.Face == nil {
		return
	}
	p.textEffects(it, l, page)
	p.textShadows(it, l, page)
	p.textDecorations(it, l, page, css.Underline|css.Overline, nil)
	c := *t
	c.Origin = typeset.Point{X: l.x + t.Origin.X + p.dx, Y: l.y + t.Origin.Y + p.dy}
	c.Parent = it.box.elem
	top, bottom := l.y+it.baseline-it.ascent, l.y+it.baseline+it.descent
	if !p.textPaint(it, l, page, &c, top, bottom) && c.Color.A > 0 {
		p.emit(it.box, true, &c, top, bottom, page)
	}
	p.emphasisMarks(it, l, page)
	p.textDecorations(it, l, page, css.LineThrough, nil)
}

// roundPx rounds v points to whole CSS pixels.
func roundPx(v float64) float64 { return float64(math.Round(v/css.Pt) * css.Pt) }

// decoShadow is a text shadow drawing the decorations of a glyph run: a
// copy of each moved by x and y, in color (CSS Text Decoration 3, 4: the
// shadow applies to the text and its decorations).
type decoShadow struct {
	x, y  float64
	color typeset.Color
}

// textDecorations paints the decoration lines of item it among which, or
// their shadow sh when not nil.
//
// With [Options.BrowserRules] each decoration takes its thickness from its
// decorating box, and an underline its position too, whatever the size
// and vertical alignment of the text inside, and the runs of one
// decorating box that touch draw one line (CSS Text Decoration 3, 2.5:
// one thickness and position on each line for the decorations of one
// decorating box). Chromium departs from that clause for overlines and
// line-throughs, which follow it: it draws them against each glyph run's
// own ascent, an overline grown up from one pixel below it, a line-through
// as far below it as the decorating box's strikeout lies below its own
// ascent. Off, as the Office converters leave it, each glyph run draws its
// own decorations at its own size.
//
// With [Options.BrowserRules] an underline lies as Blink places it for a
// face without an underline position, whatever the face's: half its
// unrounded thickness below the baseline, rounded up to whole pixels, at
// least one.
//
// # Corpus evidence
//
//   - probe, html: Chromium 9.0.0 draws the underline of every face tried,
//     Liberation Serif and Sans, DejaVu Sans and Ahem from 13 to 64px, at
//     that gap whatever the face's underline position.
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) draws a block's
//     underline as one line at the block's baseline across superscripts,
//     subscripts and larger text, each overline and line-through at the
//     glyph run's own height, all at the block's thickness; an Ahem
//     overline 1 to 30px thick at 16, 20, 40 and 50px ends one pixel
//     below the glyphs' top edge.
func (p *painter) textDecorations(it *lineItem, l *lineBox, page int, which css.TextDecoration, sh *decoShadow) {
	for _, d := range it.box.decos {
		lines := d.lines & which
		if lines == 0 {
			continue
		}
		g, ok := p.decoGeom(it, l, d, sh)
		if !ok {
			continue
		}
		dl := decoLine{it: it, l: l, page: page, style: d.style, color: g.color, thick: g.thick, x0: p.decoStart(it, l, d), x: it.x, w: it.w}
		if sh != nil {
			dl.dx, dl.dy = sh.x, sh.y
		}
		for _, line := range [...]css.TextDecoration{css.Underline, css.Overline, css.LineThrough} {
			if lines&line == 0 {
				continue
			}
			k := decoIndex(line)
			runs := []*lineItem{it}
			if sh == nil && p.blink() {
				// One line spans the runs of the decorating box that
				// touch: the first run draws an underline or overline under
				// the glyphs of all, the last a line-through over them.
				from, to, ok := p.decoRun(it, l, d, k, line == css.LineThrough)
				if !ok {
					continue
				}
				runs = l.items[from : to+1]
				lo, hi := runs[0].x, runs[0].x+runs[0].w
				for _, r := range runs {
					lo, hi = min(lo, r.x), max(hi, r.x+r.w)
				}
				dl.x, dl.w = lo, hi-lo
			}
			dl.y, dl.under = g.y[k], line == css.Underline
			dl.skip = nil
			if line != css.LineThrough {
				// Only underlines and overlines skip ink (CSS Text
				// Decoration 4, 2.10.5).
				dl.skip = runs
			}
			p.decoLine(dl)
		}
	}
}

// decoIndex returns the index of the decoration line among the positions
// of a [decoGeom].
func decoIndex(line css.TextDecoration) int {
	switch line {
	case css.Overline:
		return 1
	case css.LineThrough:
		return 2
	case css.Underline:
	}
	return 0
}

// decoGeom is where decoration d draws on a glyph run: its colour, its
// thickness and the tops of its underline, overline and line-through
// below the line top.
type decoGeom struct {
	color typeset.Color
	thick float64
	y     [3]float64
}

// decoGeom returns where decoration d draws on item it of line l, false
// when it draws nothing.
func (p *painter) decoGeom(it *lineItem, l *lineBox, d deco, sh *decoShadow) (decoGeom, bool) {
	var g decoGeom
	col, ok := colorOf(d.color)
	if sh != nil {
		col, ok = sh.color, true
	}
	if !ok || col.A <= 0 {
		return g, false
	}
	size, k := it.text.Size, 1.0
	if own := min(max(clamp(it.box.st.FontSize), 0), maxFontSize); p.blink() && d.origin != nil && d.size != own && own > 0 {
		k = d.size / own
		size *= k
	}
	// auto is a tenth of the font size, at least one pixel; from-font is
	// the face's underline thickness (CSS Text Decoration 4, 2.4).
	thick := d.thick
	if thick == fromFont {
		thick = autoThickness
		if it.ulThick > 0 {
			thick = max(roundPx(it.ulThick*k), css.Pt)
		}
	}
	auto := thick == autoThickness
	if auto {
		thick = max(roundPx(size/10), css.Pt)
		if p.blink() {
			// Blink floors the tenth of the font size.
			thick = max(math.Floor(size/10/css.Pt)*css.Pt, css.Pt)
		}
	}
	if thick <= 0 {
		return g, false
	}
	g.color, g.thick = col, thick
	// The face's underline position, else a tenth of the font size,
	// rounded to the pixel grid as browsers draw it.
	pos := float64(size / 10)
	if it.ulThick > 0 {
		pos = float64(it.ulPos * k)
	}
	pos = roundPx(pos)
	switch {
	case d.under&css.UnderUnder != 0:
		// Under the decorating box's descent, a pixel below it as
		// Chromium draws it, the offset counted from there (CSS Text
		// Decoration 4, 2.7 and 2.8.1).
		pos = float64(it.descent * k)
		if d.origin != nil {
			pos = p.e.faceMetrics(d.origin.st, d.origin.lang).Descent
		}
		if p.blink() {
			pos += css.Pt
		}
		if d.hasOffset {
			pos += d.offset
		}
	case d.under&css.UnderFromFont != 0:
		// The face's underline position, the offset counted from there.
		if d.hasOffset {
			pos += d.offset
		}
	default:
		if p.blink() {
			// Blink's gap for a face without an underline position,
			// whatever the face's.
			t := thick / css.Pt
			if auto {
				t = size / 10 / css.Pt
			}
			pos = float64(max(math.Ceil(float64(t/2)-1e-9), 1) * css.Pt)
		}
		if d.hasOffset {
			pos = d.offset
		}
	}
	g.y[0] = it.baseline + pos
	if p.blink() {
		g.y[0] = decoBaseline(it, l, d) + pos
	}
	top := it.baseline - it.ascent
	g.y[1] = top
	if p.blink() {
		g.y[1] = top + css.Pt - thick
	}
	// Centred on the face's strikeout stroke, else a third of the font
	// size above the baseline, at the decorating box's size.
	mid := float64(size / 3)
	if it.stThick > 0 {
		ft := max(roundPx(it.stThick*k), css.Pt)
		mid = roundPx(it.stPos*k) - float64(ft/2)
	}
	g.y[2] = top + float64(it.ascent*k) - mid - float64(thick/2)
	return g, true
}

// decoRun returns the first and last indices in l's items of the runs
// that draw decoration line k of d with item it: it and the runs beside
// it that d decorates at the same place, in the same colour and moved by
// the same relative offset. It reports false when another run of them
// draws the line: the first for an underline or overline, the last for a
// line-through (last).
func (p *painter) decoRun(it *lineItem, l *lineBox, d deco, k int, last bool) (int, int, bool) {
	at := slices.Index(l.items, it)
	if at < 0 {
		return 0, -1, false
	}
	g, _ := p.decoGeom(it, l, d, nil)
	ox, oy := inlineOffset(it.box.parent)
	joins := func(a, b *lineItem) bool {
		if b.text == nil || b.text.Face == nil || b.box == nil || b.box.hidden {
			return false
		}
		if math.Abs(a.x+a.w-b.x) > metricSlack && math.Abs(b.x+b.w-a.x) > metricSlack {
			return false
		}
		bx, by := inlineOffset(b.box.parent)
		if bx != ox || by != oy || innerComposited(b.box) != innerComposited(it.box) {
			return false
		}
		for _, bd := range b.box.decos {
			if bd != d {
				continue
			}
			bg, ok := p.decoGeom(b, l, bd, nil)
			return ok && bg.color == g.color && bg.thick == g.thick && bg.y[k] == g.y[k]
		}
		return false
	}
	cut := func(i int) bool { return i < len(p.cuts) && p.cuts[i] }
	from, to := at, at
	for from > 0 && !cut(from) && joins(l.items[from], l.items[from-1]) {
		if !last {
			return 0, -1, false
		}
		from--
	}
	for to+1 < len(l.items) && !cut(to+1) && joins(l.items[to], l.items[to+1]) {
		if last {
			return 0, -1, false
		}
		to++
	}
	return from, to, true
}

// decoBaseline returns the baseline, below the line top, of the decorating
// box of d on line l: the line's for a block container, the inline box's
// own for an inline box, the baseline of item it when the box is not on
// the line.
func decoBaseline(it *lineItem, l *lineBox, d deco) float64 {
	o := d.origin
	if o == nil {
		return it.baseline
	}
	if o.kind != kindInline {
		return l.baseline
	}
	for _, f := range l.frags {
		if f.box == o || o.node != nil && f.box.node == o.node {
			return f.base
		}
	}
	return it.baseline
}

// decoStart returns the left edge, relative to the line box, of the first
// glyph run of line l that d decorates: a wavy line keeps its phase from
// there across the runs of other boxes.
func (p *painter) decoStart(it *lineItem, l *lineBox, d deco) float64 {
	x := it.x
	if d.origin == nil || !p.blink() {
		return x
	}
	for _, o := range l.items {
		if o.text == nil || o.x >= x {
			continue
		}
		for _, od := range o.box.decos {
			if od.origin == d.origin {
				x = o.x
				break
			}
		}
	}
	return x
}

// decoLine is one decoration line of item it of line l to paint: its top
// at y below the line top, thick points thick, in style and color, moved
// by dx and dy for a shadow, from x to x+w relative to the line box. under
// reports an underline; x0 is where a wavy line's phase starts, relative
// to the line box.
type decoLine struct {
	it         *lineItem
	l          *lineBox
	page       int
	style      css.DecorationStyle
	color      typeset.Color
	y, thick   float64
	x, w       float64
	dx, dy, x0 float64
	under      bool
	// skip are the glyph runs whose ink the line skips, nil for none.
	skip []*lineItem
}

// decoLine paints one decoration line (CSS Text Decoration 3, 2.3): a
// double line adds a second one a thickness below (above for an overline
// or line-through, away from the glyphs for an underline), dotted and
// dashed lines are dashes of one and three thicknesses, a wavy line
// zigzags over twice the thickness.
func (p *painter) decoLine(dl decoLine) {
	it, l, thick, col := dl.it, dl.l, dl.thick, dl.color
	x0 := l.x + dl.x + p.dx + dl.dx
	w := dl.w
	if w <= 0 {
		return
	}
	top := l.y + dl.y + p.dy + dl.dy
	// band is the vertical extent of what the line draws, where it skips
	// the glyphs it crosses.
	b0, b1 := top, top+thick
	switch dl.style {
	case css.DecorationDouble:
		if dl.under {
			b1 = top + float64(3*thick)
		} else {
			b0 = top - 2*thick
		}
	case css.DecorationWavy:
		b0, b1 = top-thick, top+2*thick
	case css.DecorationSolid, css.DecorationDotted, css.DecorationDashed:
	}
	var keep [][2]float64
	if len(dl.skip) > 0 {
		s0, s1 := b0, b1
		if !dl.under && p.blink() {
			// Chromium tests the glyphs against the overline one pixel
			// above where it paints it.
			s0, s1 = s0-css.Pt, s1-css.Pt
		}
		if gaps := p.inkGaps(dl.skip, l, p.dx+dl.dx, p.dy+dl.dy, s0, s1); len(gaps) > 0 {
			keep = inkPieces(x0, x0+w, gaps)
			if len(keep) == 0 {
				return
			}
		}
	}
	emit := func(f typeset.Fragment) {
		p.emit(it.box, true, f, l.y+dl.dy, l.y+l.h+dl.dy, dl.page)
	}
	// rect fills the part of the line between y and y+h that skips no
	// glyph.
	rect := func(y, h float64) {
		if keep == nil {
			emit(&typeset.Path{Ops: rectOps(typeset.Rect{X: x0, Y: y, Width: w, Height: h}), Fill: &typeset.Fill{Color: col}})
			return
		}
		var ops []typeset.PathOp
		for _, k := range keep {
			ops = append(ops, rectOps(typeset.Rect{X: k[0], Y: y, Width: k[1] - k[0], Height: h})...)
		}
		emit(&typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: col}})
	}
	// stroke strokes ops, clipped to the parts of the band that skip no
	// glyph.
	stroke := func(ops []typeset.PathOp, s *typeset.Stroke) {
		path := &typeset.Path{Ops: ops, Stroke: s}
		if keep == nil {
			emit(path)
			return
		}
		var clip []typeset.PathOp
		for _, k := range keep {
			clip = append(clip, rectOps(typeset.Rect{X: k[0], Y: b0 - thick, Width: k[1] - k[0], Height: b1 - b0 + 2*thick})...)
		}
		emit(&typeset.Group{Clip: &typeset.Path{Ops: clip}, Content: []typeset.Fragment{path}})
	}
	switch dl.style {
	case css.DecorationDouble:
		second := top + 2*thick
		if !dl.under {
			second = top - 2*thick
		}
		rect(top, thick)
		rect(second, thick)
	case css.DecorationDotted, css.DecorationDashed:
		dash := []float64{thick, thick}
		if dl.style == css.DecorationDashed {
			dash = []float64{3 * thick, 3 * thick}
		}
		mid := top + float64(thick/2)
		stroke([]typeset.PathOp{
			{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x0, Y: mid}}},
			{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x0 + w, Y: mid}}},
		}, &typeset.Stroke{Color: col, Width: thick, Dash: dash})
	case css.DecorationWavy:
		stroke(waveOps(l.x+dl.x0+p.dx+dl.dx, x0, x0+w, top+float64(thick/2), thick), &typeset.Stroke{Color: col, Width: thick})
	case css.DecorationSolid:
		rect(top, thick)
	}
}

// waveOps returns the zigzag of a wavy line from x0 to x1 about mid, its
// vertices thick above and below mid every two thicknesses from origin,
// where it starts on mid: the runs of one decorating box drawn from one
// origin join into one wave. The last vertex moves back to x1.
func waveOps(origin, x0, x1, mid, thick float64) []typeset.PathOp {
	step := 2 * thick
	at := func(k int) (float64, float64) {
		switch {
		case k == 0:
			return origin, mid
		case k%2 == 1:
			return origin + float64(float64(k)*step), mid - thick
		}
		return origin + float64(float64(k)*step), mid + thick
	}
	// y returns the wave's height at x, between vertices k and k+1.
	y := func(x float64, k int) float64 {
		ax, ay := at(k)
		bx, by := at(k + 1)
		return ay + (by-ay)*(x-ax)/(bx-ax)
	}
	first := 0
	if x0 > origin {
		first = min(int((x0-origin)/step), maxWaveSegments) // fits int: capped
	}
	ops := []typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x0, Y: y(x0, first)}}}}
	for k, n := first+1, 0; n < maxWaveSegments; k, n = k+1, n+1 {
		vx, vy := at(k)
		ops = append(ops, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{{X: min(vx, x1), Y: vy}}})
		if vx >= x1 {
			break
		}
	}
	return ops
}

// maxWaveSegments bounds the segments of one wavy decoration line. Anti-DoS
// guard, not spec-mandated: a line item is at most a page wide, and 10,000
// segments draw a wave of one-point thickness 20,000 points long.
const maxWaveSegments = 10_000

// markerExtent returns the height the outside marker of the list item b
// takes at the top of its content when b has no line box to put it
// beside: the line it sets ([engine.markerLine]); 0 when b has a first
// line or no outside marker. b's content is at least that tall,
// which keeps its margins from collapsing through it. CSS Lists 3, 3.1
// leaves the place of a marker without a line to the user agent; this is
// Chromium's.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) draws the square of an
//     empty list item in Ahem 20px/40px centred in a 40px line at the
//     item's top and makes the item 40px tall; an item holding only a 5px
//     block is 40px tall too, the block at its top; one of list-style:
//     none stays 0px tall. A 15px image marker of an empty item stands on
//     the baseline of the line: at Ahem 40px/80px its bottom is 52px
//     below the item's top, and the item is 80px tall.
func (e *engine) markerExtent(b *box) float64 {
	if b.outside == nil {
		return 0
	}
	if _, ok := markerBaseline(b); ok {
		return 0
	}
	above, below := e.markerLine(b)
	return above + below
}

// markerLine returns the heights above and below its baseline of the line
// the outside marker of b sets when b has no line: the strut of the
// marker's style, its half-leading split as a line's is, grown above to
// hold a picture standing on the baseline.
func (e *engine) markerLine(b *box) (float64, float64) {
	mst := b.outside.st
	if mst == nil {
		mst = b.st
	}
	mt := e.faceMetrics(mst, b.lang)
	lh := e.lineHeight(mst, b.lang)
	up, _ := e.leading(lh - mt.Ascent - mt.Descent)
	above := mt.Ascent + up
	below := lh - above
	if p := b.outside.picture; p != nil {
		above = max(above, p.Height)
	}
	return above, below
}

// placeMarker lays out the outside marker of the list item b next to its
// first line box (CSS 2.1, 12.5): its end edge on the item's content edge,
// its baseline on the first line's.
func (e *engine) placeMarker(b *box) {
	m := b.outside
	if m == nil {
		return
	}
	rtl := b.st.Direction == css.RTL
	baseline, ok := markerBaseline(b)
	if m.picture != nil {
		gap := float64(e.fontSize(b.st) / 2)
		m.width = m.picture.Width + gap
		m.ascent = m.picture.Height
		if !ok {
			above, _ := e.markerLine(b)
			baseline = b.contentY() + above
		}
		left, right := e.markerEdges(b)
		m.x = left - m.width
		if rtl {
			m.x = right + gap
		}
		if m.boxW > 0 {
			m.x = markerX(b, m, m.picture.Width)
		}
		m.baseline, m.placed = baseline, true
		return
	}
	mst := m.st
	if mst == nil {
		mst = b.st
	}
	if m.runs == nil {
		style := e.textStyle(mst, b.lang)
		// The marker text runs in the item's direction: a right-to-left
		// item's suffix and its space come left of the number, between it
		// and the content, as Chromium draws them.
		dir := text.LeftToRight
		if rtl {
			dir = text.RightToLeft
		}
		para, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: m.text, Style: style}}, text.ParagraphOptions{Direction: dir})
		if err != nil {
			e.shapeFailed(err, "list marker", b.contentY())
			return
		}
		pl := para.Line(0, para.Len(), false)
		m.width = pl.Width
		for _, sp := range pl.Spans {
			if sp.Text != nil {
				m.runs = append(m.runs, sp.Text)
			}
		}
	}
	mt := e.faceMetrics(mst, b.lang)
	m.ascent = mt.Ascent
	if !ok {
		above, _ := e.markerLine(b)
		baseline = b.contentY() + above
	}
	if m.symbol {
		left, right := e.markerEdges(b)
		m.sym = symbolRect(b, mt.Ascent, baseline, left, right)
		m.x, m.baseline, m.placed = m.sym.X, baseline, true
		return
	}
	left, right := e.markerEdges(b)
	m.x = left - m.width
	if rtl {
		m.x = right
	}
	if m.boxW > 0 {
		m.x = markerX(b, m, m.width)
	}
	m.baseline, m.placed = baseline, true
}

// ancestorOffsetY returns the vertical relative offsets of b's ancestors
// and of b itself.
func ancestorOffsetY(b *box) (float64, float64) {
	_, ay := ancestorOffset(b)
	_, oy := offset(b)
	return ay, oy
}
