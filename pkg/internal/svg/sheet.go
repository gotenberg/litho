package svg

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// rule is one style rule of a style sheet: a selector list and its
// declarations in source order.
type rule struct {
	sel   *css.Selector
	decls []declaration
	order int
}

// declaration is one property declaration as written.
type declaration struct {
	prop      string
	value     string
	important bool
}

// parseSheet splits a style sheet into its style rules (CSS Syntax 3,
// 5.3.3), at most limit of them. At-rules are dropped with their blocks:
// @media, @import and @font-face style nothing an SVG drawing needs here.
// A rule whose selector the selector engine refuses is dropped (CSS Syntax
// 3, 9).
func parseSheet(src string, limit int) []rule {
	src = stripComments(src)
	var out []rule
	i := 0
	for i < len(src) && len(out) < limit {
		for i < len(src) && (isWSP(src[i]) || src[i] == ';') {
			i++
		}
		if i >= len(src) {
			break
		}
		// CDO and CDC tokens are skipped at the top level (CSS Syntax 3,
		// 5.4.1).
		if strings.HasPrefix(src[i:], "<!--") {
			i += 4
			continue
		}
		if strings.HasPrefix(src[i:], "-->") {
			i += 3
			continue
		}
		at := src[i] == '@'
		open := scanTo(src, i, '{', ';')
		if open >= len(src) {
			break
		}
		if src[open] == ';' {
			// An at-rule without a block, such as @import.
			i = open + 1
			continue
		}
		end := matchBrace(src, open)
		prelude := strings.TrimSpace(src[i:open])
		body := src[open+1 : min(end, len(src))]
		i = end + 1
		if at {
			continue
		}
		sel, ok := css.ParseSelector(prelude)
		if !ok {
			continue
		}
		out = append(out, rule{sel: sel, decls: parseDeclarations(body), order: len(out)})
	}
	return out
}

// stripComments removes /* */ comments outside strings.
func stripComments(s string) string {
	if !strings.Contains(s, "/*") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\'' {
			j := endString(s, i)
			b.WriteString(s[i:j])
			i = j - 1
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				break
			}
			i += end + 3
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// endString returns the index just past the string that starts at i.
func endString(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case q, '\n':
			return j + 1
		}
	}
	return len(s)
}

// scanTo returns the index of the first of a or b at or after i outside
// strings and parentheses, len(s) when there is none.
func scanTo(s string, i int, a, b byte) int {
	depth := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			i = endString(s, i)
			continue
		case c == '(':
			depth++
		case c == ')' && depth > 0:
			depth--
		case depth == 0 && (c == a || c == b):
			return i
		}
		i++
	}
	return len(s)
}

// matchBrace returns the index of the brace closing the one at open,
// len(s) when the block runs to the end.
func matchBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			i = endString(s, i) - 1
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(s)
}

// parseDeclarations parses a declaration list, a rule's block or a style
// attribute (CSS Style Attributes, 3): name: value pairs separated by
// semicolons, each optionally !important. Property names are ASCII
// case-insensitive; an invalid declaration is dropped.
func parseDeclarations(s string) []declaration {
	var out []declaration
	i := 0
	for i < len(s) {
		end := scanTo(s, i, ';', '}')
		part := s[i:min(end, len(s))]
		i = end + 1
		name, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		imp := false
		if k := strings.LastIndexByte(value, '!'); k >= 0 && strings.EqualFold(strings.TrimSpace(value[k+1:]), "important") {
			imp = true
			value = strings.TrimSpace(value[:k])
		}
		if name == "" || value == "" {
			continue
		}
		out = append(out, declaration{prop: name, value: value, important: imp})
	}
	return out
}

// styleText returns the text of a style element: its text and CDATA
// children joined.
func styleText(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}
