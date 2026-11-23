package html

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/segmenter"
	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// takesFirstLetter reports a display value whose box is a block container,
// the boxes ::first-letter applies to (CSS Pseudo 4, 2.3).
func takesFirstLetter(d css.Display) bool {
	switch d { //nolint:exhaustive // every other display value is no block container
	case css.Block, css.ListItem, css.InlineBlock, css.TableCell, css.TableCaption, css.FlowRoot:
		return true
	}
	return false
}

// firstLetter cuts the first letter of the first formatted line of node,
// the box of the block container n, into an inline box styled by n's
// ::first-letter (CSS Pseudo 4, 2.3 and CSS 2.2, 5.12.2). The box takes
// the place of the characters in the text that holds them, so it nests in
// that text's inline ancestors and inherits from them as the fictional
// tag sequence does, and a floated first letter floats from there. A
// letter already cut by a descendant block's own ::first-letter is left
// as it is.
func (b *builder) firstLetter(node *layout.Node, n *dom.Node) {
	if b.firstLetters == nil {
		b.firstLetters = map[*layout.Node]bool{}
	}
	w := firstLetterCut{b: b, origin: n}
	w.cut(node, 0)
}

// firstLetterCut is one search for a first letter. A text of punctuation
// alone is the whole first letter when text follows it on the line, as a
// ::before of "(" or an open quote is, and holds none when nothing does:
// CSS Pseudo 4, 2.3.2 joins that punctuation to the next letter, and
// litho follows Chromium, which does not.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 colours the "("
//     of a ::before alone and not the letter after it, the open quote of
//     a span's ::before alone in css/CSS2/selectors/first-letter-quote-002,
//     and nothing in a paragraph of "\u2014" alone; a first letter inside
//     an italic span is italic.
type firstLetterCut struct {
	b      *builder
	origin *dom.Node
	// pending is a text of punctuation alone met first: it is the first
	// letter when more text follows it on the line (pendingIn holds it at
	// pendingAt, its punctuation at [pendingPre, pendingEnd)).
	pending                *layout.Node
	pendingIn              *layout.Node
	pendingAt              int
	pendingPre, pendingEnd int
	// started reports white space that stays met first, a line of its
	// own when a block follows.
	started bool
}

// cut looks for the first letter in the children of node, at depth below
// the block container. It reports whether the search is over: a letter
// was cut, or the first formatted line starts with something that is no
// text (a replaced element, an atomic inline, a line break), which holds
// no first letter.
func (w *firstLetterCut) cut(node *layout.Node, depth int) bool {
	if depth >= maxTreeDepth {
		return true
	}
	for i, c := range node.Children {
		switch c.Kind { //nolint:exhaustive // the other kinds hold no text and end the search
		case layout.Text:
			if w.b.gcpm.paged[c] != nil {
				return true
			}
			pre, n, punctOnly, ended := firstLetterSpan(c.Text, c.Style.WhiteSpace)
			switch {
			case ended:
				// A preserved line break ends the first line before any
				// letter: it has none.
				return true
			case n < 0:
				// White space alone: the line has not started unless some
				// stays, which a block after it does not continue.
				w.started = w.started || pre < len(c.Text)
				continue
			case w.pending != nil:
				w.split(w.pendingIn, w.pendingAt, w.pending, w.pendingPre, w.pendingEnd)
				return true
			case punctOnly:
				w.pending, w.pendingIn, w.pendingAt, w.pendingPre, w.pendingEnd = c, node, i, pre, n
				continue
			}
			w.split(node, i, c, pre, n)
			return true
		case layout.Element:
			if w.b.firstLetters[c] {
				return true
			}
			st := c.Style
			if st == nil || c.Leader {
				return true
			}
			if st.Display == css.Contents {
				// An element of display: contents has no box: its
				// children sit on the line in its place (CSS Display 3,
				// 2.5), whatever float or position it sets.
				if w.cut(c, depth+1) {
					return true
				}
				continue
			}
			// Floats and absolutely positioned boxes are out of the flow:
			// the first line starts after them.
			if st.Float != css.FloatNone || st.Position == css.Absolute || st.Position == css.Fixed {
				continue
			}
			// A list item's marker holds no first letter: the search goes
			// on past it, into the item's own content (CSS Pseudo 4,
			// 2.2.2).
			switch st.Display { //nolint:exhaustive // every other display value is atomic or a table, which ends the search
			case css.Inline, css.InlineListItem:
				if w.cut(c, depth+1) {
					return true
				}
			case css.Block, css.ListItem, css.FlowRoot:
				// The first line of a block's first in-flow block child is
				// its first line (CSS Pseudo 4, 2.3), unless white space
				// that stays made a line before it.
				if w.pending != nil || w.started {
					return true
				}
				return w.cut(c, depth+1)
			default:
				return true
			}
		default:
			return true
		}
	}
	return false
}

