package layout

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// maxPageSide is the largest page side [Layout] accepts, in points: the
// composition model's bound (ISO 32000-1, Annex C.2, Table C.1).
const maxPageSide = 14400

// layout is [Layout].
func layout(ctx context.Context, root *Node, fonts *text.Fonts, opts Options) (*Result, error) {
	if root == nil || fonts == nil {
		return nil, fmt.Errorf("%w: nil root or font index", ErrInvalidOptions)
	}
	if err := checkPage(opts.Page); err != nil {
		return nil, err
	}
	first := opts.Page
	switch {
	case opts.PageFor != nil:
		first = opts.PageFor(PageInfo{})
	case opts.PageAt != nil:
		first = opts.PageAt(0)
	}
	if err := checkPage(first); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("laying out: %w", err)
	}
	areaW, areaH := areaOf(first)
	e := newEngine(ctx, fonts, opts, areaW, areaH)
	e.geom.init(e, first)
	e.rtlRoot = root.Style != nil && root.Style.Direction == css.RTL
	rb := e.generate(root)
	if e.err != nil {
		return nil, e.err
	}
	if name := pageStart(rb); name != "" {
		// The first page's type is the root's start page value (CSS Page
		// 3, 4.1): its box, and the flow's width, follow it.
		e.mark(0, name, false, nil)
		if opts.PageFor != nil {
			first = opts.PageFor(e.infoOf(0))
			if err := checkPage(first); err != nil {
				return nil, err
			}
			e.pageW, e.pageH = areaOf(first)
			e.geom = geometry{}
			e.geom.init(e, first)
		}
	}
	e.rootBox = rb
	e.layoutRoot(rb)
	e.layoutPositioned(rb)
	if e.err != nil {
		return nil, e.err
	}
	disp, links, canvas := e.paint(rb)
	if e.err != nil {
		return nil, e.err
	}
	res := &Result{Structure: e.root, ContentWidth: e.overflowRight(rb)}
	used := map[*compose.StructElem]bool{}
	res.Pages = e.assemble(disp, canvas, rb, used)
	if len(res.Pages) > e.maxPages {
		return nil, fmt.Errorf("%w: %d pages, at most %d", ErrTooManyPages, len(res.Pages), e.maxPages)
	}
	res.Anchors = e.anchors(rb, len(res.Pages))
	res.Extents = e.extents(rb, len(res.Pages))
	e.links(res, links, used)
	prune(e.root, used)
	for _, w := range e.warnings {
		page := 0
		if w.y >= 0 {
			page = min(e.pageOf(w.y)+1, len(res.Pages))
		}
		res.Warnings = append(res.Warnings, compose.Warning{Code: w.code, Message: w.msg, Page: page})
		if opts.Logger != nil {
			opts.Logger.Debug("layout: "+w.msg, slog.String("code", w.code), slog.Int("page", page))
		}
	}
	return res, nil
}

// newEngine returns the state of one layout of a page area of w by h
// points.
func newEngine(ctx context.Context, fonts *text.Fonts, opts Options, w, h float64) *engine {
	maxPages := opts.MaxPages
	if maxPages <= 0 {
		maxPages = DefaultMaxPages
	}
	return &engine{
		ctx: ctx, fonts: fonts, opts: opts, pageW: w, pageH: h, maxPages: maxPages,
		warned: map[string]bool{}, images: map[string]*Picture{},
		metrics: map[*css.Style]text.Metrics{}, spaces: map[*css.Style]float64{}, missing: map[rune]bool{},
		root: &compose.StructElem{Role: "Document", Lang: opts.Lang},
	}
}

// pageCount returns the pages the flow of the root box rb and the display
// list d need. A fragment counts for the part of it its clips leave: the
// content an overflow: hidden box cuts off adds no page, while content
// overflowing its box below the root's bottom, a monolithic piece taller
// than a page included, takes the pages it reaches unless the viewport
// clips it. The root's bottom
// margin takes no page of its own: a margin at the end of the flow
// truncates (CSS Fragmentation 3, 5.2).
func (e *engine) pageCount(rb *box, d *ditem) int {
	bottom := rb.y + rb.borderH()
	n := e.pageOf(max(bottom-1e-3, 0)) + 1
	// A viewport whose overflow is not visible clips each page to its page
	// area: content overflowing a page takes no page of its own there (CSS
	// Overflow 3, 3.3).
	clipped := viewportBox(rb) != nil
	var walk func(it *ditem, lo, hi float64)
	walk = func(it *ditem, lo, hi float64) {
		for _, k := range it.kids {
			if k.group {
				klo, khi := lo, hi
				if k.clip != nil {
					if y0, y1, ok := pathSpan(k.clip.Ops); ok {
						klo, khi = max(lo, y0), min(hi, y1)
					}
				}
				if khi > klo && !k.fixed {
					walk(k, klo, khi)
				}
				continue
			}
			y0 := max(k.y0, lo)
			if !(y0 < min(k.y1, hi) || k.y1 <= k.y0 && k.y0 >= lo && k.y0 < hi) {
				continue
			}
			if k.page >= 0 {
				n = max(n, k.page+1)
				continue
			}
			if clipped {
				n = max(n, e.pageOf(max(y0, 0))+1)
				continue
			}
			n = max(n, e.pageOf(max(min(k.y1, hi)-1e-3, y0, 0))+1)
		}
	}
	walk(d, math.Inf(-1), math.Inf(1))
	n = max(n, e.positionedPages(rb, clipped))
	return max(min(n, e.maxPages+1), 1)
}

