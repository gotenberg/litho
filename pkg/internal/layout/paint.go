package layout

import (
	"cmp"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// ditem is one node of the display list, in flow coordinates: a fragment,
// or a group of them under an opacity or a clip.
type ditem struct {
	frag compose.Fragment
	// y0 and y1 are the flow extent of the fragment; page, when not
	// negative, is the one page it paints on, else it paints on every page
	// it overlaps.
	y0, y1 float64
	page   int
	group  bool
	// opacity, clip and transform are the group's, in flow coordinates;
	// transform is nil for none.
	opacity   float64
	clip      *compose.Path
	transform *compose.Matrix
	// fixed reports the group of a fixed box: its content is in the
	// coordinates of the first page and paints on every page (CSS 2.1,
	// 9.6.1).
	fixed bool
	kids  []*ditem
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
}

// pendingLink is a link region in flow coordinates.
type pendingLink struct {
	lk   *link
	rect compose.Rect
}

// painter turns the laid-out box tree into a display list in the painting
// order of CSS 2.1, Appendix E.
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
	// alpha is the opacity of the inline boxes the current text sits in,
	// folded into its colours.
	alpha float64
	// canvas is the box whose background paints the canvas.
	canvas *box
	// viewport is the box whose overflow applies to the viewport instead
	// of itself, nil for none.
	viewport *box
	// artifact reports a repeated table header or footer being painted:
	// its fragments hang from no structure element.
	artifact bool
	// shadowPixels counts the raster pixels of the blurred shadows drawn,
	// against maxShadowBudget.
	shadowPixels int
}

// paint returns the display list of the box tree rooted at root.
func (e *engine) paint(root *box) (*ditem, []pendingLink, *box) {
	p := &painter{e: e, top: &ditem{group: true, opacity: 1}, mono: -1, alpha: 1}
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
	return a.kind == kindBlock && a.parent != nil && a.st.Overflow == css.OverflowHidden && a != p.viewport
}

// hasBackground reports a style that paints a background.
func hasBackground(st *css.Style) bool {
	return st.BackgroundColor.A > 0 || st.BackgroundImage != ""
}

