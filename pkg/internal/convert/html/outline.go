package html

import (
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// outline returns the bookmark tree of the headings, in document order,
// each nested under the closest preceding heading of a higher rank (HTML
// Standard, 4.3.11, heading levels). A heading that landed on no page or
// draws no text is left out.
func outline(headings []heading, anchors map[string]compose.Dest) []compose.Outline {
	type entry struct {
		o     compose.Outline
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
		if title == "" {
			continue
		}
		e := &entry{o: compose.Outline{Title: title, Dest: dest}, level: h.level}
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
	// The tree is at most six levels deep, one a heading rank.
	var flatten func([]*entry) []compose.Outline
	flatten = func(es []*entry) []compose.Outline {
		if len(es) == 0 {
			return nil
		}
		out := make([]compose.Outline, len(es))
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
			for i := len(cur.Children) - 1; i >= 0; i-- {
				stack = append(stack, cur.Children[i])
			}
		}
	}
	s := collapse(strings.ReplaceAll(b.String(), "\u200b", ""))
	if utf8.RuneCountInString(s) > maxOutlineTitleRunes {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:maxOutlineTitleRunes]))
	}
	return s
}
