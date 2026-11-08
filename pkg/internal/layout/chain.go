package layout

import (
	"cmp"
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// This file flows the content of a chain of linked boxes
// (css.Office.Chain) from one link into the next, as Writer flows the text
// of linked frames. The chain's content, its first link's children, is
// laid out on its own as a document whose pages are the links' content
// boxes, each as wide and as tall as its link's; each link then paints
// its page of that layout in its content box, and lays none of its own
// children out.

// maxChainLinks bounds the links of every chain of a document together.
// Anti-DoS guard, not spec-mandated: a chain of linked text boxes runs over
// a handful of frames, a newsletter's over a few dozen.
const maxChainLinks = 1000

// autoLinkHeight is the height a link without a definite height lays its
// share of the chain's content out in before it takes the height of what
// it holds, 200 inches: more than any page.
const autoLinkHeight = 14400

// chainEndID marks the end of a chain's content, whose anchor gives the
// height a link without a definite height holds.
const chainEndID = "\x00chain-end"

// chainLink is what a link of a chain paints: its page of the chain's
// content, and the height its content box takes when its style gives
// none.
type chainLink struct {
	frags []compose.Fragment
	auto  bool
	h     float64
}

// linkNode is one link met in the tree: its node, its place in the chain.
type linkNode struct {
	n     *Node
	index int
}

// prepareChains lays out the content of every chain of linked boxes of the
// tree rooted at root and returns what each link paints. A chain whose
// first link has no definite width lays its content out in its first link
// as any box does.
func prepareChains(ctx context.Context, root *Node, fonts *text.Fonts, opts Options) map[*Node]*chainLink {
	chains := map[string][]linkNode{}
	var names []string
	count := 0
	type item struct {
		n     *Node
		depth int
	}
	stack := []item{{root, 0}}
	for len(stack) > 0 && count < maxChainLinks {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := it.n
		if n == nil || it.depth >= maxTreeDepth {
			continue
		}
		if n.Kind == Element && n.Style != nil && n.Style.Chain != "" {
			name := n.Style.Chain
			if _, ok := chains[name]; !ok {
				names = append(names, name)
			}
			chains[name] = append(chains[name], linkNode{n: n, index: n.Style.ChainIndex})
			count++
		}
		for i := len(n.Children) - 1; i >= 0; i-- {
			stack = append(stack, item{n.Children[i], it.depth + 1})
		}
	}
	if len(names) == 0 {
		return nil
	}
	out := map[*Node]*chainLink{}
	for _, name := range names {
		links := chains[name]
		slices.SortStableFunc(links, func(a, b linkNode) int { return cmp.Compare(a.index, b.index) })
		layoutChain(ctx, links, fonts, opts, out)
	}
	return out
}

// layoutChain lays the content of the chain whose links are links, in
// order, out across them and records what each paints in out.
func layoutChain(ctx context.Context, links []linkNode, fonts *text.Fonts, opts Options, out map[*Node]*chainLink) {
	first := links[0].n
	var boxes []PageBox
	var nodes []*Node
	w := 0.0
	for i, l := range links {
		if i > 0 && l.index == links[i-1].index {
			// A second link at the same place stays empty.
			out[l.n] = &chainLink{}
			continue
		}
		lw, ok := linkWidth(l.n.Style)
		switch {
		case ok:
			w = lw
		case i == 0:
			return
		}
		h, ok := linkHeight(l.n.Style)
		out[l.n] = &chainLink{auto: !ok}
		if len(boxes) > 0 && boxes[len(boxes)-1].Height == autoLinkHeight {
			// The links after one that grows to its content stay empty.
			continue
		}
		if !ok {
			h = autoLinkHeight
		}
		boxes = append(boxes, PageBox{Width: max(w, 1), Height: max(h, 1)})
		nodes = append(nodes, l.n)
	}
	st := *first.Style
	st.Office = css.Office{}
	st.Position, st.Float, st.Display = css.Static, css.FloatNone, css.Block
	st.Width, st.Height = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	st.MinHeight, st.MaxHeight = css.Pts(0), css.Dimension{Kind: css.None}
	st.Margin, st.Padding, st.Border = [4]css.Dimension{}, [4]css.Dimension{}, [4]css.Border{}
	st.BackgroundColor, st.BackgroundImage, st.BackgroundGradient = css.Color{}, "", nil
	st.BoxShadow, st.Transform, st.ZIndex = nil, nil, css.ZIndex{}
	st.PageAnchor, st.AnchorPage, st.PageClamp = false, 0, false
	end := &Node{Kind: Element, Style: &css.Style{Display: css.Block, Height: css.Pts(0)}, ID: chainEndID}
	content := &Node{Kind: Element, Style: &st, Children: append(slices.Clone(first.Children), end)}
	sub := opts
	sub.Page = boxes[0]
	sub.PageFor, sub.MarginContent, sub.Placed, sub.FitFirstPage = nil, nil, nil, false
	sub.PageAt = func(i int) PageBox { return boxes[min(i, len(boxes)-1)] }
	sub.MaxPages = len(boxes) + 1
	res, err := Layout(ctx, content, fonts, sub)
	if err != nil {
		// Content too long for the links' pages, or a page cap: the chain
		// paints what the first pass could not; its first link lays its
		// content out alone.
		delete(out, first)
		return
	}
	for i, n := range nodes {
		if i >= len(res.Pages) {
			break
		}
		cl := out[n]
		if depthOf(res.Pages[i].Content) < MaxVectorDepth {
			cl.frags = res.Pages[i].Content
		}
		if cl.auto {
			cl.h = 0
			if d, ok := res.Anchors[chainEndID]; ok && d.Page == i+1 {
				cl.h = d.Y
			} else if ok && d.Page > i+1 {
				cl.h = autoLinkHeight
			}
		}
	}
}

// linkWidth returns the content width of a link from its style, false
// when the style gives none in points.
func linkWidth(st *css.Style) (float64, bool) {
	if st.Width.Kind != css.Points {
		return 0, false
	}
	w := st.Width.Value
	if st.BoxSizing == css.BorderBox {
		w -= st.Border[css.Left].Width + st.Border[css.Right].Width + pts(st.Padding[css.Left]) + pts(st.Padding[css.Right])
	}
	return w, w > 0
}

// linkHeight returns the content height of a link from its style, false
// when the style gives none in points.
func linkHeight(st *css.Style) (float64, bool) {
	if st.Height.Kind != css.Points {
		return 0, false
	}
	h := st.Height.Value
	if st.BoxSizing == css.BorderBox {
		h -= st.Border[css.Top].Width + st.Border[css.Bottom].Width + pts(st.Padding[css.Top]) + pts(st.Padding[css.Bottom])
	}
	return h, h > 0
}

// pts returns the length d in points, 0 for anything else.
func pts(d css.Dimension) float64 {
	if d.Kind == css.Points {
		return d.Value
	}
	return 0
}

// chainContent paints the share of its chain's content the link b holds
// in its content box.
func (p *painter) chainContent(b *box) {
	if b.chain == nil || len(b.chain.frags) == 0 || b.hidden {
		return
	}
	x, y := b.contentX()+p.dx, b.contentY()+p.dy
	g := &compose.Group{Opacity: 1, Transform: compose.Matrix{A: 1, D: 1, E: x, F: y}, Content: adopt(b.chain.frags, b.elem, 0)}
	page := p.e.monoPage(y, b.h)
	p.emit(b, true, g, y, y+b.h, page)
}
