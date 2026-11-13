package html

import (
	"context"
	"fmt"
	"slices"
	"strings"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxNamedStringBytes bounds the value of one named string: 4 KiB.
// Anti-DoS guard, not spec-mandated: a running header shows a title, and
// each value is laid out on every page that shows it.
const maxNamedStringBytes = 4 << 10

// maxAssignments bounds the named string assignments and running elements
// a document records: 65,536. Anti-DoS guard, not spec-mandated: a book
// sets a string per heading, a few hundred, and every page scans them.
const maxAssignments = 1 << 16

// assignment is one value a named string or a running element takes in
// document order (CSS GCPM 3, 1): its name, the anchor that says where it
// landed, its text or its content, and its lead, how far below the page
// area's top its anchor lands when its element starts the page.
type assignment struct {
	name, id string
	value    string
	node     *layout.Node
	lead     float64
	// paged reports a running element whose generated content reads the
	// page or pages counter.
	paged bool
}

// gcpmState holds the named strings and running elements of a document,
// which the page-margin boxes show, and the values the document's
// counters take along it, which the page-margin boxes read.
type gcpmState struct {
	strings, running []assignment
	// placed are the anchors of the last layout, by ID.
	placed map[string]typeset.Dest
	ids    int
	// paged holds the Text nodes of generated content that read the page
	// or pages counter.
	paged map[*layout.Node][]pagedText
	// track names the document counters a page-margin box reads, marks
	// their values along the document, and markAt is where the last
	// lookup of [gcpmState.countersAt] stopped.
	track  []string
	marks  []counterMark
	markAt int
}

// active reports a document with named strings, running elements or
// counter marks, whose anchors the page-margin boxes need.
func (g *gcpmState) active() bool {
	return len(g.strings) > 0 || len(g.running) > 0 || len(g.marks) > 0
}

// pagedText is a counter() or counters() of the page or pages counter that
// generated content wrote into a Text node, at bytes start to end: shown
// in a page-margin box, a running element reads the page's values (CSS
// GCPM 3, 1.2.1; CSS Page 3, 6.1).
type pagedText struct {
	start, end int
	item       css.ContentItem
}

// counterMark holds the values of the tracked counters just before and
// just after the element anchored at id runs its counter properties,
// recorded where they change.
type counterMark struct {
	id            string
	before, after [][]int
}

// maxCounterMarks bounds the counter marks of a document: 65,536.
// Anti-DoS guard, not spec-mandated: a mark is recorded where a counter a
// page-margin box reads changes, once per chapter or section of a book,
// and every page scans them.
const maxCounterMarks = 1 << 16

// gcpmID returns an anchor ID no element of the document carries.
func (b *builder) gcpmID() string {
	for {
		b.gcpm.ids++
		id := fmt.Sprintf("litho-gcpm-%d", b.gcpm.ids)
		if !b.scan.ids[id] {
			return id
		}
	}
}

// record adds the assignment a to list, false past [maxAssignments].
func (b *builder) record(list *[]assignment, a assignment) bool {
	if len(b.gcpm.strings)+len(b.gcpm.running) >= maxAssignments {
		b.warnOnce(warn.LimitReached, warn.Omitted, "gcpm", fmt.Sprintf("more than %d named strings and running elements; the others were left out", maxAssignments))
		return false
	}
	*list = append(*list, a)
	return true
}

// runAway takes the element n, built as node and styled st, out of the
// flow as a running element (CSS GCPM 3, 1.2.1): element() shows it in the
// page-margin boxes of the pages from the one where it would have been. It
// returns the empty inline box that keeps its place in the flow, anchored
// to tell its page.
func (b *builder) runAway(n *dom.Node, node *layout.Node, st *css.Style) *layout.Node {
	id := b.gcpmID()
	// The empty inline box that stays has no margin of its own.
	a := assignment{name: st.Running, id: id, node: node, lead: b.lead(n, 0), paged: b.gcpm.pagedIn(node, 0)}
	if !b.record(&b.gcpm.running, a) {
		return nil
	}
	node.ID = ""
	return &layout.Node{Kind: layout.Element, Style: anonInline(st), ID: id}
}

// setStrings records the named strings the element n sets, its node
// node, styled st, whose ::before and ::after nodes are before and after
// (CSS GCPM 3, 2.1.1): the values are built from the counters in scope on
// the element.
func (b *builder) setStrings(n *dom.Node, node *layout.Node, st *css.Style, before, after *layout.Node) {
	if node.ID == "" {
		node.ID = b.gcpmID()
	}
	own := 0.0
	if st.Display != css.Inline && st.Display != css.InlineBlock {
		own = max(resolveDim(st.Margin[css.Top], 0), 0)
	}
	lead := b.lead(n, own)
	for _, ns := range st.StringSet {
		var sb strings.Builder
		for _, it := range ns.Items {
			switch it.Kind {
			case css.ItemString:
				sb.WriteString(it.Text)
			case css.ItemAttr:
				if v, ok := attr(n, it.Text); ok {
					sb.WriteString(v)
				} else {
					sb.WriteString(it.Fallback)
				}
			case css.ItemCounter:
				v := 0
				if in := b.counters.innermost(it.Text); in != nil {
					v = in.value
				}
				sb.WriteString(counterText(b.counterStyles, it.Style, it.StyleName, v))
			case css.ItemCounters:
				for i, v := range b.counters.values(it.Text) {
					if i > 0 {
						sb.WriteString(it.Separator)
					}
					sb.WriteString(counterText(b.counterStyles, it.Style, it.StyleName, v))
				}
			case css.ItemContent:
				// content() collapses white space as white-space: normal
				// lays it out (CSS GCPM 3, 2.1.1.1).
				switch it.Text {
				case "before":
					sb.WriteString(collapse(nodeText(before)))
				case "after":
					sb.WriteString(collapse(nodeText(after)))
				case "first-letter":
					sb.WriteString(firstLetter(collapse(domText(n))))
				case "marker":
					if node.Marker != nil {
						sb.WriteString(collapse(nodeText(node.Marker)))
					}
				default:
					sb.WriteString(collapse(domText(n)))
				}
			case css.ItemOpenQuote, css.ItemCloseQuote, css.ItemNoOpenQuote, css.ItemNoCloseQuote,
				css.ItemImage, css.ItemLeader, css.ItemNamedString, css.ItemElement:
			}
			if sb.Len() > maxNamedStringBytes {
				break
			}
		}
		v := sb.String()
		if len(v) > maxNamedStringBytes {
			v = strings.ToValidUTF8(v[:maxNamedStringBytes], "")
		}
		if !b.record(&b.gcpm.strings, assignment{name: ns.Name, id: node.ID, value: v, lead: lead}) {
			return
		}
	}
}

// lead returns how far below the page area's top the box of the element n
// starts when it is the first element on its page, own being its own top
// margin: the top margins, borders and paddings of the ancestors whose
// first in-flow content it is lie above it too. Adjoining top margins
// collapse into the largest, a border or a padding keeps them apart, and
// the root element's margin collapses with none (CSS 2.1, 8.3.1); a forced
// break keeps the margins after it (CSS Fragmentation 3, 5.5).
func (b *builder) lead(n *dom.Node, own float64) float64 {
	total, adjoining := 0.0, own
	c := n
	for depth := 0; depth < maxTreeDepth && c.Parent != nil && b.opens(c); depth++ {
		p := c.Parent
		ps := b.computed.Style(p)
		if ps == nil {
			break
		}
		bp := usedBorder(ps.Border[css.Top]) + max(resolveDim(ps.Padding[css.Top], 0), 0)
		if bp > 0 || p.Parent == nil || p.Parent.Type == dom.DocumentNode {
			total += adjoining + bp
			adjoining = 0
		}
		adjoining = max(adjoining, resolveDim(ps.Margin[css.Top], 0))
		c = p
	}
	return total + adjoining
}

// opens reports whether the element c is the first in-flow content of its
// parent: no text but white space, no in-flow element and no ::before
// box comes before it (CSS 2.1, 9.3).
func (b *builder) opens(c *dom.Node) bool {
	if ps := b.computed.Pseudo(c.Parent, css.PseudoBefore); ps != nil && ps.Display != css.DisplayNone && ps.Content.Kind == css.ContentList {
		return false
	}
	for s := c.PrevSibling; s != nil; s = s.PrevSibling {
		switch s.Type { //nolint:exhaustive // comments and the rest make no box
		case dom.TextNode:
			if strings.TrimSpace(s.Data) != "" {
				return false
			}
		case dom.ElementNode:
			st := b.computed.Style(s)
			if st == nil || st.Display == css.DisplayNone || st.Float != css.FloatNone ||
				st.Position == css.Absolute || st.Position == css.Fixed || st.Running != "" || st.Footnote || st.Note != "" {
				continue
			}
			return false
		}
	}
	return true
}

// recordPaged records that generated content wrote the page counter items
// segs into the Text node n.
func (g *gcpmState) recordPaged(n *layout.Node, segs []pagedText) {
	if g.paged == nil {
		g.paged = map[*layout.Node][]pagedText{}
	}
	g.paged[n] = segs
}

// pagedIn reports whether the subtree of n, at depth, holds a Text node
// that reads the page or pages counter.
func (g *gcpmState) pagedIn(n *layout.Node, depth int) bool {
	if n == nil || len(g.paged) == 0 || depth >= maxTreeDepth {
		return false
	}
	if _, ok := g.paged[n]; ok {
		return true
	}
	if g.pagedIn(n.Marker, depth+1) {
		return true
	}
	for _, k := range n.Children {
		if g.pagedIn(k, depth+1) {
			return true
		}
	}
	return false
}

// atPage returns a copy of the subtree of n, at depth, whose page counter
// items say what text gives them on the page a page-margin box shows it
// on.
func (g *gcpmState) atPage(n *layout.Node, text func(css.ContentItem) string, depth int) *layout.Node {
	if n == nil || depth >= maxTreeDepth {
		return n
	}
	c := *n
	if segs, ok := g.paged[n]; ok {
		var sb strings.Builder
		at := 0
		for _, sg := range segs {
			sb.WriteString(n.Text[at:sg.start])
			sb.WriteString(text(sg.item))
			at = sg.end
		}
		sb.WriteString(n.Text[at:])
		c.Text = sb.String()
	}
	c.Marker = g.atPage(n.Marker, text, depth+1)
	if len(n.Children) > 0 {
		c.Children = make([]*layout.Node, len(n.Children))
		for i, k := range n.Children {
			c.Children[i] = g.atPage(k, text, depth+1)
		}
	}
	return &c
}

// trackedValues returns the values of the counters a page-margin box
// reads, in the order of [gcpmState.track].
func (b *builder) trackedValues() [][]int {
	vals := make([][]int, len(b.gcpm.track))
	for i, name := range b.gcpm.track {
		vals[i] = b.counters.values(name)
	}
	return vals
}

// markCounters records the values before, before the element n styled st
// ran its counter properties, and after it, when the element changed them
// or something since the last mark did: a page-margin box shows them as an
// element at the start of the page would see them (CSS Page 3, 6.1). The
// mark anchors at n; an element whose box gets no anchor where it stands
// in the flow, out of the flow or replaced, is left unmarked, and the next
// element records what changed.
func (b *builder) markCounters(n *dom.Node, st *css.Style, before [][]int) {
	g := &b.gcpm
	if len(g.marks) >= maxCounterMarks || n.Namespace != "" || st.Float != css.FloatNone ||
		st.Position == css.Absolute || st.Position == css.Fixed || st.Running != "" || st.Footnote || st.Note != "" {
		return
	}
	switch n.DataAtom { //nolint:exhaustive // every other element gets its node and its anchor
	case atom.Br, atom.Wbr, atom.Img, atom.Input, atom.Select, atom.Textarea, atom.Object, atom.Canvas:
		return
	}
	after := b.trackedValues()
	var last [][]int
	if k := len(g.marks); k > 0 {
		last = g.marks[k-1].after
	} else {
		last = make([][]int, len(g.track))
	}
	if slices.EqualFunc(before, last, slices.Equal) && slices.EqualFunc(after, before, slices.Equal) {
		return
	}
	id, ok := b.ids[n]
	if !ok {
		id, _ = attr(n, "id")
	}
	if id == "" {
		if n.DataAtom == atom.A {
			// A named anchor's name is its anchor.
			return
		}
		id = b.gcpmID()
		if b.ids == nil {
			b.ids = map[*dom.Node]string{}
		}
		b.ids[n] = id
	}
	g.marks = append(g.marks, counterMark{id: id, before: before, after: after})
}

// countersAt returns the values of the tracked counter of index i at the
// start of page index: those before the first mark on that page or a
// later one, those after the last mark past them. Pages come in
// order, so the scan resumes where the last one stopped.
func (g *gcpmState) countersAt(i, index int) []int {
	if len(g.marks) == 0 {
		return nil
	}
	if g.markAt > 0 {
		if d, ok := g.placed[g.marks[g.markAt-1].id]; ok && d.Page-1 >= index {
			g.markAt = 0
		}
	}
	for ; g.markAt < len(g.marks); g.markAt++ {
		if d, ok := g.placed[g.marks[g.markAt].id]; ok && d.Page-1 >= index {
			return g.marks[g.markAt].before[i]
		}
	}
	return g.marks[len(g.marks)-1].after[i]
}

// nodeText returns the text of the Text nodes under n.
func nodeText(n *layout.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	stack := []*layout.Node{n}
	for len(stack) > 0 && sb.Len() <= maxNamedStringBytes {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if c.Kind == layout.Text {
			sb.WriteString(c.Text)
		}
		for _, v := range slices.Backward(c.Children) {
			stack = append(stack, v)
		}
	}
	return sb.String()
}

// domText returns the text content of the element n (DOM Standard,
// 4.4, textContent), past [maxNamedStringBytes] cut.
func domText(n *dom.Node) string {
	var sb strings.Builder
	stack := []*dom.Node{n}
	for len(stack) > 0 && sb.Len() <= maxNamedStringBytes {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if c.Type == dom.TextNode {
			sb.WriteString(c.Data)
		}
		var kids []*dom.Node
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			kids = append(kids, k)
		}
		for _, kid := range slices.Backward(kids) {
			stack = append(stack, kid)
		}
	}
	return sb.String()
}

