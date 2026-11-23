package layout

import (
	"strings"
	"unicode/utf8"
)

// StatedLang returns the language most of the text under root states: the
// language of each Text node is the Lang of the node itself or of its
// nearest ancestor that has one, and the language whose text counts the
// most characters wins, a tie going to the language stated first. Text in
// no stated language counts for none. "" when no text states one.
//
// A converter whose source states no document language takes this one, so
// that the text of the document is in a language a reader can determine
// (ISO 14289-1, 7.2) with the least structure: the runs in it need no Span
// of their own, only those that differ do.
func StatedLang(root *Node) string {
	type frame struct {
		n    *Node
		lang string
	}
	counts := map[string]int{}
	var order []string
	stack := []frame{{root, ""}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.n == nil {
			continue
		}
		lang := f.lang
		if f.n.Lang != "" {
			lang = f.n.Lang
		}
		if f.n.Kind == Text && lang != "" {
			key := strings.ToLower(lang)
			if _, ok := counts[key]; !ok {
				order = append(order, lang)
			}
			counts[key] += utf8.RuneCountInString(f.n.Text)
		}
		for i := len(f.n.Children) - 1; i >= 0; i-- {
			stack = append(stack, frame{f.n.Children[i], lang})
		}
	}
	best, most := "", 0
	for _, l := range order {
		if c := counts[strings.ToLower(l)]; c > most {
			best, most = l, c
		}
	}
	return best
}
