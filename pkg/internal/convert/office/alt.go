package office

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// MaxAltText caps the bytes of the text a Figure's alt repeats.
// Anti-DoS guard, not spec-mandated: a text box shows a few paragraphs,
// and a reader is told the first few thousand characters of a longer
// one.
const MaxAltText = 4096

// ShownText returns the text of nodes, its white space collapsed and a
// space between blocks, at most [MaxAltText] bytes cut at a character:
// the alt of a Figure that shows text, such as a text box. A text node
// with an ActualText reads as it, not as the characters it draws: a
// symbol font's codes drawn in OpenSymbol are private-use characters,
// which no Alt may hold (ISO 14289-2, 8.4.3).
func ShownText(nodes []*layout.Node) string {
	var sb strings.Builder
	stack := slices.Clone(nodes)
	slices.Reverse(stack)
	for len(stack) > 0 && sb.Len() < MaxAltText {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch {
		case n == nil:
		case n.Kind == layout.Text && n.ActualText != "":
			sb.WriteString(n.ActualText)
		case n.Kind == layout.Text:
			sb.WriteString(n.Text)
		case n.Kind == layout.LineBreak:
			sb.WriteByte(' ')
		default:
			if n.Style != nil && n.Style.Display != css.Inline && sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			for i := len(n.Children) - 1; i >= 0; i-- {
				stack = append(stack, n.Children[i])
			}
		}
	}
	out := strings.Join(strings.Fields(sb.String()), " ")
	for len(out) > MaxAltText {
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	return out
}

// Caption returns the text of a caption as a picture's alt: its white
// space collapsed, at most [MaxAltText] bytes cut at a character.
func Caption(text string) string {
	out := strings.Join(strings.Fields(text), " ")
	for len(out) > MaxAltText {
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	return out
}

// FigureSequence reports whether a caption numbered by the sequence name
// can caption a picture: any name but those an application gives the
// captions of other things, a table, an equation or text (Word's labels
// Table and Equation, LibreOffice's Table and Text), compared without
// case. A Word SEQ field (ECMA-376 Part 1, 17.16.5, SEQ) and an
// OpenDocument text:sequence (ODF 1.3 Part 3, 7.4.13) name their sequence
// so.
func FigureSequence(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "table", "equation", "text":
		return false
	}
	return true
}