// firstLetter returns the first letter of s as ::first-letter takes it
// (CSS GCPM 3, 2.1.1.1, content(first-letter); see [firstLetterSpan]):
// the punctuation alone when no letter follows it, "" for white space.
func firstLetter(s string) string {
	pre, n, _ := firstLetterSpan(s)
	if n < 0 {
		return ""
	}
	return s[pre:n]
}

// pick returns the assignment of name in list that a page-margin box of
// page index shows under p (CSS GCPM 3, 1.1.2 for string(), 1.2.2 for
// element()), and false when nothing applies:
//
//   - first: the first assignment on the page, else the entry value, the
//     one in effect at the end of the previous page;
//   - start: the first assignment when its element is the first element on
//     the page, else the entry value;
//   - last: the exit value, the one in effect at the end of the page;
//   - first-except: the empty string on a page that assigns, else the
//     entry value.
//
// top is the page area's top on the page: an element is the first on its
// page when its anchor lands no lower than its lead below it.
func (g *gcpmState) pick(list []assignment, name string, p css.PagePick, index int, top float64) (assignment, bool) {
	var entry, first, last assignment
	hasEntry, onPage, firstAtTop := false, false, false
	for _, a := range list {
		if a.name != name {
			continue
		}
		d, ok := g.placed[a.id]
		if !ok {
			continue
		}
		switch page := d.Page - 1; {
		case page < index:
			entry, hasEntry = a, true
		case page == index:
			if !onPage {
				first, firstAtTop = a, d.Y <= top+a.lead+pageTopSlack
			}
			last, onPage = a, true
		}
	}
	switch p {
	case css.PickStart:
		if onPage && firstAtTop {
			return first, true
		}
	case css.PickLast:
		if onPage {
			return last, true
		}
	case css.PickFirstExcept:
		if onPage {
			return assignment{}, false
		}
	case css.PickFirst, css.PickAllOnce:
		if onPage {
			return first, true
		}
	}
	return entry, hasEntry
}

