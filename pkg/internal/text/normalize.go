package text

import (
	"unicode"

	"github.com/go-text/typesetting/di"
	"golang.org/x/text/unicode/bidi"
)

// normalized is the text of a [Fonts.Layout] call after white space
// handling: one rune slice, the run each rune comes from, and the
// paragraphs it splits into.
type normalized struct {
	text  []rune
	style []int
	paras []paragraphSpan
}

// paragraphSpan is one paragraph of a [normalized] text: text[start:end],
// and the run whose style a blank paragraph takes.
type paragraphSpan struct {
	start, end, style int
}

// isForcedBreak reports whether r ends a line in [Preserve] mode: the UAX #14
// mandatory breaks (classes BK, CR, LF and NL).
func isForcedBreak(r rune) bool {
	switch r {
	case '\n', '\v', '\f', '\r', '\u0085', '\u2028', '\u2029':
		return true
	}
	return false
}

// isCollapsible reports whether r folds into a space in [Collapse] mode.
func isCollapsible(r rune) bool {
	return r == ' ' || r == '\t' || isForcedBreak(r)
}

// normalize applies the white space mode ws to runs.
func normalize(runs []Run, ws Whitespace) normalized {
	if ws == Preserve {
		return normalizePreserve(runs)
	}
	return normalizeCollapse(runs)
}

// normalizeCollapse folds white space as CSS white-space: normal does, into
// one paragraph. A folded space takes the style of the first white space
// character it replaces.
func normalizeCollapse(runs []Run) normalized {
	var n normalized
	pending := -1
	for i, run := range runs {
		for _, r := range run.Text {
			switch {
			case isCollapsible(r):
				if len(n.text) > 0 && pending < 0 {
					pending = i
				}
			case unicode.IsControl(r):
			default:
				if pending >= 0 {
					n.text = append(n.text, ' ')
					n.style = append(n.style, pending)
					pending = -1
				}
				n.text = append(n.text, r)
				n.style = append(n.style, i)
			}
		}
	}
	if len(n.text) > 0 {
		n.paras = []paragraphSpan{{start: 0, end: len(n.text), style: n.style[0]}}
	}
	return n
}

// normalizePreserve keeps white space as CSS white-space: pre-wrap does,
// splitting paragraphs at forced breaks. CR LF counts as one break, and a
// break at the end of the text starts no paragraph.
func normalizePreserve(runs []Run) normalized {
	var n normalized
	start, style, col := 0, -1, 0
	afterCR := false
	closePara := func(run int) {
		if style < 0 {
			style = run
		}
		n.paras = append(n.paras, paragraphSpan{start: start, end: len(n.text), style: style})
		start, style, col = len(n.text), -1, 0
	}
	for i, run := range runs {
		for _, r := range run.Text {
			if afterCR {
				afterCR = false
				if r == '\n' {
					continue
				}
			}
			switch {
			case isForcedBreak(r):
				afterCR = r == '\r'
				closePara(i)
			case r == '\t':
				if style < 0 {
					style = i
				}
				for range tabSize - col%tabSize {
					n.text = append(n.text, ' ')
					n.style = append(n.style, i)
				}
				col = 0
			case unicode.IsControl(r):
			default:
				if style < 0 {
					style = i
				}
				n.text = append(n.text, r)
				n.style = append(n.style, i)
				col++
			}
		}
	}
	if start < len(n.text) {
		closePara(len(runs) - 1)
	}
	return n
}

// paragraphDirection returns the direction of the paragraph para: that of
// its first strong character outside isolates (UAX #9, P2 and P3), left to
// right when it has none.
func paragraphDirection(para []rune) di.Direction {
	depth := 0
	for _, r := range para {
		p, _ := bidi.LookupRune(r)
		switch c := p.Class(); {
		case c == bidi.LRI || c == bidi.RLI || c == bidi.FSI:
			depth++
		case c == bidi.PDI:
			if depth > 0 {
				depth--
			}
		case depth > 0:
		case c == bidi.L:
			return di.DirectionLTR
		case c == bidi.R || c == bidi.AL:
			return di.DirectionRTL
		}
	}
	return di.DirectionLTR
}
