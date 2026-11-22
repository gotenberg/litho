package layout

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// embedded paints the document the picture of the replaced box b embeds
// into its content box box, the document's viewport (HTML Standard,
// 15.3.2: a child navigable's viewport fits inside the content box of its
// container), clipped to it as a replaced element's content is (CSS
// Overflow 3, 3.1), with its links and, hung from b's element, its
// structure. In a turned flow the document stands upright on the page, as
// a picture does (CSS Writing Modes 4, 7).
//
// The leaves keep the structure elements and the order the document's own
// layout gave them: what the document paints is read in its own order,
// inside the frame's element.
func (p *painter) embedded(b *box, box typeset.Rect) {
	view := box
	var turn *typeset.Matrix
	if b.turn != css.HorizontalTB {
		cx, cy := box.X+float64(box.Width/2), box.Y+float64(box.Height/2)
		view = typeset.Rect{X: cx - float64(box.Height/2), Y: cy - float64(box.Width/2), Width: box.Height, Height: box.Width}
		m := uprightTurn(b.turn, cx, cy)
		turn = &m
	} else if page := p.e.monoPage(box.Y, box.Height); page >= 0 && p.e.cols == nil {
		view = p.e.snappedView(box, page)
	}
	doc := b.picture.Embed(p.e.ctx, view.Width, view.Height)
	if doc == nil || len(doc.Content) == 0 {
		return
	}
	if depthOf(doc.Content) > MaxEmbedDepth {
		p.e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("a frame whose document nests groups deeper than %d levels was left out", MaxEmbedDepth), box.Y)
		return
	}
	content := doc.Content
	if p.artifact || !p.hang(b.elem, doc.Structure) {
		// A repeated table header paints copies that hang from nothing,
		// and a frame without an element of its own hangs its content
		// from the element it paints in.
		var parent *typeset.StructElem
		if !p.artifact {
			parent = b.elem
		}
		content = adoptAll(content, parent, 0)
	}
	clip := &typeset.Path{Ops: rectOps(typeset.Rect{Width: view.Width, Height: view.Height}), Fill: &typeset.Fill{}}
	var f typeset.Fragment = &typeset.Group{Opacity: 1, Transform: typeset.Matrix{A: 1, D: 1, E: view.X, F: view.Y}, Clip: clip, Content: content}
	if turn != nil {
		f = &typeset.Group{Opacity: 1, Transform: *turn, Content: []typeset.Fragment{f}}
	}
	page := p.e.monoPage(box.Y, box.Height)
	if p.mono >= 0 {
		page = p.mono
	}
	c := p.holder(b, false)
	c.kids = append(c.kids, &ditem{frag: f, y0: box.Y, y1: box.Y + box.Height, page: page})
	if p.e.frameLeaves == nil {
		p.e.frameLeaves = map[typeset.Fragment]bool{}
	}
	p.e.frameLeaves[f] = true
	if p.artifact {
		return
	}
	for _, l := range doc.Links {
		if l.URI == "" {
			continue
		}
		r, ok := intersect(l.Rect, typeset.Rect{Width: view.Width, Height: view.Height})
		if !ok {
			continue
		}
		r.X += view.X
		r.Y += view.Y
		if turn != nil {
			r = turnedRect(b.turn, box.X+float64(box.Width/2), box.Y+float64(box.Height/2), r)
		}
		lk := &link{href: l.URI, elem: l.Parent, contents: l.Contents, described: true}
		p.links = append(p.links, pendingLink{lk: lk, rect: r})
	}
}

// snappedView returns the content box box, on page page, with its edges
// moved to the nearest whole CSS pixel of the page: Blink lays a frame's
// document out, and clips it, in the content box snapped to whole pixels
// (blink::LayoutEmbeddedContent::UpdateGeometry), so the document's
// edges and its borders' do not share a pixel. box itself when snapping
// leaves no area.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 paints the yellow
//     canvas of a framed document with a 1 pixel border, below lines of
//     text, on whole pixels, its border in the pixel row under it; laid out
//     on the unsnapped content box, 0.07 pixel into that row, litho's
//     canvas covered the border there as poppler draws it.
func (e *engine) snappedView(box typeset.Rect, page int) typeset.Rect {
	pb := e.box(page)
	ox, oy := pb.Bleed+pb.Margin[css.Left], pb.Bleed+pb.Margin[css.Top]-e.pageTop(page)
	snap := func(v, o float64) float64 { return float64(math.Round((v+o)/css.Pt)*css.Pt) - o }
	x0, y0 := snap(box.X, ox), snap(box.Y, oy)
	x1, y1 := snap(box.X+box.Width, ox), snap(box.Y+box.Height, oy)
	if !(x1 > x0) || !(y1 > y0) {
		return box
	}
	return typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// embeds reports a replaced box that paints an embedded document: its
// element holds the document's structure, not content of its own.
func (b *box) embeds() bool {
	return b.picture != nil && b.picture.Embed != nil
}

// hang hangs the kids of doc, the root of an embedded document's structure
// tree, from el, the element of the box the document paints into, once a
// layout, and reports whether the document's leaves keep their elements:
// false when el is not the element of such a box, or when the document
// would nest the tree deeper than [maxStructDepth], with a warning.
func (p *painter) hang(el, doc *typeset.StructElem) bool {
	depth, ok := p.e.embeds[el]
	switch {
	case el == nil || doc == nil || !ok:
		return false
	case depth < 0:
		return true
	case depth+structDepth(doc) > maxStructDepth:
		p.e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("a frame's document nesting structure roles deeper than %d hangs from the frame's element", maxStructDepth), -1)
		return false
	}
	p.e.embeds[el] = -1
	el.Kids = append(el.Kids, doc.Kids...)
	if el.Lang == "" {
		el.Lang = doc.Lang
	}
	return true
}

// structDepth returns the levels of the structure tree below root, at
// most [maxStructDepth] + 1: the walk stops there.
func structDepth(root *typeset.StructElem) int {
	type item struct {
		e     *typeset.StructElem
		depth int
	}
	deepest := 0
	stack := []item{{root, 0}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		deepest = max(deepest, it.depth)
		if it.depth > maxStructDepth {
			return it.depth
		}
		for _, k := range it.e.Kids {
			if k != nil {
				stack = append(stack, item{k, it.depth + 1})
			}
		}
	}
	return deepest
}

// adoptAll returns a copy of frags whose leaves, at every depth, hang
// from parent; depth is the group nesting walked so far, bounded by
// [MaxEmbedDepth].
func adoptAll(frags []typeset.Fragment, parent *typeset.StructElem, depth int) []typeset.Fragment {
	out := make([]typeset.Fragment, 0, len(frags))
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Group:
			g := *v
			if depth < MaxEmbedDepth {
				g.Content = adoptAll(v.Content, parent, depth+1)
			}
			out = append(out, &g)
		case *typeset.Text:
			c := *v
			c.Parent, c.Order = parent, 0
			out = append(out, &c)
		case *typeset.Image:
			c := *v
			c.Parent, c.Order = parent, 0
			out = append(out, &c)
		case *typeset.Path:
			c := *v
			c.Parent, c.Order = parent, 0
			out = append(out, &c)
		}
	}
	return out
}