// footnoteCounter is the counter that numbers footnotes (CSS GCPM 3,
// 3.5).
const footnoteCounter = "footnote"

// footnote turns the element n, built as node and styled st, into a
// footnote (CSS GCPM 3, 3.2): the ::footnote-call it leaves in the flow,
// which carries its body, the element as footnote-display lays it out with
// its ::footnote-marker first (CSS GCPM 3, 3.3, 3.6 and 3.7). A call
// without content still anchors the body.
func (b *builder) footnote(ctx context.Context, n *dom.Node, node *layout.Node, st *css.Style) *layout.Node {
	bs := *st
	bs.Display = css.Block
	if st.FootnoteDisplay == css.FootnoteInline {
		bs.Display = css.Inline
	}
	node.Style = &bs
	if m := b.pseudo(ctx, n, css.PseudoFootnoteMarker); m != nil {
		node.Children = append([]*layout.Node{m}, node.Children...)
	}
	if node.Role == "" || node.Role == "Span" || node.Role == "P" {
		node.Role = "Note"
	}
	call := b.pseudo(ctx, n, css.PseudoFootnoteCall)
	if call == nil {
		call = &layout.Node{Kind: layout.Element, Style: anonInline(st)}
	} else if call.Style.Display != css.Inline {
		cs := *call.Style
		cs.Display = css.Inline
		call.Style = &cs
	}
	call.Footnote = node
	return call
}