// split replaces the text t, the child at of parent, with the text before
// pre, the first letter box holding [pre, end) and the text after end.
func (w *firstLetterCut) split(parent *layout.Node, at int, t *layout.Node, pre, end int) {
	ps := w.b.computed.FirstLetter(w.origin, t.Style)
	if ps == nil {
		return
	}
	st := letterStyle(ps)
	letter := &layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{
		{Kind: layout.Text, Style: st, Text: t.Text[pre:end]},
	}}
	letter.InitialLetter = st.InitialLetter.Size >= 1
	w.b.firstLetters[letter] = true
	w.b.onFirstLine(letter, w.origin, css.PseudoFirstLetter, true)
	repl := make([]*layout.Node, 0, 3)
	if pre > 0 {
		repl = append(repl, &layout.Node{Kind: layout.Text, Style: t.Style, Text: t.Text[:pre]})
	}
	repl = append(repl, letter)
	if end < len(t.Text) {
		repl = append(repl, &layout.Node{Kind: layout.Text, Style: t.Style, Text: t.Text[end:]})
	}
	parent.Children = append(parent.Children[:at:at], append(repl, parent.Children[at+1:]...)...)
}

// letterStyle returns the style of a first letter box from the computed
// style ps of its ::first-letter: inline, or a block when it floats (CSS
// Pseudo 4, 2.3.1), and never positioned. An initial letter is a block
// floating to the start side, which the layout sizes and places
// (layout.Node.InitialLetter); initial-letter does not apply to a letter
// that floats on its own (CSS Inline 3, 4.3.1).
func letterStyle(ps *css.Style) *css.Style {
	st := *ps
	switch {
	case st.Float != css.FloatNone:
		st.Display = css.Block
		st.InitialLetter = css.InitialLetter{}
	case st.InitialLetter.Size >= 1:
		st.Display = css.Block
		st.Float = css.FloatLeft
		if st.Direction == css.RTL {
			st.Float = css.FloatRight
		}
	default:
		st.Display = css.Inline
	}
	st.Position = css.Static
	return &st
}