// positionedPages returns the pages the border boxes of the absolutely
// positioned boxes under rb reach, painted or not. An absolutely positioned
// box adds its border box to the scrollable overflow of its containing
// block, and so of the root (CSS Overflow 3, 2.2), which the pages show
// whole, as Chromium prints it: a tall positioned box with nothing to paint
// still takes its pages. A fixed box is on every page already, and the boxes
// a clipping or transformed box holds are left to the display list, which
// knows where they paint. With clipped, a viewport that clips each page, a
// box takes only the page its top is on.
func (e *engine) positionedPages(rb *box, clipped bool) int {
	n := 0
	var walk func(b *box)
	walk = func(b *box) {
		for _, k := range b.kids {
			if k.fixed || k.transformed() {
				continue
			}
			if k.abs && k.laidOut {
				end := k.y + k.borderH() - 1e-3
				if clipped {
					end = k.y
				}
				n = max(n, e.pageOf(max(end, k.y, 0))+1)
			}
			if k.kind == kindBlock && k.st.Overflow != css.OverflowVisible {
				continue
			}
			walk(k)
		}
	}
	walk(rb)
	return n
}

// pathSpan returns the vertical extent of the points of ops, false for no
// point.
func pathSpan(ops []compose.PathOp) (float64, float64, bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, op := range ops {
		n := 1
		switch op.Kind {
		case compose.CurveTo:
			n = 3
		case compose.ClosePath:
			n = 0
		case compose.MoveTo, compose.LineTo:
		}
		for _, q := range op.P[:n] {
			lo, hi = min(lo, q.Y), max(hi, q.Y)
		}
	}
	return lo, hi, lo <= hi
}

// assemble slices the display list into pages: each page paints the canvas,
// then the part of the flow its page area shows, translated to the page and
// clipped to the page area. used collects the structure elements the
// fragments hang from.
func (e *engine) assemble(d *ditem, canvas, rb *box, used map[*compose.StructElem]bool) []*compose.Page {
	n := e.pageCount(rb, d)
	pages := make([]*compose.Page, n)
	e.pageFrags = make([]int, n)
	free := map[compose.Fragment]bool{}
	perPage := e.distribute(d, 0, n, used, free)
	fitH := 0.0
	if e.opts.FitFirstPage {
		fitH = e.fittedHeight(rb, d)
	}
	for k := range n {
		pb := e.assembledBox(k, fitH)
		pg := &compose.Page{Width: pb.Width + 2*pb.Bleed, Height: pb.Height + 2*pb.Bleed}
		e.paintPageBackground(pg, pb)
		e.paintMarks(pg, pb)
		sheet := len(pg.Content)
		e.paintCanvas(pg, canvas, pb)
		if e.opts.MarginContent != nil {
			pg.Content = append(pg.Content, e.opts.MarginContent(e.infoOf(k), n)...)
		}
		top := e.pageTop(k)
		frags := perPage[k]
		for i := 0; i < len(frags); {
			// A run of fragments is clipped to the page area, the slice of
			// the flow the page shows; a transformed box that lands on one
			// page alone, and a line or a replaced box, may paint past it,
			// up to the page's edges.
			j := i + 1
			for j < len(frags) && free[frags[j]] == free[frags[i]] {
				j++
			}
			clip := compose.Rect{X: -pb.Margin[css.Left], Y: top, Width: pb.Width, Height: e.areaH(k)}
			if free[frags[i]] {
				clip = compose.Rect{X: -pb.Margin[css.Left], Y: top - pb.Margin[css.Top], Width: pb.Width, Height: pb.Height}
			}
			pg.Content = append(pg.Content, &compose.Group{
				Transform: compose.Matrix{A: 1, D: 1, E: pb.Margin[css.Left], F: pb.Margin[css.Top] - top},
				Clip:      &compose.Path{Ops: rectOps(clip), Fill: &compose.Fill{}},
				Opacity:   1,
				Content:   frags[i:j],
			})
			i = j
		}
		trim(pg, pb, sheet)
		pages[k] = pg
	}
	return pages
}