// pageTopSlack is how far below the page area's top an element still
// starts the page, for string(name, start): a hundredth of a point.
const pageTopSlack = 0.01

// noteCounter is the counter that numbers notes.
const noteCounter = "note"

// note turns the element n, built as node and styled st, into a note
// (position: note(), the notes WeasyPrint draws): the ::note-call it
// leaves in the flow carries its body, the element with its ::note-marker
// first and its ::note-callback last, which the layout moves to the note
// area of the page the call lands on. A call without content still
// anchors the body.
func (b *builder) note(ctx context.Context, n *dom.Node, node *layout.Node, st *css.Style) *layout.Node {
	if m := b.pseudo(ctx, n, css.PseudoNoteMarker); m != nil {
		node.Children = append([]*layout.Node{m}, node.Children...)
	}
	if cb := b.pseudo(ctx, n, css.PseudoNoteCallback); cb != nil {
		node.Children = append(node.Children, cb)
	}
	if node.Role == "" || node.Role == "Span" || node.Role == "P" {
		node.Role = "Note"
	}
	call := b.pseudo(ctx, n, css.PseudoNoteCall)
	if call == nil {
		call = &layout.Node{Kind: layout.Element, Style: anonInline(st)}
	} else if call.Style.Display != css.Inline {
		cs := *call.Style
		cs.Display = css.Inline
		call.Style = &cs
	}
	call.Footnote = node
	return call
}