// firstLetterSpan returns the bytes of s, of white-space ws, its first
// letter takes: the collapsible white space before it at [0, pre), then
// the white space that stays, the punctuation before the letter, the
// letter and the punctuation after it up to n (CSS Pseudo 4, 2.3.2, the
// first typographic letter unit). The letter is the first grapheme
// cluster that is no punctuation nor white space. n is -1 when s is white
// space alone. punctOnly reports punctuation that runs to the end of s
// with no letter, [pre, n) holding it. ended reports a line break ws
// keeps before any letter, which ends the first line.
//
// CSS Pseudo 4 takes the punctuation of the general categories Ps, Pe,
// Pi, Pf and Po (CSS 2.2, 5.12.2 alike) and the typographic spaces (Zs)
// between it and the letter. Chromium departs from it, and litho follows
// Chromium: before the letter it takes every punctuation category, Pd and
// Pc included, and any white space between; after it, Pc as well as the
// five, and a space other than U+0020 only when punctuation follows it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 cuts "-h", "_l",
//     "\u2014d" and "\"  q" before the letter, "T_" and "T\u2003." after
//     it, but "T" alone from "T-", "T\u2014" and "T ."; it finds the letter
//     past a leading no-break space and U+3000, and takes "+" and "$" as
//     letters. The background of the ::first-letter covers leading
//     no-break spaces, the spaces and the tab white-space: pre and
//     pre-wrap keep, and not the spaces pre-line collapses; under pre, a
//     line break before the letter leaves the paragraph without one.
func firstLetterSpan(s string, ws css.WhiteSpace) (pre, n int, punctOnly, ended bool) {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isFirstLetterSpace(r) {
			break
		}
		if isLineBreak(r) && keepsBreaks(ws) {
			return 0, -1, false, true
		}
		if !collapsibleSpace(r, ws) {
			break
		}
		i += size
	}
	pre = i
	end := i
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsPunct(r) {
			i += size
			end = i
			continue
		}
		if !isFirstLetterSpace(r) {
			break
		}
		if isLineBreak(r) && keepsBreaks(ws) {
			return 0, -1, false, true
		}
		i += size
	}
	if end == pre && i == len(s) {
		return pre, -1, false, false
	}
	if i == len(s) {
		return pre, end, true, false
	}
	i += graphemeLen(s[i:])
	for j := i; j < len(s); {
		r, size := utf8.DecodeRuneInString(s[j:])
		j += size
		if isTrailingPunct(r) {
			i = j
			continue
		}
		if r == ' ' || !unicode.Is(unicode.Zs, r) {
			break
		}
	}
	return pre, i, false, false
}

// isLineBreak reports a segment break (CSS Text 3, 4.1).
func isLineBreak(r rune) bool { return r == '\n' || r == '\r' || r == '\f' }

// keepsBreaks reports a white-space value that keeps segment breaks (CSS
// Text 3, 3).
func keepsBreaks(ws css.WhiteSpace) bool {
	return ws != css.WhiteSpaceNormal && ws != css.Nowrap
}

// collapsibleSpace reports document white space ws collapses (CSS Text 3,
// 4.1.1): a space, tab or segment break under normal and nowrap, a space
// or tab under pre-line.
func collapsibleSpace(r rune, ws css.WhiteSpace) bool {
	switch ws { //nolint:exhaustive // the other values keep their spaces
	case css.WhiteSpaceNormal, css.Nowrap:
		return r == ' ' || r == '\t' || isLineBreak(r)
	case css.PreLine, css.PreLineNowrap:
		return r == ' ' || r == '\t'
	}
	return false
}

// maxGraphemeScan bounds the bytes searched for the end of a first
// letter's grapheme: 256. Anti-DoS guard, not spec-mandated: the longest
// emoji sequences take a few dozen bytes.
const maxGraphemeScan = 256

// graphemeLen returns the bytes of the extended grapheme cluster s starts
// with (UAX #29), one character when s does not segment.
func graphemeLen(s string) int {
	_, size := utf8.DecodeRuneInString(s)
	head := s
	if len(head) > maxGraphemeScan {
		head = strings.ToValidUTF8(head[:maxGraphemeScan], "")
	}
	var seg segmenter.Segmenter
	if seg.InitWithString(head) != nil {
		return size
	}
	it := seg.GraphemeIterator()
	if !it.Next() {
		return size
	}
	return max(it.Grapheme().LengthInBytes, size)
}

// isFirstLetterSpace reports the white space around a first letter: the
// document white space characters (CSS Text 3, 4.1) and the typographic
// spaces (Zs), no-break space included.
func isFirstLetterSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\r', '\f':
		return true
	}
	return unicode.Is(unicode.Zs, r)
}

// isTrailingPunct reports the punctuation a first letter takes after its
// letter: the general categories Ps, Pe, Pi, Pf and Po of CSS Pseudo 4,
// and Pc, as Chromium takes it (see [firstLetterSpan]).
func isTrailingPunct(r rune) bool {
	return unicode.In(r, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf, unicode.Po, unicode.Pc)
}