// cur returns the current frame.
func (p *painter) cur() *frame { return p.frames[len(p.frames)-1] }

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
		if p.clips(a) {
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
		g := &ditem{group: true, opacity: 1, clip: p.clipPath(b)}
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
func (p *painter) clipPath(b *box) *compose.Path {
	r := p.borderRect(b)
	radii := radiiOf(b, r.Width, r.Height)
	pr, pradii := insetRect(r, radii, b.bd)
	return &compose.Path{Ops: roundedRect(pr, pradii), Fill: &compose.Fill{Rule: compose.NonZero}}
}

// emit adds a fragment of box b to the display list. content reports a
// fragment of b's content, which b's own clip covers.
func (p *painter) emit(b *box, content bool, frag compose.Fragment, y0, y1 float64, page int) {
	c := p.sync(p.chain(b, content))
	if p.mono >= 0 {
		page = p.mono
	}
	if p.artifact {
		frag = cloneFragment(frag)
		switch v := frag.(type) {
		case *compose.Text:
			v.Parent = nil
		case *compose.Image:
			v.Parent = nil
		case *compose.Path:
			v.Parent = nil
		}
	}
	c.kids = append(c.kids, &ditem{frag: frag, y0: y0, y1: y1, page: page})
}

// borderRect returns b's border box with the relative offset in force.
func (p *painter) borderRect(b *box) compose.Rect {
	return compose.Rect{X: b.x + p.dx, Y: b.y + p.dy, Width: b.borderW(), Height: b.borderH()}
}

// offset returns the relative positioning offset of b (CSS 2.1, 9.4.3).
func offset(b *box) (float64, float64) {
	if b.st.Position != css.Relative || b.kind == kindText || b.kind == kindBreak || b.tpart.internal() {
		return 0, 0
	}
	cbw, cbh := 0.0, 0.0
	if b.parent != nil {
		cbw, cbh = b.parent.w, b.parent.h
	}
	dx, dy := 0.0, 0.0
	switch {
	case b.st.Left.Kind != css.Auto:
		dx = resolve(b.st.Left, cbw)
	case b.st.Right.Kind != css.Auto:
		dx = -resolve(b.st.Right, cbw)
	}
	switch {
	case b.st.Top.Kind != css.Auto:
		dy = resolve(b.st.Top, cbh)
	case b.st.Bottom.Kind != css.Auto:
		dy = -resolve(b.st.Bottom, cbh)
	}
	return dx, dy
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
	if b.fixed {
		savedMono := p.mono
		p.mono = -1
		defer func() { p.mono = savedMono }()
		push(&ditem{group: true, opacity: 1, fixed: true})
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
	if op < 1 || has {
		g := &ditem{group: true, opacity: op}
		if has {
			g.transform = &m
		}
		push(g)
	}
	if p.clips(b) && pushed == 0 {
		// The box's own clip is relative to the frame of its context: a
		// frame of its own keeps the chains short.
		push(&ditem{group: true, opacity: 1})
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
		p.context(f)
	}
	p.content(b)
	for _, k := range blocks {
		p.content(k)
	}
	for _, c := range zero {
		p.lifted(c)
	}
	for _, c := range pos {
		p.lifted(c)
	}
	// Outlines paint over everything else of the context (CSS 2.1,
	// Appendix E, step 10).
	p.outline(b)
	for _, k := range blocks {
		p.outline(k)
	}
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
			if c.isStackingContext() || depth >= maxTreeDepth {
				continue
			}
			var blocks, floats, sub []*box
			p.collect(c, &blocks, &floats, &sub)
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
		case k.layered():
			*layers = append(*layers, k)
		case k.float != css.FloatNone:
			*floats = append(*floats, k)
		case k.tpart == partTable:
			// A table paints its rows and cells itself (CSS 2.1, 17.5.1).
			*blocks = append(*blocks, k)
		default:
			*blocks = append(*blocks, k)
			if k.kind == kindBlock {
				p.collect(k, blocks, floats, layers)
			}
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
	r := p.borderRect(b)
	radii := radiiOf(b, r.Width, r.Height)
	p.shadows(b, b.st, r, radii, b.bd, false)
	p.background(b, b.st, r, radii, b.bd, b.pd)
	p.shadows(b, b.st, r, radii, b.bd, true)
	p.border(b, b.st, r, radii, b.bd, [4]bool{true, true, true, true})
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
	p.marker(b)
	for _, l := range b.lines {
		p.line(l)
	}
}

// replaced paints the picture of the replaced box b in its content box,
// sized by object-fit and placed by object-position (CSS Images 3, 5.5
// and 5.6), the part outside the content box clipped.
func (p *painter) replaced(b *box) {
	if b.hidden || b.picture == nil || b.w <= 0 || b.h <= 0 {
		return
	}
	box := compose.Rect{X: b.contentX() + p.dx, Y: b.contentY() + p.dy, Width: b.w, Height: b.h}
	r := objectRect(b.st, b.picture, box)
	img := &compose.Image{Rect: r, Source: b.picture.Source, Parent: b.elem}
	page := p.e.pageOf(box.Y)
	if r == box {
		p.emit(b, false, img, r.Y, r.Y+r.Height, page)
		return
	}
	if p.mono >= 0 {
		page = p.mono
	}
	g := &ditem{group: true, opacity: 1, clip: &compose.Path{Ops: rectOps(box), Fill: &compose.Fill{}}}
	g.kids = append(g.kids, &ditem{frag: img, y0: box.Y, y1: box.Y + box.Height, page: page})
	c := p.sync(p.chain(b, false))
	c.kids = append(c.kids, g)
}

// objectRect returns where the picture pic lands in the content box box,
// placed by the object-fit and object-position of st (CSS Images 3, 5.5 and
// 5.6): fill stretches it over the box, contain and cover scale it keeping
// its ratio, none keeps its intrinsic size, scale-down the smaller of none
// and contain.
func objectRect(st *css.Style, pic *Picture, box compose.Rect) compose.Rect {
	iw, ih := pic.Width, pic.Height
	if st.ObjectFit == css.FitFill || !(iw > 0) || !(ih > 0) {
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
	w, h := iw*k, ih*k
	return compose.Rect{
		X:     box.X + resolve(st.ObjectPosition[0], box.Width-w),
		Y:     box.Y + resolve(st.ObjectPosition[1], box.Height-h),
		Width: w, Height: h,
	}
}

// linkBox records the border box of a block-level or replaced box that
// starts a link as its link region.
func (p *painter) linkBox(b *box) {
	if b.link == nil || b.link.node != b.node || b.node == nil || b.inline || b.kind == kindInline {
		return
	}
	p.links = append(p.links, pendingLink{lk: b.link, rect: p.borderRect(b)})
}

// marker paints the outside marker of the list item b.
func (p *painter) marker(b *box) {
	m := b.outside
	if m == nil || !m.placed || b.hidden {
		return
	}
	if m.picture != nil {
		r := compose.Rect{X: m.x + p.dx, Y: m.baseline - m.picture.Height + p.dy, Width: m.picture.Width, Height: m.picture.Height}
		p.emit(b, false, &compose.Image{Rect: r, Source: m.picture.Source, Parent: m.elem}, r.Y, r.Y+r.Height, p.e.pageOf(m.baseline-m.ascent))
		return
	}
	for _, t := range m.runs {
		if t.Color.A <= 0 || t.Face == nil {
			continue
		}
		c := *t
		c.Origin = compose.Point{X: m.x + t.Origin.X + p.dx, Y: m.baseline + p.dy}
		c.Parent = m.elem
		p.emit(b, false, &c, m.baseline-m.ascent, m.baseline, p.e.pageOf(m.baseline-m.ascent+eps))
	}
}

// line paints one line box: the backgrounds and borders of its inline
// boxes, its text, its decorations and its atomic inlines, and records its
// link regions.
func (p *painter) line(l *lineBox) {
	page := p.e.pageOf(l.y + eps)
	if p.mono >= 0 {
		page = p.mono
	}
	// The decorations of an inline box paint in tree order with the text,
	// just before its first item: over the text of earlier siblings, as
	// Blink paints them. A box with no item on the line paints first.
	first := map[*box]int{}
	for i, it := range l.items {
		for k := it.box; k != nil; k = k.parent {
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
	for _, fs := range before {
		slices.SortStableFunc(fs, func(a, b *inlineFrag) int { return a.box.depth - b.box.depth })
	}
	paintFrags := func(fs []*inlineFrag) {
		for _, f := range fs {
			p.lineFrag(l, f, page)
		}
	}
	type span struct {
		rect compose.Rect
		set  bool
	}
	var linkOrder []*link
	linkRects := map[*link]*span{}
	paintFrags(before[0])
	for i, it := range l.items {
		if i > 0 {
			paintFrags(before[i])
		}
		if it.box == nil {
			continue
		}
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
			r := compose.Rect{X: l.x + it.x + p.dx, Y: l.y + it.baseline - it.ascent + p.dy, Width: it.w, Height: it.ascent + it.descent}
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
	for _, lk := range linkOrder {
		if s := linkRects[lk]; s.rect.Width > 0 && s.rect.Height > 0 {
			p.links = append(p.links, pendingLink{lk: lk, rect: s.rect})
		}
	}
}

// lineFrag paints the background and borders of the inline fragment f of
// line l.
func (p *painter) lineFrag(l *lineBox, f *inlineFrag, page int) {
	if f.box.hidden {
		return
	}
	ox, oy := inlineOffset(f.box)
	r := compose.Rect{X: l.x + f.x0 + p.dx + ox, Y: l.y + f.top + p.dy + oy, Width: f.x1 - f.x0, Height: f.bottom - f.top}
	if r.Width <= 0 && r.Height <= 0 {
		return
	}
	radii := radiiOf(f.box, r.Width, r.Height)
	sides := [4]bool{true, f.end, true, f.start}
	bd := f.box.bd
	pd := f.box.pd
	if !f.start {
		bd[css.Left], pd[css.Left] = 0, 0
		radii[css.TopLeft], radii[css.BottomLeft] = [2]float64{}, [2]float64{}
	}
	if !f.end {
		bd[css.Right], pd[css.Right] = 0, 0
		radii[css.TopRight], radii[css.BottomRight] = [2]float64{}, [2]float64{}
	}
	p.inlineFrag(f.box, r, radii, bd, pd, sides, page)
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
func union(a, b compose.Rect) compose.Rect {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.Width, b.X+b.Width), max(a.Y+a.Height, b.Y+b.Height)
	return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// inlineFrag paints the background and borders of one line fragment of an
// inline box (CSS 2.1, 8.6; box-decoration-break: slice).
func (p *painter) inlineFrag(b *box, r compose.Rect, radii [4][2]float64, bd, pd [4]float64, sides [4]bool, page int) {
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

// inlineAlpha returns the product of the opacities of the inline boxes b
// sits in up to its block container: an inline box with an opacity below 1
// folds it into the colours of its text, an approximation of the group it
// would form.
func inlineAlpha(b *box) float64 {
	a := 1.0
	for k := b.parent; k != nil && k.kind == kindInline; k = k.parent {
		a *= min(max(clamp(k.st.Opacity), 0), 1)
	}
	return a
}

// text paints the glyph run of item it of line l and its decorations
// (CSS Text Decoration 3, 2).
func (p *painter) text(it *lineItem, l *lineBox, page int) {
	alpha := inlineAlpha(it.box)
	t := it.text
	if t.Face == nil {
		return
	}
	c := *t
	c.Origin = compose.Point{X: l.x + t.Origin.X + p.dx, Y: l.y + t.Origin.Y + p.dy}
	c.Color.A *= alpha
	c.Parent = it.box.elem
	top, bottom := l.y+it.baseline-it.ascent, l.y+it.baseline+it.descent
	if c.Color.A > 0 {
		p.emit(it.box, true, &c, top, bottom, page)
	}
	size := t.Size
	for _, d := range it.box.decos {
		col, ok := colorOf(d.color)
		col.A *= alpha
		if !ok || col.A <= 0 {
			continue
		}
		thick := it.ulThick
		if thick <= 0 {
			thick = size / 14
		}
		var ys []float64
		if d.lines&css.Underline != 0 {
			pos := it.ulPos
			if it.ulThick <= 0 {
				pos = size / 10
			}
			ys = append(ys, it.baseline+pos)
		}
		if d.lines&css.Overline != 0 {
			ys = append(ys, it.baseline-it.ascent)
		}
		if d.lines&css.LineThrough != 0 {
			st, sth := it.stPos, it.stThick
			if sth <= 0 {
				st = size / 3
			}
			ys = append(ys, it.baseline-st)
		}
		for _, y := range ys {
			r := compose.Rect{X: l.x + it.x + p.dx, Y: l.y + y + p.dy, Width: it.w, Height: thick}
			if r.Width <= 0 {
				continue
			}
			p.emit(it.box, true, &compose.Path{Ops: rectOps(r), Fill: &compose.Fill{Color: col}}, l.y, l.y+l.h, page)
		}
	}
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
	baseline, ok := firstBaseline(b)
	if m.picture != nil {
		gap := e.fontSize(b.st) / 2
		m.width = m.picture.Width + gap
		m.ascent = m.picture.Height
		if !ok {
			baseline = b.contentY() + m.ascent
		}
		m.x = b.contentX() - m.width
		if rtl {
			m.x = b.contentX() + b.w + gap
		}
		m.baseline, m.placed = baseline, true
		return
	}
	if m.runs == nil {
		style := e.textStyle(b.st, b.lang)
		para, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: m.text, Style: style}}, text.ParagraphOptions{})
		if err != nil {
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
	mt := e.faceMetrics(b.st)
	m.ascent = mt.Ascent
	if !ok {
		baseline = b.contentY() + mt.Ascent + (e.lineHeight(b.st)-mt.Ascent-mt.Descent)/2
	}
	m.x = b.contentX() - m.width
	if rtl {
		m.x = b.contentX() + b.w
	}
	m.baseline, m.placed = baseline, true
}