// distribute returns the fragments of the container it per page, groups
// rebuilt on each page they reach, depth the groups already open. free
// collects the top-level groups of transformed boxes that land on one page
// and the top-level fragments placed on one page.
func (e *engine) distribute(it *ditem, depth, n int, used map[*compose.StructElem]bool, free map[compose.Fragment]bool) map[int][]compose.Fragment {
	out := map[int][]compose.Fragment{}
	for _, k := range it.kids {
		if k.group && k.fixed {
			e.repeat(k, depth, n, used, out)
			continue
		}
		if k.group {
			sub := e.distribute(k, depth+1, n, used, free)
			for _, p := range slices.Sorted(mapKeys(sub)) {
				frags := sub[p]
				if depth+1 >= maxGroupDepth {
					e.warnOnce(codeGroupDepth, fmt.Sprintf("opacity and clipping nested deeper than %d levels painted without the inner groups", maxGroupDepth), -1)
					out[p] = append(out[p], frags...)
					continue
				}
				if e.admit(p) {
					g := &compose.Group{Transform: groupTransform(k), Clip: k.clip, Opacity: k.opacity, Content: frags}
					if depth == 0 && k.transform != nil && len(sub) == 1 {
						free[g] = true
					}
					out[p] = append(out[p], g)
				}
			}
			continue
		}
		p0, p1 := k.page, k.page
		if k.page < 0 {
			p0 = e.pageOf(max(k.y0, 0))
			p1 = e.pageOf(max(k.y1-1e-3, k.y0, 0))
		}
		p0, p1 = max(p0, 0), min(p1, n-1)
		for p := p0; p <= p1; p++ {
			if !e.admitN(p, fragNodes(k.frag)) {
				continue
			}
			f := e.fitLeaf(k.frag, depth)
			if f == nil {
				continue
			}
			if p1 > p0 {
				f = cloneFragment(f)
			}
			markTree(f, used)
			if depth == 0 && k.page >= 0 {
				// A line or a replaced box sits on one page whole (CSS
				// Fragmentation 3, 4.4), so its ink overflow, an
				// underline or a glyph below the line box, paints into
				// the page margin with it, as browsers print it.
				free[f] = true
			}
			out[p] = append(out[p], f)
		}
	}
	return out
}

// groupTransform returns the transform of the group it.
func groupTransform(it *ditem) compose.Matrix {
	if it.transform != nil {
		return *it.transform
	}
	return compose.Identity
}

// repeat adds the content of the fixed group it, laid out on the first
// page, to every one of the n pages of out, moved to each page's place in
// the flow (CSS 2.1, 9.6.1). The copies after the first are artifacts.
func (e *engine) repeat(it *ditem, depth, n int, used map[*compose.StructElem]bool, out map[int][]compose.Fragment) {
	if depth+1 >= maxGroupDepth {
		e.warnOnce(codeGroupDepth, fmt.Sprintf("opacity and clipping nested deeper than %d levels painted without the inner groups", maxGroupDepth), -1)
		return
	}
	leaves := countLeaves(it)
	for p := range n {
		if e.pageFrags[p]+leaves > maxPageFragments {
			e.warnOnce(codeTooManyFragments, fmt.Sprintf("a page of more than %d fragments was cut short", maxPageFragments), e.pageTop(p))
			continue
		}
		e.pageFrags[p] += leaves
		frags := e.flatten(it, depth+1, p > 0)
		if p == 0 {
			for _, f := range frags {
				markTree(f, used)
			}
		}
		out[p] = append(out[p], &compose.Group{
			Transform: compose.Matrix{A: 1, D: 1, F: e.pageTop(p)},
			Opacity:   1,
			Content:   frags,
		})
	}
}

// countLeaves returns the fragments of the group it, its subgroups'
// included.
func countLeaves(it *ditem) int {
	n := 0
	for _, k := range it.kids {
		if k.group {
			n += countLeaves(k)
		} else {
			n += fragNodes(k.frag)
		}
	}
	return n
}

