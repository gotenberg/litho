package layout

import (
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// pageMark is what a forced break decided about the page it starts: its
// page type name, and whether the page before it is blank.
type pageMark struct {
	set   bool
	name  string
	blank bool
	// owner is the box whose break set the mark, so laying it out again
	// moves its mark.
	owner *box
}

// usedPage returns the used value of the page property of b: its own
// name, else the nearest ancestor's, "" when none names a page (CSS Page
// 3, 4.1: auto values resolve against non-auto ancestors).
func usedPage(b *box) string {
	for ; b != nil; b = b.parent {
		if b.st != nil && b.st.Page != "" {
			return b.st.Page
		}
	}
	return ""
}

// pageApplies reports a box the page property applies to: one that creates
// class A break points, an in-flow block-level box of a block container's
// flow (CSS Page 3, 4.1; CSS Fragmentation 3, 4.1). An anonymous block of
// collapsible white space alone is none: its line boxes do not exist
// (CSS 2.1, 9.4.2), so the white space between the blocks of a source
// does not end the walk to a start page value.
func pageApplies(b *box) bool {
	return !b.inline && !b.abs && b.float == css.FloatNone && b.kind == kindBlock &&
		(b.parent == nil || b.parent.tpart == partNone || b.parent.tpart == partWrapper) &&
		(!b.anon || !b.inlineKids || !blankInline(b.kids, 0))
}

// blankInline reports inline content, at depth, that makes no line box
// exist (CSS 2.1, 9.4.2): text of collapsible white space, inline boxes
// without content, margins, borders or paddings, and boxes out of flow,
// floats and absolutely positioned boxes, which are no in-flow content.
//
// # Corpus evidence
//
//   - wpt, css/css-page/page-name-propagated-006-print.html: Chromium
//     prints an absolutely positioned box that follows the last block of
//     a named page, among white space, on that block's page.
func blankInline(kids []*box, depth int) bool {
	if depth > maxTreeDepth {
		return false
	}
	for _, k := range kids {
		if k.abs || k.float != css.FloatNone {
			continue
		}
		switch k.kind { //nolint:exhaustive // any other kind of box makes a line box exist
		case kindText:
			if k.marker || k.st == nil || !collapses(k.st.WhiteSpace) || strings.Trim(k.text, " \t\n\r\f") != "" {
				return false
			}
		case kindInline:
			st := k.st
			if st == nil {
				return false
			}
			for _, side := range [...]css.Side{css.Left, css.Right} {
				bd := st.Border[side]
				if st.Margin[side].Value != 0 || st.Padding[side].Value != 0 || bd.Style != css.BorderNone && bd.Style != css.BorderHidden && bd.Width != 0 {
					return false
				}
			}
			if !blankInline(k.kids, depth+1) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// flowKids returns the in-flow children of b the page property applies
// to, nil when b lays its content out as lines, as a table, or as a flex
// or grid container, whose items create no class A break points (CSS Page
// 3, 4.1): no start or end page value propagates out of them.
func flowKids(b *box) []*box {
	if b.inlineKids || b.tpart != partNone && b.tpart != partWrapper || b.flex != nil || b.grid != nil {
		return nil
	}
	return b.kids
}

// pageStart returns the start page value of b: the one its first child
// the page property applies to propagates, else its used value (CSS Page
// 3, 4.1).
func pageStart(b *box) string {
	for range maxTreeDepth + 1 {
		var next *box
		for _, k := range flowKids(b) {
			if pageApplies(k) {
				next = k
				break
			}
		}
		if next == nil {
			break
		}
		b = next
	}
	return usedPage(b)
}

// pageEnd returns the end page value of b, from its last child as
// [pageStart] does from its first.
func pageEnd(b *box) string {
	for range maxTreeDepth + 1 {
		var next *box
		kids := flowKids(b)
		for _, kid := range slices.Backward(kids) {
			if pageApplies(kid) {
				next = kid
				break
			}
		}
		if next == nil {
			break
		}
		b = next
	}
	return usedPage(b)
}

// namesDiffer reports siblings a and b meeting at a class A break point
// whose page values differ, which forces a page break between them (CSS
// Page 3, 4.1).
func namesDiffer(a, b *box) bool {
	return pageApplies(a) && pageApplies(b) && pageEnd(a) != pageStart(b)
}

// mark records that the forced break before owner starts page p on a page
// named name, the page before it blank, of the same name, when blank is
// set (CSS Page 3, 4.2.3). It forgets the marks owner set before and every
// mark past p, which the layout sets again as it reaches them, and
// resolves again the page boxes the change affects.
func (e *engine) mark(p int, name string, blank bool, owner *box) {
	if p < 0 || p > e.maxPages || e.cols != nil {
		return
	}
	for i := range e.marks {
		if i > p || e.marks[i].owner == owner {
			e.marks[i] = pageMark{}
		}
	}
	for len(e.marks) <= p {
		e.marks = append(e.marks, pageMark{})
	}
	e.marks[p] = pageMark{set: true, name: name, owner: owner}
	if blank && p > 0 {
		e.marks[p-1] = pageMark{set: true, name: name, blank: true, owner: owner}
	}
	e.geom.resync()
}

// infoOf returns the page p as the forced breaks decided it: the name of
// the closest mark at or before it, the root's start page value for the
// first page, its blankness, and the page groups it belongs to.
func (e *engine) infoOf(p int) PageInfo {
	info := PageInfo{Index: p, Groups: e.groupsOf(p)}
	for i := min(p, len(e.marks)-1); i >= 0; i-- {
		if m := e.marks[i]; m.set {
			info.Name = m.name
			info.Blank = i == p && m.blank
			return info
		}
	}
	return info
}

// pageGroup is a page group (CSS GCPM 3, 3.2): the box that makes it, its
// page type name, and the pages its box starts and ends on, end -1 while
// the box is being laid out.
type pageGroup struct {
	owner     *box
	name      string
	page, end int
}

// maxPageGroups bounds the page groups of a layout: 4,096. Anti-DoS guard,
// not spec-mandated: a group starts a page, a book has a few hundred
// chapters, and every page's info scans the groups.
const maxPageGroups = 4096

// startsGroup reports a box that makes a page group (CSS GCPM 3, 3.2): an
// in-flow block-level box whose page property names a page and whose
// break-before forces a page break.
func startsGroup(b *box) bool {
	return b.st != nil && b.st.Page != "" && isForced(b.st.BreakBefore) && pageApplies(b)
}

// startGroup records the page group of b, whose box starts on page p. It
// forgets the group b made before and every group past p, which the layout
// makes again as it reaches them, as [engine.mark] forgets marks.
func (e *engine) startGroup(b *box, p int) {
	kept := e.groups[:0]
	for _, g := range e.groups {
		if g.owner != b && g.page <= p {
			kept = append(kept, g)
		}
	}
	e.groups = kept
	if len(e.groups) < maxPageGroups {
		e.groups = append(e.groups, pageGroup{owner: b, name: b.st.Page, page: p, end: -1})
	}
	e.geom.resync()
}

// endGroup records the page the box of b's page group ends on.
func (e *engine) endGroup(b *box) {
	for i := range e.groups {
		if g := &e.groups[i]; g.owner == b {
			g.end = max(e.pageOf(max(b.y+b.borderH()-1e-3, b.y, 0)), g.page)
			e.geom.resync()
			return
		}
	}
}

// groupsOf returns the page groups page p belongs to: those whose box
// spans it, unless a forced break before a box outside the group's box
// starts a page after the group's first and at or before p.
func (e *engine) groupsOf(p int) []css.PageGroup {
	var out []css.PageGroup
	for _, g := range e.groups {
		if g.page > p || g.end >= 0 && p > g.end || e.leftGroup(g, p) {
			continue
		}
		out = append(out, css.PageGroup{Name: g.name, Index: p - g.page + 1})
	}
	return out
}

// leftGroup reports a forced break before a box outside g's box that
// starts a page after g's first and at or before p.
func (e *engine) leftGroup(g pageGroup, p int) bool {
	for i := g.page + 1; i <= p && i < len(e.marks); i++ {
		if m := e.marks[i]; m.set && !inSubtree(m.owner, g.owner) {
			return true
		}
	}
	return false
}

// inSubtree reports whether b is anc or one of its descendants.
func inSubtree(b, anc *box) bool {
	for depth := 0; b != nil && depth <= maxTreeDepth; depth++ {
		if b == anc {
			return true
		}
		b = b.parent
	}
	return false
}

// widthDelta returns how much wider the area of page p is than the first
// page's, the width the flow is laid out at.
func (e *engine) widthDelta(p int) float64 {
	if !e.geom.varies() {
		return 0
	}
	w, _ := areaOf(e.box(p))
	return w - e.pageW
}

// startPage returns the page a box placed at flow position y starts on,
// the next one when mode forces a break there.
func (e *engine) startPage(y float64, mode breakMode) int {
	p := e.pageOf(max(y, 0))
	if mode != breakNone && !e.atPageTop(y) {
		p++
	}
	return p
}

// followsPages reports a box whose width follows the page area's: the root
// and the block boxes in flow of auto width under it, each as wide as the
// page area less its margins, borders and padding.
func followsPages(b *box) bool {
	for ; b != nil; b = b.parent {
		if b.inline || b.abs || b.float != css.FloatNone || b.kind != kindBlock || b.tpart != partNone || b.st.Width.Kind != css.Auto {
			return false
		}
	}
	return true
}
