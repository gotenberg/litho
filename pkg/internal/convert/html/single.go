package html

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// A single page ([Options.SinglePage]) is the document laid out as it is
// printed, in one pass: on the page boxes of its paged print, each as tall
// as the bound of a single page ([printSetup.layoutBox]), so the flow runs
// on in one column at the width of the paper's page area, at the scale
// and with the shrink to fit of a document wider than its area that a
// paged print applies ([prepared.refit]). Nothing ends the page
// ([unbroken]). The layout trims the first page to the content it holds
// plus its top and bottom margins (layout.Options.FitFirstPage), and that
// page is written. The page keeps the width of the first page's box: the
// paper's in its orientation, or its @page size's.
//
// Chromium's single page, as Gotenberg prints it, takes its height from
// the document laid out on a screen 800 CSS pixels wide, then prints it on
// the paper's page area with its forced breaks: text that wraps taller on
// the paper, or follows a forced break, is lost. litho does not follow it
// there.

// unbrokenCheck is how many nodes [unbroken] visits between two checks of
// its context.
const unbrokenCheck = 1024

// unbroken readies the tree under root to be laid out on a single page,
// one continuous column nothing ends: a forced page break, to any page,
// and a page break a box avoids become auto, since a forced break only
// breaks a fragmented flow (CSS Fragmentation 3, 3.1); and every box takes
// the page name first, the start page value of the root, since a change of
// page name forces a break (CSS Page 3, 4.1): the page keeps the first
// page's box, and a later named page changes nothing. Column breaks, and
// a break avoided in any fragmentation context, stay: the columns of a
// multi-column container are still fragmentainers. Styles shared by
// several nodes stay shared: each is copied once. It returns ctx's error
// when ctx ends first.
func unbroken(ctx context.Context, root *layout.Node, first string) error {
	done := map[*css.Style]*css.Style{}
	stack := []*layout.Node{root}
	for visited := 0; len(stack) > 0; visited++ {
		if visited%unbrokenCheck == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		name := ""
		if n == root {
			name = first
		}
		if st := n.Style; st != nil && paged(st, name) {
			c, ok := done[st]
			if !ok || c.Page != name {
				cp := *st
				cp.BreakBefore, cp.BreakAfter = unpaged(cp.BreakBefore), unpaged(cp.BreakAfter)
				if cp.BreakInside == css.InsideAvoidPage {
					cp.BreakInside = css.InsideAuto
				}
				cp.Page = name
				c = &cp
				if n != root {
					done[st] = c
				}
			}
			n.Style = c
		}
		stack = append(stack, n.Children...)
		if n.Marker != nil {
			stack = append(stack, n.Marker)
		}
		if n.Footnote != nil {
			stack = append(stack, n.Footnote)
		}
	}
	return nil
}

// paged reports a style that names something [unbroken] undoes, name being
// the page name it takes.
func paged(st *css.Style, name string) bool {
	return unpaged(st.BreakBefore) != st.BreakBefore || unpaged(st.BreakAfter) != st.BreakAfter ||
		st.BreakInside == css.InsideAvoidPage || st.Page != name
}

// unpaged returns the break value b takes on a page nothing ends: auto for
// a forced or avoided page break, b otherwise.
func unpaged(b css.Break) css.Break {
	switch b {
	case css.Page, css.LeftPage, css.RightPage, css.BreakAvoidPage:
		return css.BreakAuto
	case css.BreakAuto, css.BreakAvoid, css.Column, css.BreakAvoidColumn:
		return b
	}
	return b
}

// verticalRoot reports a document whose principal writing mode is
// vertical: the root's, or, as CSS Writing Modes 4, 8 asks of an HTML
// document, its body's, the first element child of the root. The layout
// lays such a single page out growing sideways, as tall as the paper.
func verticalRoot(root *layout.Node) bool {
	if root == nil || root.Style == nil {
		return false
	}
	wm := root.Style.WritingMode
	for _, k := range root.Children {
		if k != nil && k.Kind == layout.Element && k.Style != nil {
			wm = k.Style.WritingMode
			break
		}
	}
	return wm.Vertical()
}

// singleViewport returns the height, in points of the layout, of the
// initial containing block of a single page: the page area of its first
// page in the paged print of the same paper, scale and @page rules, which
// the media features and viewport units of media read (CSS 2.1, 10.1; CSS
// Page 3, 3.1 and 5.1). The page the content is laid out on is as tall as
// a single page may be, so its own area cannot be the block: a root of
// height 100% or a box positioned at bottom 0 of the block would reach the
// bound. With the paged area, such a document prints on a page as tall as
// the paper and one without such a box as tall as its content, and one
// layout decides both.
func singleViewport(media css.Media) float64 {
	if media.ViewportHeight > 0 {
		return media.ViewportHeight
	}
	return media.Height
}

// singleCut reports whether the content of the single page res laid out
// passed the bound of a single page: nothing else ends the page, so a
// second page holds what the first could not.
func (p *prepared) singleCut(res *layout.Result) bool {
	return p.o.SinglePage && len(res.Pages) > 1
}
