package layout

import "github.com/gotenberg/litho/pkg/internal/css"

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
// flow (CSS Page 3, 4.1; CSS Fragmentation 3, 4.1).
func pageApplies(b *box) bool {
	return !b.inline && !b.abs && b.float == css.FloatNone && b.kind == kindBlock &&
		(b.parent == nil || b.parent.tpart == partNone || b.parent.tpart == partWrapper)
}

// flowKids returns the in-flow children of b the page property applies
// to, nil when b lays its content out as lines or as a table.
func flowKids(b *box) []*box {
	if b.inlineKids || b.tpart != partNone && b.tpart != partWrapper {
		return nil
	}
	return b.kids
}

// pageStart returns the start page value of b: the one its first child
// the page property applies to propagates, else its used value (CSS Page
// 3, 4.1).
func pageStart(b *box) string {
	for depth := 0; depth < maxTreeDepth+1; depth++ {
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
	for depth := 0; depth < maxTreeDepth+1; depth++ {
		var next *box
		kids := flowKids(b)
		for i := len(kids) - 1; i >= 0; i-- {
			if pageApplies(kids[i]) {
				next = kids[i]
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
// first page, and its blankness.
func (e *engine) infoOf(p int) PageInfo {
	info := PageInfo{Index: p}
	for i := min(p, len(e.marks)-1); i >= 0; i-- {
		if m := e.marks[i]; m.set {
			info.Name = m.name
			info.Blank = i == p && m.blank
			return info
		}
	}
	return info
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