// flatten returns the fragments of the group it whatever their page, its
// subgroups rebuilt; with artifact set, copies that hang from no structure
// element.
func (e *engine) flatten(it *ditem, depth int, artifact bool) []compose.Fragment {
	var out []compose.Fragment
	for _, k := range it.kids {
		if k.group {
			if depth+1 >= maxGroupDepth {
				out = append(out, e.flatten(k, depth+1, artifact)...)
				continue
			}
			out = append(out, &compose.Group{Transform: groupTransform(k), Clip: k.clip, Opacity: k.opacity, Content: e.flatten(k, depth+1, artifact)})
			continue
		}
		f := e.fitLeaf(k.frag, depth)
		if f == nil {
			continue
		}
		if artifact {
			f = cloneFragment(f)
			switch v := f.(type) {
			case *compose.Text:
				v.Parent = nil
			case *compose.Image:
				v.Parent = nil
			case *compose.Path:
				v.Parent = nil
			case *compose.Group:
				v.Content = adopt(v.Content, nil, 0)
			}
		}
		out = append(out, f)
	}
	return out
}

// markTree records the structure elements the fragments of f hang from.
func markTree(f compose.Fragment, used map[*compose.StructElem]bool) {
	if g, ok := f.(*compose.Group); ok {
		for _, k := range g.Content {
			markTree(k, used)
		}
		return
	}
	markUsed(f, used)
}

// admit counts one more fragment on page p and reports whether it fits
// [maxPageFragments].
func (e *engine) admit(p int) bool { return e.admitN(p, 1) }

// admitN counts n more fragments on page p and reports whether they fit
// [maxPageFragments].
func (e *engine) admitN(p, n int) bool {
	if e.pageFrags[p]+n > maxPageFragments {
		e.warnOnce(codeTooManyFragments, fmt.Sprintf("a page of more than %d fragments was cut short", maxPageFragments), e.pageTop(p))
		return false
	}
	e.pageFrags[p] += n
	return true
}

