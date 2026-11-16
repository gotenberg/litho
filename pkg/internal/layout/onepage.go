package layout

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// onePageCheck is how many nodes [OnePage] visits between two checks of
// its context.
const onePageCheck = 1024

// OnePage readies the tree under root to be laid out on one page as tall
// as its content ([Options.FitFirstPage]), where nothing ends a page: a
// forced page break, to any page, and a break a box avoids become auto
// (CSS Fragmentation 3, 3.1: a forced break only breaks a fragmented
// flow); every box takes the unnamed page, since a change of page name
// forces a break (CSS Page 3, 4.1); a multi-column container whose columns
// fill in turn balances them instead, as no page height ends a column
// (CSS Multi-column 1, 7.1); and a frame anchored to a numbered page is
// anchored to the page of its anchor. Column breaks stay. Styles shared by
// several nodes stay shared: each is copied once. It returns ctx's error
// when ctx ends first.
func OnePage(ctx context.Context, root *Node) error {
	done := map[*css.Style]*css.Style{}
	stack := []*Node{root}
	for visited := 0; len(stack) > 0; visited++ {
		if visited%onePageCheck == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if st := n.Style; st != nil && paged(st) {
			c, ok := done[st]
			if !ok {
				cp := *st
				cp.BreakBefore, cp.BreakAfter = unpaged(cp.BreakBefore), unpaged(cp.BreakAfter)
				if cp.BreakInside == css.InsideAvoidPage {
					cp.BreakInside = css.InsideAuto
				}
				cp.Page = ""
				if cp.Columns.Fill == css.FillAuto {
					cp.Columns.Fill = css.FillBalance
				}
				cp.AnchorPage = 0
				c = &cp
				done[st] = c
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

// paged reports a style that names something [OnePage] undoes.
func paged(st *css.Style) bool {
	return unpaged(st.BreakBefore) != st.BreakBefore || unpaged(st.BreakAfter) != st.BreakAfter ||
		st.BreakInside == css.InsideAvoidPage || st.Page != "" || st.Columns.Fill == css.FillAuto || st.AnchorPage != 0
}

// unpaged returns the break value b takes on a page nothing ends: auto for
// a forced or avoided page break, b for a column break or auto.
func unpaged(b css.Break) css.Break {
	switch b {
	case css.Page, css.LeftPage, css.RightPage, css.BreakAvoidPage:
		return css.BreakAuto
	case css.BreakAuto, css.BreakAvoid, css.Column, css.BreakAvoidColumn:
		return b
	}
	return b
}
