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
	pb := opts.Page
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !finite(pb.Width) || !finite(pb.Height) || pb.Width <= 0 || pb.Height <= 0 || pb.Width > maxPageSide || pb.Height > maxPageSide {
		return nil, fmt.Errorf("%w: page of %v by %v points", ErrInvalidOptions, pb.Width, pb.Height)
	}
	for _, m := range pb.Margin {
		if !finite(m) || m < 0 {
			return nil, fmt.Errorf("%w: page margin %v", ErrInvalidOptions, m)
		}
	}
	areaW := pb.Width - pb.Margin[css.Left] - pb.Margin[css.Right]
	areaH := pb.Height - pb.Margin[css.Top] - pb.Margin[css.Bottom]
	if areaW <= 0 || areaH <= 0 {
		return nil, fmt.Errorf("%w: margins leave a page area of %v by %v points", ErrInvalidOptions, areaW, areaH)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("laying out: %w", err)
	}
	e := newEngine(ctx, fonts, opts, areaW, areaH)
	rb := e.generate(root)
	if e.err != nil {
		return nil, e.err
	}
	e.rootBox = rb
	e.layoutRoot(rb)
	if e.err != nil {
		return nil, e.err
	}
	disp, links, canvas := e.paint(rb)
	if e.err != nil {
		return nil, e.err
	}
	res := &Result{Structure: e.root}
	used := map[*compose.StructElem]bool{}
	res.Pages = e.assemble(disp, canvas, rb, used)
	if len(res.Pages) > e.maxPages {
		return nil, fmt.Errorf("%w: %d pages, at most %d", ErrTooManyPages, len(res.Pages), e.maxPages)
	}
	res.Anchors = e.anchors(rb, len(res.Pages))
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
// list d need.
func (e *engine) pageCount(rb *box, d *ditem) int {
	bottom := rb.y + rb.borderH() + max(rb.m[css.Bottom], 0)
	n := e.pageOf(max(bottom-1e-3, 0)) + 1
	var walk func(*ditem)
	walk = func(it *ditem) {
		for _, k := range it.kids {
			if k.group {
				walk(k)
				continue
			}
			if k.page >= 0 {
				n = max(n, k.page+1)
			} else {
				n = max(n, e.pageOf(max(k.y0, 0))+1)
			}
		}
	}
	walk(d)
	return max(min(n, e.maxPages+1), 1)
}

// assemble slices the display list into pages: each page paints the canvas,
// then the part of the flow its page area shows, translated to the page and
// clipped to the page area. used collects the structure elements the
// fragments hang from.
func (e *engine) assemble(d *ditem, canvas, rb *box, used map[*compose.StructElem]bool) []*compose.Page {
	n := e.pageCount(rb, d)
	pb := e.opts.Page
	pages := make([]*compose.Page, n)
	e.pageFrags = make([]int, n)
	perPage := e.distribute(d, 0, n, used)
	for k := range n {
		pg := &compose.Page{Width: pb.Width, Height: pb.Height}
		e.paintCanvas(pg, canvas)
		if frags := perPage[k]; len(frags) > 0 {
			top := float64(k) * e.pageH
			pg.Content = append(pg.Content, &compose.Group{
				Transform: compose.Matrix{A: 1, D: 1, E: pb.Margin[css.Left], F: pb.Margin[css.Top] - top},
				Clip: &compose.Path{Ops: rectOps(compose.Rect{
					X: -pb.Margin[css.Left], Y: top, Width: pb.Width, Height: e.pageH,
				}), Fill: &compose.Fill{}},
				Opacity: 1,
				Content: frags,
			})
		}
		pages[k] = pg
	}
	return pages
}

// distribute returns the fragments of the container it per page, groups
// rebuilt on each page they reach, depth the groups already open.
func (e *engine) distribute(it *ditem, depth, n int, used map[*compose.StructElem]bool) map[int][]compose.Fragment {
	out := map[int][]compose.Fragment{}
	for _, k := range it.kids {
		if k.group {
			sub := e.distribute(k, depth+1, n, used)
			for _, p := range slices.Sorted(mapKeys(sub)) {
				frags := sub[p]
				if depth+1 >= maxGroupDepth {
					e.warnOnce(codeGroupDepth, fmt.Sprintf("opacity and clipping nested deeper than %d levels painted without the inner groups", maxGroupDepth), -1)
					out[p] = append(out[p], frags...)
					continue
				}
				if e.admit(p) {
					out[p] = append(out[p], &compose.Group{Transform: compose.Identity, Clip: k.clip, Opacity: k.opacity, Content: frags})
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
			if !e.admit(p) {
				continue
			}
			f := k.frag
			if p1 > p0 {
				f = cloneFragment(f)
			}
			markUsed(f, used)
			out[p] = append(out[p], f)
		}
	}
	return out
}

// admit counts one more fragment on page p and reports whether it fits
// [maxPageFragments].
func (e *engine) admit(p int) bool {
	if e.pageFrags[p] >= maxPageFragments {
		e.warnOnce(codeTooManyFragments, fmt.Sprintf("a page of more than %d fragments was cut short", maxPageFragments), e.pageTop(p))
		return false
	}
	e.pageFrags[p]++
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

// paintCanvas paints the canvas background of the page pg from the box
// canvas (CSS 2.1, 14.2): the colour over the whole page, the image
// positioned in the page area and repeated over the page.
func (e *engine) paintCanvas(pg *compose.Page, canvas *box) {
	if canvas == nil {
		return
	}
	st := canvas.st
	whole := compose.Rect{Width: pg.Width, Height: pg.Height}
	if c, ok := colorOf(st.BackgroundColor); ok {
		pg.Content = append(pg.Content, &compose.Path{Ops: rectOps(whole), Fill: &compose.Fill{Color: c}})
	}
	if st.BackgroundImage == "" {
		return
	}
	pic := e.picture(st.BackgroundImage, -1)
	if pic == nil {
		return
	}
	pb := e.opts.Page
	area := compose.Rect{X: pb.Margin[css.Left], Y: pb.Margin[css.Top], Width: e.pageW, Height: e.pageH}
	g := &compose.Group{Transform: compose.Identity, Opacity: 1, Clip: &compose.Path{Ops: rectOps(whole), Fill: &compose.Fill{}}}
	for _, t := range e.tiles(st, pic, area, whole, -1) {
		g.Content = append(g.Content, &compose.Image{Rect: t, Source: pic.Source})
	}
	if len(g.Content) > 0 {
		pg.Content = append(pg.Content, g)
	}
}

// dest converts a flow position to a destination.
func (e *engine) dest(x, y float64) compose.Dest {
	p := e.pageOf(y)
	pb := e.opts.Page
	return compose.Dest{Page: p + 1, X: pb.Margin[css.Left] + x, Y: pb.Margin[css.Top] + y - e.pageTop(p)}
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
	pb := e.opts.Page
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
			y0, y1 := max(r.Y, top), min(r.Y+r.Height, top+e.pageH)
			if y1 <= y0 {
				continue
			}
			l := compose.Link{
				Rect: compose.Rect{X: pb.Margin[css.Left] + r.X, Y: pb.Margin[css.Top] + y0 - top, Width: r.Width, Height: y1 - y0},
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
