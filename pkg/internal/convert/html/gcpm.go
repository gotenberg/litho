package html

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
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
// document order (CSS GCPM 3, 2): its name, the anchor that says where it
// landed, and its text or its content.
type assignment struct {
	name, id string
	value    string
	node     *layout.Node
}

// gcpmState holds the named strings and running elements of a document,
// which the page-margin boxes show.
type gcpmState struct {
	strings, running []assignment
	// placed are the anchors of the last layout, by ID.
	placed map[string]compose.Dest
	ids    int
}

// active reports a document with named strings or running elements.
func (g *gcpmState) active() bool { return len(g.strings) > 0 || len(g.running) > 0 }

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
		b.warnOnce(CodeGeneratedLimit, "gcpm", fmt.Sprintf("more than %d named strings and running elements; the others were left out", maxAssignments))
		return false
	}
	*list = append(*list, a)
	return true
}

// runAway takes the element node styled st out of the flow as a running
// element (CSS GCPM 3, 2.2.1): element() shows it in the page-margin boxes
// of the pages from the one where it would have been. It returns the empty
// inline box that keeps its place in the flow, anchored to tell its page.
func (b *builder) runAway(node *layout.Node, st *css.Style) *layout.Node {
	id := b.gcpmID()
	if !b.record(&b.gcpm.running, assignment{name: st.Running, id: id, node: node}) {
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
				sb.WriteString(layout.CounterText(it.Style, v))
			case css.ItemCounters:
				for i, v := range b.counters.values(it.Text) {
					if i > 0 {
						sb.WriteString(it.Separator)
					}
					sb.WriteString(layout.CounterText(it.Style, v))
				}
			case css.ItemContent:
				switch it.Text {
				case "before":
					sb.WriteString(collapseSpace(nodeText(before)))
				case "after":
					sb.WriteString(collapseSpace(nodeText(after)))
				case "first-letter":
					sb.WriteString(firstLetter(collapseSpace(domText(n))))
				case "marker":
					if node.Marker != nil {
						sb.WriteString(collapseSpace(nodeText(node.Marker)))
					}
				default:
					sb.WriteString(collapseSpace(domText(n)))
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
		if !b.record(&b.gcpm.strings, assignment{name: ns.Name, id: node.ID, value: v}) {
			return
		}
	}
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
		for i := len(c.Children) - 1; i >= 0; i-- {
			stack = append(stack, c.Children[i])
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
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, kids[i])
		}
	}
	return sb.String()
}

// collapseSpace returns s as white-space: normal lays it out: each run of
// white space one space, none at either end (CSS GCPM 3, 2.1.1.1).
func collapseSpace(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	}), " ")
}

// firstLetter returns the first letter of s with the punctuation before
// it (CSS Pseudo 4, 2.4, the first typographic letter unit).
func firstLetter(s string) string {
	for i, r := range s {
		if !unicode.IsPunct(r) {
			return s[:i+utf8.RuneLen(r)]
		}
	}
	return s
}

// pick returns the assignment of name in list that a page-margin box of
// page index shows under pick (CSS GCPM 3, 2.1.2): top is the page area's
// top on the page, for start. ok is false when nothing applies.
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
				first, firstAtTop = a, d.Y <= top+pageTopSlack
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
	case css.PickFirst:
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
