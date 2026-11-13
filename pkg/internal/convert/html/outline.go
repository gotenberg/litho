package html

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// outline returns the bookmark tree of the elements with a bookmark-level,
// in document order, each nested under the closest preceding one of a
// lower level (CSS Generated Content 3, 8.1), as the h1 to h6 rank
// headings (HTML Standard, 4.3.11, heading levels). An entry's title is
// its bookmark-label, or, for the initial content(text), the text its
// box draws, generated content and image alternative text included; an
// entry that landed on no page or whose title is empty is left out.
func outline(headings []heading, anchors map[string]typeset.Dest) []typeset.Outline {
	type entry struct {
		o     typeset.Outline
		level int
		kids  []*entry
	}
	var roots, stack []*entry
	for _, h := range headings {
		dest, ok := anchors[h.node.ID]
		if !ok {
			continue
		}
		title := headingTitle(h.node)
		if h.labelled {
			title = clipTitle(collapse(h.label))
		}
		if title == "" {
			continue
		}
		e := &entry{o: typeset.Outline{Title: title, Dest: dest, Closed: h.closed}, level: h.level}
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
	// The tree is at most maxBookmarkLevel levels deep.
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

// headingTitle returns the text a heading's subtree draws, whitespace
// collapsed, an image standing for its alt text, at most
// [maxOutlineTitleRunes] characters.
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
	return clipTitle(collapse(strings.ReplaceAll(b.String(), "\u200b", "")))
}

// clipTitle returns s cut to [maxOutlineTitleRunes] characters.
func clipTitle(s string) string {
	if utf8.RuneCountInString(s) > maxOutlineTitleRunes {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:maxOutlineTitleRunes]))
	}
	return s
}
