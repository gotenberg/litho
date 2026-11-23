package slides

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// statedLang returns the language most of the text of doc's slides and
// notes states: the Lang of each run, the language whose runs count the
// most characters winning, a tie going to the language stated first; ""
// when no run states one. A presentation whose metadata and default text
// style state no language takes it, so that its text is in a language a
// reader can determine (ISO 14289-1, 7.2) and only the runs in another
// one need a Span stating theirs.
func statedLang(doc *Document) string {
	counts := map[string]int{}
	var order []string
	count := func(ps []Paragraph) {
		for _, p := range ps {
			for _, r := range p.Runs {
				if r.Style.Lang == "" {
					continue
				}
				key := strings.ToLower(r.Style.Lang)
				if _, ok := counts[key]; !ok {
					order = append(order, r.Style.Lang)
				}
				counts[key] += utf8.RuneCountInString(r.Text)
			}
		}
	}
	for _, s := range doc.Slides {
		if s == nil {
			continue
		}
		// A stack read in document order: kids pushed last to first.
		stack := slices.Clone(s.Shapes)
		slices.Reverse(stack)
		for len(stack) > 0 {
			sh := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if sh == nil {
				continue
			}
			if sh.Text != nil {
				count(sh.Text.Paragraphs)
			}
			for i := len(sh.Kids) - 1; i >= 0; i-- {
				stack = append(stack, sh.Kids[i])
			}
		}
		count(s.Notes)
	}
	best, most := "", 0
	for _, l := range order {
		if c := counts[strings.ToLower(l)]; c > most {
			best, most = l, c
		}
	}
	return best
}
