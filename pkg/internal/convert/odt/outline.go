package odt

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxOutlineTitleRunes bounds an outline entry's title: 256 characters.
// Anti-DoS guard, not spec-mandated: a heading's title is a line.
const maxOutlineTitleRunes = 256

// outline returns the bookmark tree of the headings, each under the
// closest preceding heading of a lower outline level (ODF 1.3, Part 3,
// 5.1.2), dest mapping where each landed to the kept pages. A heading
// that landed on no kept page, or draws no text, is left out.
func outline(headings []heading, anchors map[string]typeset.Dest, dest func(typeset.Dest) (typeset.Dest, bool)) []typeset.Outline {
	type entry struct {
		o     typeset.Outline
		level int
		kids  []*entry
	}
	var roots, stack []*entry
	for _, h := range headings {
		d, ok := anchors[h.node.ID]
		if !ok {
			continue
		}
		if d, ok = dest(d); !ok {
			continue
		}
		title := headingTitle(h.node)
		if title == "" {
			continue
		}
		if m := h.node.Marker; m != nil {
			// A heading in a list is titled with its list label first, as
			// Writer exports its bookmark.
			if lbl := headingTitle(m); lbl != "" {
				title = lbl + " " + title
			}
		}
		e := &entry{o: typeset.Outline{Title: title, Dest: d}, level: h.level}
		for len(stack) > 0 && stack[len(stack)-1].level >= h.level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, e)
		} else {
			top := stack[len(stack)-1]
			top.kids = append(top.kids, e)
		}
		stack = append(stack, e)
	}
	var flatten func([]*entry) []typeset.Outline
	flatten = func(es []*entry) []typeset.Outline {
		if len(es) == 0 {
			return nil
		}
		out := make([]typeset.Outline, len(es))
		for i, e := range es {
			out[i] = e.o
			out[i].Kids = flatten(e.kids)
		}
		return out
	}
	return flatten(roots)
}

// headingTitle returns the text a heading draws, white space collapsed,
// at most [maxOutlineTitleRunes] characters.
func headingTitle(n *layout.Node) string {
	var b strings.Builder
	stack := []*layout.Node{n}
	for len(stack) > 0 && b.Len() < 4*maxOutlineTitleRunes {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch cur.Kind {
		case layout.Text:
			b.WriteString(cur.Text)
		case layout.LineBreak:
			b.WriteByte(' ')
		case layout.Replaced:
			b.WriteString(cur.Alt)
		case layout.Element:
			for _, v := range slices.Backward(cur.Children) {
				stack = append(stack, v)
			}
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(s) > maxOutlineTitleRunes {
		s = strings.TrimSpace(string([]rune(s)[:maxOutlineTitleRunes]))
	}
	return s
}
