package docx

import (
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// maxOutlineTitleRunes caps an outline entry's title. Anti-DoS guard, not
// spec-mandated: a heading's text is a line.
const maxOutlineTitleRunes = 512

// outline returns the bookmark tree of the headings, in document order,
// each nested under the closest preceding heading of a higher level: the
// outline levels of the paragraphs (17.3.1.20), as LibreOffice exports
// them. A heading that landed on no page or draws no text is left out.
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
	// The tree is at most nine levels deep, one an outline level.
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

// headingTitle returns the text a heading's subtree draws, its marker's
// label first, whitespace collapsed, at most [maxOutlineTitleRunes]
// characters.
func headingTitle(n *layout.Node) string {
	var b strings.Builder
	stack := []*layout.Node{n}
	if n.Marker != nil {
		stack = append(stack, n.Marker)
	}
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
			if cur.Footnote != nil || cur != n && (cur.Style.Float != css.FloatNone || cur.Style.Position == css.Absolute) {
				// A note's body and an anchored drawing's text are not
				// the heading's title, as LibreOffice bookmarks it.
				continue
			}
			for i := len(cur.Children) - 1; i >= 0; i-- {
				stack = append(stack, cur.Children[i])
			}
		}
	}
	s := strings.Join(strings.Fields(strings.NewReplacer("\u200b", "", "\u00a0", " ").Replace(b.String())), " ")
	if utf8.RuneCountInString(s) > maxOutlineTitleRunes {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:maxOutlineTitleRunes]))
	}
	return s
}
