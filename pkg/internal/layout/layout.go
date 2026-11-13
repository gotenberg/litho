package layout

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxPageSide is the largest page side [Layout] accepts, in points: the
// typeset model's bound (ISO 32000-1, Annex C.2, Table C.1).
const maxPageSide = 14400

// layout is [Layout].
func layout(ctx context.Context, root *Node, fonts *text.Fonts, opts Options) (*Result, error) {
	if root == nil || fonts == nil {
		return nil, fmt.Errorf("%w: nil root or font index", ErrInvalidOptions)
	}
	if err := checkPage(opts.Page); err != nil {
		return nil, err
	}
	if opts.PageAt != nil {
		// A pass after the first asks for the same pages again: each
		// index is asked once.
		opts.PageAt = memoPages(opts.PageAt)
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
	chains := prepareChains(ctx, root, fonts, opts)
	var e *engine
	var rb *box
	var seeds []wrapSeed
	defer func() { e.release() }()
	for pass := 0; ; pass++ {
		var err error
		prev := e
		e, rb, err = layoutFlow(ctx, root, fonts, opts, first, seeds, chains)
		prev.release()
		if err != nil {
			return nil, err
		}
		found := e.collectWordWraps(rb, e.collectWraps(rb, nil))
		limit := maxWrapPasses
		if hasWordSeed(found) {
			limit += maxWordPasses
		}
		if sameWraps(found, seeds) || pass+1 >= limit {
			break
		}
		seeds = found
	}
	disp, links, canvas := e.paint(rb)
	e.paintNotes()
	if e.err != nil {
		return nil, e.err
	}
	if opts.Placed != nil || opts.Paginated != nil {
		n := e.pageCount(rb, disp)
		if opts.Placed != nil {
			opts.Placed(e.anchors(rb, n))
		}
		if opts.Paginated != nil {
			infos := make([]PageInfo, n)
			for k := range infos {
				infos[k] = e.infoOf(k)
			}
			opts.Paginated(infos)
		}
	}
	res := &Result{Structure: e.root, ContentWidth: e.overflowRight(rb)}
	used := map[*typeset.StructElem]bool{}
	res.Pages = e.assemble(disp, canvas, rb, used)
	if len(res.Pages) > e.maxPages {
		return nil, fmt.Errorf("%w: %d, at most %d", limits.ErrTooManyPages, len(res.Pages), e.maxPages)
	}
	res.Anchors = e.anchors(rb, len(res.Pages))
	e.anchorsFound = res.Anchors
	res.Extents = e.extents(rb, len(res.Pages))
	e.links(res, links, used)
	typeset.PruneTree(e.root, used)
	dropPrunedDests(e.root, res)
	for _, w := range e.warnings {
		page := 0
		if w.y >= 0 {
			page = min(e.pageOf(w.y)+1, len(res.Pages))
		}
		res.Warnings = append(res.Warnings, typeset.Warning{Code: w.code, Effect: w.effect, Message: w.msg, Page: page})
		if opts.Logger != nil {
			opts.Logger.Debug("layout: "+w.msg, slog.String("code", w.code.String()), slog.Int("page", page))
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
		ctx: ctx, fonts: fonts, opts: opts, pageW: w, pageH: h, maxPages: maxPages, mem: limits.BudgetFrom(ctx),
		warned: map[string]bool{}, images: map[string]*Picture{},
		metrics: map[*css.Style]text.Metrics{}, spaces: map[spaceKey]float64{}, hyphens: map[spaceKey]*hyphenMark{}, emMarks: map[spaceKey]*emphasisMark{}, missing: map[rune]bool{},
		root: &typeset.StructElem{Role: "Document", Lang: opts.Lang},
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
				if fx := k.effects(); fx.clip != nil {
					if y0, y1, ok := pathSpan(fx.clip.Ops); ok {
						klo, khi = max(lo, y0), min(hi, y1)
					}
				}
				if khi > klo && !k.fixedGroup() {
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
	n = max(n, e.positionedPages(rb, clipped), e.notePages())
	return max(min(n, e.maxPages+1), 1)
}

// positionedPages returns the pages the border boxes of the absolutely
// positioned boxes under rb reach, painted or not. An absolutely positioned
// box adds its border box to the scrollable overflow of its containing
// block, and so of the root (CSS Overflow 3, 2.2), which the pages show
// whole, as Chromium prints it: a tall positioned box with nothing to paint
// still takes its pages. A fixed box is on every page already, and the boxes
// a clipping or transformed box holds are left to the display list, which
// knows where they paint. A page-anchored box takes its anchor page alone.
// With clipped, a viewport that clips each page, a
// box takes only the page its top is on.
func (e *engine) positionedPages(rb *box, clipped bool) int {
	n := 0
	var walk func(b *box)
	walk = func(b *box) {
		for _, k := range b.kids {
			if k.fixed || k.transformed() {
				continue
			}
			if k.pageAnchored && k.laidOut {
				// A page-anchored box lies on its anchor page whole, its
				// margins included: it takes that page and no other,
				// whatever its height in flow coordinates.
				n = max(n, k.anchorPage+1)
				continue
			}
			if k.abs && k.laidOut {
				end := k.y + k.borderH() - 1e-3
				if clipped {
					end = k.y
				}
				n = max(n, e.pageOf(max(end, k.y, 0))+1)
			}
			if k.kind == kindBlock && k.st.Overflow != css.OverflowVisible && !k.toViewport() || k.mc != nil {
				// The display list knows where a multi-column container's
				// positioned boxes paint, moved to their columns.
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
func pathSpan(ops []typeset.PathOp) (float64, float64, bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, op := range ops {
		n := 1
		switch op.Kind {
		case typeset.CurveTo:
			n = 3
		case typeset.ClosePath:
			n = 0
		case typeset.MoveTo, typeset.LineTo:
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
func (e *engine) assemble(d *ditem, canvas, rb *box, used map[*typeset.StructElem]bool) []*typeset.Page {
	n := e.pageCount(rb, d)
	pages := make([]*typeset.Page, n)
	e.pageFrags = make([]int, n)
	free := map[typeset.Fragment]bool{}
	perPage := e.distribute(d, 0, n, used, free)
	fitH := 0.0
	if e.opts.FitFirstPage {
		fitH = e.fittedHeight(rb, d)
	}
	for k := range n {
		pb := e.assembledBox(k, fitH)
		pg := &typeset.Page{Width: pb.Width + 2*pb.Bleed, Height: pb.Height + 2*pb.Bleed}
		e.paintPageBackground(pg, pb)
		e.paintMarks(pg, pb)
		sheet := len(pg.Content)
		e.paintCanvas(pg, canvas, rb, pb, k)
		e.paintFrame(pg, pb)
		if e.opts.MarginContent != nil {
			pg.Content = append(pg.Content, e.opts.MarginContent(e.infoOf(k), n)...)
		}
		if e.opts.MarginLinks != nil {
			for _, l := range e.opts.MarginLinks(e.infoOf(k)) {
				if l.Parent != nil {
					e.root.Kids = append(e.root.Kids, l.Parent)
					used[l.Parent] = true
				}
				pg.Links = append(pg.Links, l)
			}
		}
		top := e.pageTop(k)
		// The note area at the top of the page area pushes its slice of
		// the flow down.
		shift := e.noteShift(k)
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
			clip := typeset.Rect{X: -pb.Margin[css.Left], Y: top, Width: pb.Width, Height: e.sliceH(k)}
			if e.opts.BrowserRules {
				// Across the page Blink cuts the slice at the page area
				// too: content overflowing into the side margins, a fixed
				// box's or an absolutely positioned one's, is not drawn
				// there. A word processor draws there.
				clip.X, clip.Width = 0, max(pb.Width-pb.Margin[css.Left]-pb.Margin[css.Right], 0)
			}
			if free[frags[i]] {
				clip = typeset.Rect{X: -pb.Margin[css.Left], Y: top - pb.Margin[css.Top], Width: pb.Width, Height: pb.Height}
			}
			pg.Content = append(pg.Content, &typeset.Group{
				Transform: typeset.Matrix{A: 1, D: 1, E: pb.Margin[css.Left], F: pb.Margin[css.Top] - top + shift},
				Clip:      &typeset.Path{Ops: rectOps(clip), Fill: &typeset.Fill{}},
				Opacity:   1,
				Content:   frags[i:j],
			})
			i = j
		}
		e.assembleNotes(pg, k, pb, used)
		trim(pg, pb, sheet)
		pages[k] = pg
	}
	return pages
}

// distribute returns the fragments of the container it per page, groups
// rebuilt on each page they reach, depth the groups already open. free
// collects the top-level groups of transformed boxes that land on one page
// and the top-level fragments placed on one page.
func (e *engine) distribute(it *ditem, depth, n int, used map[*typeset.StructElem]bool, free map[typeset.Fragment]bool) map[int][]typeset.Fragment {
	out := map[int][]typeset.Fragment{}
	for _, k := range it.kids {
		if k.group && k.fixedGroup() {
			e.repeat(k, depth, n, used, out)
			continue
		}
		if k.group {
			sub := e.distribute(k, depth+1, n, used, free)
			for _, p := range slices.Sorted(mapKeys(sub)) {
				frags := sub[p]
				if depth+1 >= maxGroupDepth {
					e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("opacity and clipping nested deeper than %d levels painted without the inner groups", maxGroupDepth), -1)
					out[p] = append(out[p], frags...)
					continue
				}
				if e.admit(p) {
					if fx := k.effects(); len(fx.filter) > 0 {
						var band *typeset.Rect
						if fx.flow {
							band = &typeset.Rect{Y: e.pageTop(p), Height: e.sliceH(p)}
						}
						frags = e.filtered(k, frags, band, p)
					}
					g := groupOf(k, frags)
					if depth == 0 && k.effects().transform != nil && len(sub) == 1 {
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

// groupOf returns the group of the display list group it, holding content.
// A box of opacity below 1 or of a blend mode is composited as a whole
// (CSS Color 4, 4.1; CSS Compositing 1, 3.2): its content is one isolated
// transparency group (ISO 32000-1, 11.4), as every stacking context is in
// CSS Compositing 1, 3.3, unless the content is one object, whose own
// alpha and blend mode composite alike and cost no form XObject. A group
// that holds something blending is isolated whatever it paints.
//
// # Corpus evidence
//
//   - probe, html: Chromium writes every box of opacity 0.5 as an isolated
//     transparency group form painted under /ca 0.5, one filled rectangle
//     or one line of text included.
func groupOf(it *ditem, content []typeset.Fragment) *typeset.Group {
	fx := it.effects()
	g := &typeset.Group{Transform: groupTransform(it), Clip: fx.clip, Opacity: fx.opacity, Blend: fx.blend, Content: content}
	if fx.isolate || fx.mask != nil || (fx.opacity < 1 || fx.blend != typeset.BlendNormal) && !oneObject(content) {
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
	// TODO: one copy with oneObject in pkg/internal/svg/render.go; typeset could hold it.
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

// groupTransform returns the transform of the group it.
func groupTransform(it *ditem) typeset.Matrix {
	if m := it.effects().transform; m != nil {
		return *m
	}
	return typeset.Identity
}

// repeat adds the content of the fixed group it, laid out on the first
// page, to every one of the n pages of out, moved to each page's place in
// the flow (CSS 2.1, 9.6.1). The copies after the first are artifacts.
func (e *engine) repeat(it *ditem, depth, n int, used map[*typeset.StructElem]bool, out map[int][]typeset.Fragment) {
	if depth+1 >= maxGroupDepth {
		e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("opacity and clipping nested deeper than %d levels painted without the inner groups", maxGroupDepth), -1)
		return
	}
	leaves := countLeaves(it)
	for p := range n {
		if e.pageFrags[p]+leaves > maxPageFragments {
			e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("a page of more than %d fragments was cut short", maxPageFragments), e.pageTop(p))
			continue
		}
		e.pageFrags[p] += leaves
		frags := e.flatten(it, depth+1, p > 0)
		if p == 0 {
			for _, f := range frags {
				markTree(f, used)
			}
		}
		out[p] = append(out[p], &typeset.Group{
			Transform: typeset.Matrix{A: 1, D: 1, F: e.pageTop(p)},
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
func (e *engine) flatten(it *ditem, depth int, artifact bool) []typeset.Fragment {
	var out []typeset.Fragment
	for _, k := range it.kids {
		if k.group {
			if depth+1 >= maxGroupDepth {
				out = append(out, e.flatten(k, depth+1, artifact)...)
				continue
			}
			frags := e.flatten(k, depth+1, artifact)
			if len(k.effects().filter) > 0 {
				frags = e.filtered(k, frags, nil, -1)
			}
			out = append(out, groupOf(k, frags))
			continue
		}
		f := e.fitLeaf(k.frag, depth)
		if f == nil {
			continue
		}
		if artifact {
			f = cloneFragment(f)
			switch v := f.(type) {
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
		out = append(out, f)
	}
	return out
}

// markTree records the structure elements the fragments of f hang from.
func markTree(f typeset.Fragment, used map[*typeset.StructElem]bool) {
	if g, ok := f.(*typeset.Group); ok {
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
		e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("a page of more than %d fragments was cut short", maxPageFragments), e.pageTop(p))
		return false
	}
	e.pageFrags[p] += n
	return true
}

// mapKeys returns the keys of m, for sorting.
func mapKeys(m map[int][]typeset.Fragment) func(func(int) bool) {
	return func(yield func(int) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// cloneFragment returns a copy of a fragment painted on several pages.
func cloneFragment(f typeset.Fragment) typeset.Fragment {
	switch v := f.(type) {
	case *typeset.Path:
		c := *v
		return &c
	case *typeset.Image:
		c := *v
		return &c
	case *typeset.Text:
		c := *v
		return &c
	case *typeset.Group:
		c := *v
		return &c
	}
	return f
}

// markUsed records the structure element f hangs from.
func markUsed(f typeset.Fragment, used map[*typeset.StructElem]bool) {
	switch v := f.(type) {
	case *typeset.Text:
		if v.Parent != nil {
			used[v.Parent] = true
		}
	case *typeset.Image:
		if v.Parent != nil {
			used[v.Parent] = true
		}
	case *typeset.Path:
		if v.Parent != nil {
			used[v.Parent] = true
		}
	}
}

// paintCanvas paints the canvas background of page k, pg, whose page box
// is pb, from the box canvas (CSS 2.1, 14.2) over the page box's border
// box, the page area, not its margins (CSS Page 3, 3.1). Its image is
// sized and positioned in the background origin box of the root rb, as if
// painted for the root alone (CSS Backgrounds 3, 2.11.2), the root's
// slice of the flow on the page moved as its content is. A fixed image is
// positioned in the viewport, on paper the page area.
func (e *engine) paintCanvas(pg *typeset.Page, canvas, rb *box, pb PageBox, k int) {
	if canvas == nil {
		return
	}
	st := canvas.st
	w, h := areaOf(pb)
	whole := typeset.Rect{X: pb.Margin[css.Left], Y: pb.Margin[css.Top], Width: w, Height: h}
	if c, ok := colorOf(st.BackgroundColor); ok {
		pg.Content = append(pg.Content, &typeset.Path{Ops: rectOps(whole), Fill: &typeset.Fill{Color: c}})
	}
	area := whole
	if rb != nil && rb.laidOut && rb.borderW() > 0 && rb.borderH() > 0 && st.BackgroundAttachment != css.AttachFixed {
		r := typeset.Rect{X: pb.Margin[css.Left] + rb.x, Y: pb.Margin[css.Top] - e.pageTop(k) + e.noteShift(k) + rb.y, Width: rb.borderW(), Height: rb.borderH()}
		switch st.BackgroundOrigin {
		case css.BorderEdge, css.TextEdge:
			area = r
		case css.PaddingEdge:
			area, _ = insetRect(r, [4][2]float64{}, rb.bd)
		case css.ContentEdge:
			area, _ = insetRect(r, [4][2]float64{}, addSides(rb.bd, rb.pd))
		}
	}
	e.paintPageLayer(pg, st, area, whole)
}

// paintPageLayer paints the background image or gradient of st over the
// rectangle whole of the page pg, positioned in area.
func (e *engine) paintPageLayer(pg *typeset.Page, st *css.Style, area, whole typeset.Rect) {
	g := &typeset.Group{Transform: typeset.Identity, Opacity: 1, Clip: &typeset.Path{Ops: rectOps(whole), Fill: &typeset.Fill{}}}
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
			g.Content = append(g.Content, &typeset.Image{Rect: t, Source: pic.Source})
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
// included, as browsers print it.
//
// # Corpus evidence
//
//   - wpt css/css-page/root-element-display-none-print.html: the page of a
//     root of display: none matches a blank reference, page background
//     included.
func (e *engine) paintPageBackground(pg *typeset.Page, pb PageBox) {
	st := pb.Background
	if st == nil || (e.rootBox != nil && e.rootBox.node != nil && e.rootBox.st.Display == css.DisplayNone) {
		return
	}
	whole := typeset.Rect{Width: pg.Width, Height: pg.Height}
	if c, ok := colorOf(st.BackgroundColor); ok {
		pg.Content = append(pg.Content, &typeset.Path{Ops: rectOps(whole), Fill: &typeset.Fill{Color: c}})
	}
	w, h := areaOf(pb)
	area := typeset.Rect{X: pb.Bleed + pb.Margin[css.Left], Y: pb.Bleed + pb.Margin[css.Top], Width: w, Height: h}
	if st.BackgroundAttachment == css.AttachFixed {
		// A fixed background is positioned in the viewport, which on
		// paper is the page box (CSS Backgrounds 3, 3.5; CSS Page 3, 3.3).
		area = typeset.Rect{X: pb.Bleed, Y: pb.Bleed, Width: pb.Width, Height: pb.Height}
	}
	e.paintPageLayer(pg, st, area, whole)
}

// dest converts a flow position to a destination.
func (e *engine) dest(x, y float64) typeset.Dest {
	p := e.pageOf(y)
	pb := e.box(p)
	return typeset.Dest{Page: p + 1, X: pb.Bleed + pb.Margin[css.Left] + x, Y: pb.Bleed + pb.Margin[css.Top] + y - e.pageTop(p)}
}

// anchors returns where the first box of every node with an ID landed, in
// tree order, for a document of n pages.
func (e *engine) anchors(rb *box, n int) map[string]typeset.Dest {
	out := map[string]typeset.Dest{}
	var walk func(*box)
	walk = func(b *box) {
		if b.id != "" {
			if _, ok := out[b.id]; !ok {
				x, y, ok := b.anchorX, b.anchorY, b.anchorSet
				if !ok && b.laidOut && b.kind != kindInline && b.kind != kindText && b.kind != kindBreak {
					x, y, ok = b.x, b.y, true
				}
				if ok {
					x, y = toFlow(b, x, y)
					d := e.dest(clamp(x), max(clamp(y), 0))
					if b.elem != e.root {
						// The root is no destination worth naming, and a
						// converter that merges several layouts drops it.
						d.Elem = b.elem
					}
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
	e.noteAnchors(out, n)
	return out
}

// links adds the link regions to the pages of res, cut at the page cuts,
// internal targets resolved against res.Anchors.
func (e *engine) links(res *Result, links []pendingLink, used map[*typeset.StructElem]bool) {
	for _, pl := range links {
		l, ok := e.resolveLink(pl)
		if !ok {
			continue
		}
		r := pl.rect
		p0, p1 := e.pageOf(max(r.Y, 0)), e.pageOf(max(r.Y+r.Height-1e-3, 0))
		for p := p0; p <= p1 && p < len(res.Pages); p++ {
			top := e.pageTop(p)
			pb := e.box(p)
			y0, y1 := max(r.Y, top), min(r.Y+r.Height, top+e.sliceH(p))
			if y1 <= y0 {
				continue
			}
			l.Rect = typeset.Rect{X: pb.Bleed + pb.Margin[css.Left] + r.X, Y: pb.Bleed + pb.Margin[css.Top] + y0 - top, Width: r.Width, Height: y1 - y0}
			if l.Parent != nil {
				used[l.Parent] = true
			}
			res.Pages[p].Links = append(res.Pages[p].Links, l)
		}
	}
	e.noteLinks(res, used)
}

// resolveLink returns the link of the region pl, its rectangle unset, its
// internal target resolved against the anchors e.anchors found; false for
// an empty region or a target no element names, which it warns about.
func (e *engine) resolveLink(pl pendingLink) (typeset.Link, bool) {
	r := pl.rect
	if !numeric.Positive(r.Width) || !numeric.Positive(r.Height) {
		return typeset.Link{}, false
	}
	href := pl.lk.href
	if id, ok := strings.CutPrefix(href, "#"); ok {
		d, found := e.anchorsFound[id]
		if !found {
			d, found = e.opts.Targets[id]
		}
		if !found || id == "" {
			// The text prints; the target the link names is not in the
			// source either, so nothing of the source is missing.
			e.warnOnce(warn.LinkUnresolved, warn.Approximated, fmt.Sprintf("link to %q left out: no element has that ID", href), r.Y)
			return typeset.Link{}, false
		}
		return typeset.Link{Dest: &d, Parent: pl.lk.elem, Contents: pl.lk.describe()}, true
	}
	return typeset.Link{URI: href, Parent: pl.lk.elem, Contents: pl.lk.describe()}, true
}

// dropPrunedDests clears the element of every anchor and link destination
// of res that [typeset.PruneTree] dropped from the tree under root: the destination keeps
// its page position alone.
func dropPrunedDests(root *typeset.StructElem, res *Result) {
	kept := map[*typeset.StructElem]bool{}
	stack := []*typeset.StructElem{root}
	for len(stack) > 0 {
		el := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		kept[el] = true
		stack = append(stack, el.Kids...)
	}
	for id, d := range res.Anchors {
		if d.Elem != nil && !kept[d.Elem] {
			d.Elem = nil
			res.Anchors[id] = d
		}
	}
	for _, pg := range res.Pages {
		for i, l := range pg.Links {
			if l.Dest != nil && l.Dest.Elem != nil && !kept[l.Dest.Elem] {
				d := *l.Dest
				d.Elem = nil
				pg.Links[i].Dest = &d
			}
		}
	}
}

// layoutFlow lays the tree rooted at root out on pages whose first is first,
// the rectangles of seeds kept clear as the wrapped boxes of the previous
// pass ask (framewrap.go), the links of chains painting what chains
// holds for them (chain.go), and returns the engine and the root box.
func layoutFlow(ctx context.Context, root *Node, fonts *text.Fonts, opts Options, first PageBox, seeds []wrapSeed, chains map[*Node]*chainLink) (*engine, *box, error) {
	areaW, areaH := areaOf(first)
	e := newEngine(ctx, fonts, opts, areaW, areaH)
	e.wraps, e.chains = seeds, chains
	for _, s := range seeds {
		if s.node != nil {
			if e.frozen == nil {
				e.frozen = map[*Node]wrapSeed{}
			}
			e.frozen[s.node] = s
		}
	}
	e.geom.init(e, first)
	e.rtlRoot = root.Style != nil && root.Style.Direction == css.RTL
	rb := e.generate(root)
	if e.err != nil {
		return nil, nil, e.err
	}
	e.anonymize(rb)
	if e.foot != nil && !e.geom.varies() {
		// Footnote areas shorten the slices of the flow the pages show.
		e.geom = geometry{}
		e.geom.init(e, first)
	}
	if name := pageStart(rb); name != "" {
		// The first page's type is the root's start page value (CSS Page
		// 3, 4.1): its box, and the flow's width, follow it.
		e.mark(0, name, false, nil)
		if opts.PageFor != nil {
			first = opts.PageFor(e.infoOf(0))
			if err := checkPage(first); err != nil {
				return nil, nil, err
			}
			e.pageW, e.pageH = areaOf(first)
			e.geom = geometry{}
			e.geom.init(e, first)
		}
	}
	e.rootBox = rb
	e.layoutRoot(rb)
	e.layoutPositioned(rb)
	e.finishNotes(rb.y + rb.borderH())
	if e.err != nil {
		return nil, nil, e.err
	}
	return e, rb, nil
}

// memoPages returns at, asked once per index.
func memoPages(at func(int) PageBox) func(int) PageBox {
	seen := map[int]PageBox{}
	return func(i int) PageBox {
		if pb, ok := seen[i]; ok {
			return pb
		}
		pb := at(i)
		seen[i] = pb
		return pb
	}
}