// mapKeys returns the keys of m, for sorting.
func mapKeys(m map[int][]compose.Fragment) func(func(int) bool) {
	return func(yield func(int) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// cloneFragment returns a copy of a fragment painted on several pages.
func cloneFragment(f compose.Fragment) compose.Fragment {
	switch v := f.(type) {
	case *compose.Path:
		c := *v
		return &c
	case *compose.Image:
		c := *v
		return &c
	case *compose.Text:
		c := *v
		return &c
	case *compose.Group:
		c := *v
		return &c
	}
	return f
}

// markUsed records the structure element f hangs from.
func markUsed(f compose.Fragment, used map[*compose.StructElem]bool) {
	switch v := f.(type) {
	case *compose.Text:
		if v.Parent != nil {
			used[v.Parent] = true
		}
	case *compose.Image:
		if v.Parent != nil {
			used[v.Parent] = true
		}
	case *compose.Path:
		if v.Parent != nil {
			used[v.Parent] = true
		}
	}
}

// paintCanvas paints the canvas background of the page pg, whose page box
// is pb, from the box canvas (CSS 2.1, 14.2) over the page box's border
// box, the page area, not its margins (CSS Page 3, 3.1).
func (e *engine) paintCanvas(pg *compose.Page, canvas *box, pb PageBox) {
	if canvas == nil {
		return
	}
	st := canvas.st
	w, h := areaOf(pb)
	area := compose.Rect{X: pb.Margin[css.Left], Y: pb.Margin[css.Top], Width: w, Height: h}
	if c, ok := colorOf(st.BackgroundColor); ok {
		pg.Content = append(pg.Content, &compose.Path{Ops: rectOps(area), Fill: &compose.Fill{Color: c}})
	}
	e.paintPageLayer(pg, st, area, area)
}

// paintPageLayer paints the background image or gradient of st over the
// rectangle whole of the page pg, positioned in area.
func (e *engine) paintPageLayer(pg *compose.Page, st *css.Style, area, whole compose.Rect) {
	g := &compose.Group{Transform: compose.Identity, Opacity: 1, Clip: &compose.Path{Ops: rectOps(whole), Fill: &compose.Fill{}}}
	switch {
	case st.BackgroundGradient != nil:
		for _, pa := range e.gradientTiles(st, area, whole, -1) {
			g.Content = append(g.Content, pa)
		}
	case st.BackgroundImage != "":
		pic := e.picture(st.BackgroundImage, -1)
		if pic == nil {
			return
		}
		for _, t := range e.tiles(st, pic, area, whole, -1) {
			g.Content = append(g.Content, &compose.Image{Rect: t, Source: pic.Source})
		}
	}
	if len(g.Content) > 0 {
		pg.Content = append(pg.Content, g)
	}
}

// paintPageBackground paints the page box's own background on pg, under
// the canvas (CSS Page 3, page backgrounds and painting order): its colour
// over the whole page, its image or gradient positioned in the page box's
// area, the page inside its margins, which has no border or padding. A
// root element that generates no box leaves the page blank, background
// included, as browsers print it (WPT css-page,
// root-element-display-none-print).
func (e *engine) paintPageBackground(pg *compose.Page, pb PageBox) {
	st := pb.Background
	if st == nil || (e.rootBox != nil && e.rootBox.node != nil && e.rootBox.st.Display == css.DisplayNone) {
		return
	}
	whole := compose.Rect{Width: pg.Width, Height: pg.Height}
	if c, ok := colorOf(st.BackgroundColor); ok {
		pg.Content = append(pg.Content, &compose.Path{Ops: rectOps(whole), Fill: &compose.Fill{Color: c}})
	}
	w, h := areaOf(pb)
	area := compose.Rect{X: pb.Bleed + pb.Margin[css.Left], Y: pb.Bleed + pb.Margin[css.Top], Width: w, Height: h}
	if st.BackgroundAttachment == css.AttachFixed {
		// A fixed background is positioned in the viewport, which on
		// paper is the page box (CSS Backgrounds 3, 3.5; CSS Page 3, 3.3).
		area = compose.Rect{X: pb.Bleed, Y: pb.Bleed, Width: pb.Width, Height: pb.Height}
	}
	e.paintPageLayer(pg, st, area, whole)
}

// dest converts a flow position to a destination.
func (e *engine) dest(x, y float64) compose.Dest {
	p := e.pageOf(y)
	pb := e.box(p)
	return compose.Dest{Page: p + 1, X: pb.Bleed + pb.Margin[css.Left] + x, Y: pb.Bleed + pb.Margin[css.Top] + y - e.pageTop(p)}
}

// anchors returns where the first box of every node with an ID landed, in
// tree order, for a document of n pages.
func (e *engine) anchors(rb *box, n int) map[string]compose.Dest {
	out := map[string]compose.Dest{}
	var walk func(*box)
	walk = func(b *box) {
		if b.id != "" {
			if _, ok := out[b.id]; !ok {
				x, y, ok := b.anchorX, b.anchorY, b.anchorSet
				if !ok && b.laidOut && b.kind != kindInline && b.kind != kindText && b.kind != kindBreak {
					x, y, ok = b.x, b.y, true
				}
				if ok {
					d := e.dest(clamp(x), max(clamp(y), 0))
					if d.Page <= n {
						out[b.id] = d
					}
				}
			}
		}
		for _, k := range b.kids {
			walk(k)
		}
	}
	walk(rb)
	return out
}

// links adds the link regions to the pages of res, cut at the page cuts,
// internal targets resolved against res.Anchors.
func (e *engine) links(res *Result, links []pendingLink, used map[*compose.StructElem]bool) {
	for _, pl := range links {
		href := pl.lk.href
		var dest *compose.Dest
		uri := ""
		if id, ok := strings.CutPrefix(href, "#"); ok {
			d, found := res.Anchors[id]
			if !found || id == "" {
				e.warnOnce(codeLink, fmt.Sprintf("link to %q left out: no element has that ID", href), pl.rect.Y)
				continue
			}
			dest = &d
		} else {
			uri = href
		}
		r := pl.rect
		if !(r.Width > 0) || !(r.Height > 0) {
			continue
		}
		p0, p1 := e.pageOf(max(r.Y, 0)), e.pageOf(max(r.Y+r.Height-1e-3, 0))
		for p := p0; p <= p1 && p < len(res.Pages); p++ {
			top := e.pageTop(p)
			pb := e.box(p)
			y0, y1 := max(r.Y, top), min(r.Y+r.Height, top+e.areaH(p))
			if y1 <= y0 {
				continue
			}
			l := compose.Link{
				Rect: compose.Rect{X: pb.Bleed + pb.Margin[css.Left] + r.X, Y: pb.Bleed + pb.Margin[css.Top] + y0 - top, Width: r.Width, Height: y1 - y0},
				URI:  uri, Dest: dest, Parent: pl.lk.elem,
			}
			if l.Parent != nil {
				used[l.Parent] = true
			}
			res.Pages[p].Links = append(res.Pages[p].Links, l)
		}
	}
}

// prune drops the structure elements no fragment hangs from, directly or
// through their kids, and reports whether el keeps content.
func prune(el *compose.StructElem, used map[*compose.StructElem]bool) bool {
	kids := el.Kids[:0]
	for _, k := range el.Kids {
		if prune(k, used) {
			kids = append(kids, k)
		}
	}
	el.Kids = kids
	return used[el] || len(kids) > 0
}
