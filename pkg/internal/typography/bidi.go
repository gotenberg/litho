package typography

import (
	"sort"
	"strings"

	"golang.org/x/text/unicode/bidi"

	"github.com/gotenberg/litho/pkg/models"
)

// isRTL returns true if the rune belongs to a right-to-left script (Arabic,
// Hebrew, Syriac, Thaana, NKo, and other RTL Unicode blocks).
func isRTL(r rune) bool {
	p, _ := bidi.LookupRune(r)
	c := p.Class()
	return c == bidi.R || c == bidi.AL || c == bidi.AN
}

// bidiClass is the bidirectional character type of UAX #9, Table 4, reduced
// to the types the resolution in [visualToLogical] tells apart. Explicit
// formatting characters, isolates and boundary neutrals read as other
// neutrals, since that resolution applies no embedding (rules X1-X10).
type bidiClass uint8

const (
	bidiL bidiClass = iota
	bidiR
	bidiAL
	bidiEN
	bidiES
	bidiET
	bidiAN
	bidiCS
	bidiNSM
	bidiWS
	bidiON
)

// runeBidiClass returns the [bidiClass] of r from the Unicode Character
// Database's Bidi_Class property (UAX #9, 3.2). Segment and paragraph
// separators read as whitespace: a line carries none of the paragraphs they
// would separate.
func runeBidiClass(r rune) bidiClass {
	p, _ := bidi.LookupRune(r)
	switch p.Class() {
	case bidi.L:
		return bidiL
	case bidi.R:
		return bidiR
	case bidi.AL:
		return bidiAL
	case bidi.EN:
		return bidiEN
	case bidi.ES:
		return bidiES
	case bidi.ET:
		return bidiET
	case bidi.AN:
		return bidiAN
	case bidi.CS:
		return bidiCS
	case bidi.NSM:
		return bidiNSM
	case bidi.WS, bidi.B, bidi.S:
		return bidiWS
	case bidi.ON, bidi.BN, bidi.Control, bidi.LRO, bidi.RLO, bidi.LRE, bidi.RLE, bidi.PDF, bidi.LRI, bidi.RLI, bidi.FSI, bidi.PDI:
		return bidiON
	}
	return bidiON
}

// elementBidiClass returns the [bidiClass] of one text element: that of its
// first strong character, or of its first character when it has none. An
// element carries the characters one glyph maps to (ISO 32000-1, 9.10.2),
// so a ligature's characters keep the order its /ToUnicode entry gives them
// (ISO 32000-1, 9.10.3). An element with no character is a neutral.
func elementBidiClass(text string) bidiClass {
	first := bidiON
	for i, r := range text {
		c := runeBidiClass(r)
		if c == bidiL || c == bidiR || c == bidiAL {
			return c
		}
		if i == 0 {
			first = c
		}
	}
	return first
}

// isStrongRTL reports whether an element starts with a strong right-to-left
// character (UAX #9 types R and AL). Arabic numbers do not count: their
// digits run from left to right.
func isStrongRTL(text string) bool {
	for _, r := range text {
		c := runeBidiClass(r)
		return c == bidiR || c == bidiAL
	}
	return false
}

// readsInVisualOrder reports whether a line shows its right-to-left glyphs
// in visual order, leftmost first. ISO 32000-1, 14.8.2.3.3 lets a producer
// show right-to-left text either way, so the order is read from the
// positions: of each two consecutive strong right-to-left glyphs in the
// stream, the second sits to the right of the first in visual order and to
// its left in logical order. The line reads in visual order when the moves
// to the right outnumber those to the left. A line whose runs are placed
// from right to left while each run shows its glyphs from left to right
// counts as visual: its runs hold more steps than there are runs.
func readsInVisualOrder(elems []models.TextElement, indices []int) bool {
	rightward, leftward := 0, 0
	prev := -1
	for _, idx := range indices {
		if !isStrongRTL(elems[idx].Text) {
			continue
		}
		if prev >= 0 {
			switch dx := elems[idx].X - elems[prev].X; {
			case dx > 0:
				rightward++
			case dx < 0:
				leftward++
			}
		}
		prev = idx
	}
	return rightward > leftward
}

// bidiSpace stands in the order [reorderBidiIndices] returns for a word
// space read from a gap between two glyphs, where no element is.
const bidiSpace = -1

// visualOrder returns the elements of a line from left to right, with a
// [bidiSpace] where spaced reports a word gap between two glyphs. The stream
// is cut where an element sits wholly to the left of the one shown before
// it, a move back that starts another run, and the runs are set in the
// order of their first positions. Inside a run the stream order stands, so
// a glyph a producer shows with a small move back, a space before a mark of
// punctuation or a mark over its letter, stays where the stream puts it.
//
// Gaps are read inside a run only, between two glyphs the stream shows one
// after the other and neither of them a space glyph, as PDF.js reads them
// before it reorders a text item (src/core/evaluator.js, addFakeSpaces,
// which it calls only for a move forward along the line). A space read
// there is then reordered with the text around it.
func visualOrder(elems []models.TextElement, indices []int, spaced func(left, right int) bool) []int {
	type run struct {
		x     float64
		elems []int
	}
	var runs []run
	for i, idx := range indices {
		el := elems[idx]
		if i > 0 {
			before := indices[i-1]
			prev := elems[before]
			if el.X+max(el.Width, 0) >= prev.X {
				r := &runs[len(runs)-1]
				if spaced != nil && !isSpaceGlyph(prev.Text) && !isSpaceGlyph(el.Text) && spaced(before, idx) {
					r.elems = append(r.elems, bidiSpace)
				}
				r.elems = append(r.elems, idx)
				continue
			}
		}
		runs = append(runs, run{x: el.X, elems: []int{idx}})
	}
	sort.SliceStable(runs, func(a, b int) bool { return runs[a].x < runs[b].x })
	out := make([]int, 0, len(indices))
	for _, r := range runs {
		out = append(out, r.elems...)
	}
	return out
}

// reorderBidiIndices returns the elements of a horizontal line in logical
// order, and whether that order departs from the stream's. A line that
// holds no right-to-left text, or that shows it in logical order already
// ([readsInVisualOrder]), keeps the order of the content stream. Otherwise
// the elements are first set in visual order ([visualOrder]) and then
// resolved to logical order ([visualToLogical]). The word spaces of a
// reordered line are those spaced reads in visual order, each a
// [bidiSpace] in the order returned: two glyphs a reordered line sets side
// by side may sit far apart on the page, or in the reverse order, so the
// gap between them says nothing. A nil spaced reads no gap.
//
// # Corpus evidence
//
//   - pdfjs issue11656.pdf: a Hebrew line placed run by run from right to
//     left, each run drawn from left to right, read its runs reversed,
//     "ףיעס יפל", and "כולם ?" for the "כולם?" its stream shows.
//   - pdfjs issue12705.pdf, issue15033.pdf: numbers inside Hebrew lines read
//     with their digits reversed, "10010" for "01001", "0018817-30" for
//     "03-7188100".
//   - pdfjs issue5677.pdf: an Arabic tanween drawn between its letter and a
//     full stop moved past the stop, "عذرا.ً".
//   - pdfjs issue12705.pdf, issue15033.pdf: word gaps of
//     reversed text with no space glyph were read between glyphs logical
//     order sets side by side, which drops them, or adds one where a
//     right-to-left word touches left-to-right text.
//   - All corpora: words match PDF.js on 2 more files (pdfjs issue11656.pdf,
//     issue5677.pdf) and characters on 2 more, on none fewer. Text changes
//     on 10 files, all pdfjs, each holding right-to-left text, and each comes
//     closer to PDF.js.
func reorderBidiIndices(elems []models.TextElement, indices []int, spaced func(left, right int) bool) ([]int, bool) {
	if len(indices) < 2 || !readsInVisualOrder(elems, indices) {
		return indices, false
	}
	visual := visualOrder(elems, indices, spaced)
	texts := make([]string, len(visual))
	for i, idx := range visual {
		if idx == bidiSpace {
			texts[i] = " "
			continue
		}
		texts[i] = elems[idx].Text
	}
	order := visualToLogical(texts)
	out := make([]int, len(order))
	for i, pos := range order {
		out[i] = visual[pos]
	}
	return out, true
}

// rtlShareThreshold and shortLineLength set the base direction of a line in
// visual order: right to left when at least rtlShareThreshold of its
// characters are right-to-left letters or Arabic numbers, or when it holds
// at most shortLineLength characters, left to right otherwise. UAX #9 rule
// P2 takes the base direction from the first strong character in logical
// order, which text in visual order does not give. Rule HL1 lets a higher
// level protocol set it instead, and the values are PDF.js's
// (src/core/bidi.js, bidi), the reference litho's extraction is measured
// against.
const (
	rtlShareThreshold = 0.3
	shortLineLength   = 4
)

// visualToLogical returns the logical order of the elements of a line, given
// by their texts in visual order, leftmost first, as positions in texts. It resolves the line with the weak,
// neutral and implicit rules of UAX #9 (W1-W7, N1-N2, I1-I2) at the base
// direction [rtlShareThreshold] sets, and reverses runs by rule L2, as
// PDF.js reads a text item (src/core/bidi.js, bidi). Like PDF.js it applies
// no explicit embedding (X1-X10), no bracket pairing (N0) and no mirroring
// (L4): a glyph already shows the shape it is drawn with. It works on
// elements rather than characters, so the characters of one glyph stay in
// their /ToUnicode order ([elementBidiClass]).
func visualToLogical(texts []string) []int {
	n := len(texts)
	types := make([]bidiClass, n)
	rtlChars, chars := 0, 0
	for i, text := range texts {
		types[i] = elementBidiClass(text)
		for _, r := range text {
			chars++
			if c := runeBidiClass(r); c == bidiR || c == bidiAL || c == bidiAN {
				rtlChars++
			}
		}
	}
	rtl := chars <= shortLineLength || float64(rtlChars) >= rtlShareThreshold*float64(chars)
	base := 0
	sor := bidiL
	if rtl {
		base = 1
		sor = bidiR
	}

	resolveWeakTypes(types, sor)
	resolveNeutralTypes(types, sor)

	levels := make([]int, n)
	highest, lowestOdd := -1, -1
	for i, t := range types {
		levels[i] = base
		switch {
		case base == 0 && t == bidiR:
			levels[i] = 1 // I1
		case base == 0 && (t == bidiAN || t == bidiEN):
			levels[i] = 2 // I1
		case base == 1 && (t == bidiL || t == bidiAN || t == bidiEN):
			levels[i] = 2 // I2
		}
		highest = max(highest, levels[i])
		if levels[i]%2 == 1 && (lowestOdd < 0 || levels[i] < lowestOdd) {
			lowestOdd = levels[i]
		}
	}

	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	if lowestOdd < 0 {
		return out
	}
	// L2: from the highest level down to the lowest odd one, reverse every
	// run of elements at that level or higher.
	for level := highest; level >= lowestOdd; level-- {
		start := -1
		for i := 0; i <= n; i++ {
			if i < n && levels[i] >= level {
				if start < 0 {
					start = i
				}
				continue
			}
			if start >= 0 {
				reverseInts(out[start:i])
				start = -1
			}
		}
	}
	return out
}

// resolveWeakTypes applies UAX #9 rules W1-W7 to the types of one level run
// whose start and end of run type is sor. W6 leaves every separator and
// terminator it does not resolve as an other neutral, for
// [resolveNeutralTypes].
func resolveWeakTypes(types []bidiClass, sor bidiClass) {
	n := len(types)
	last := sor
	for i, t := range types { // W1
		if t == bidiNSM {
			types[i] = last
		} else {
			last = t
		}
	}
	last = sor
	for i, t := range types { // W2
		if t == bidiEN && last == bidiAL {
			types[i] = bidiAN
		}
		if t == bidiL || t == bidiR || t == bidiAL {
			last = t
		}
	}
	for i, t := range types { // W3
		if t == bidiAL {
			types[i] = bidiR
		}
	}
	for i := 1; i < n-1; i++ { // W4
		prev, next := types[i-1], types[i+1]
		switch {
		case types[i] == bidiES && prev == bidiEN && next == bidiEN:
			types[i] = bidiEN
		case types[i] == bidiCS && (prev == bidiEN || prev == bidiAN) && next == prev:
			types[i] = prev
		}
	}
	for i := range types { // W5
		if types[i] != bidiEN {
			continue
		}
		for j := i - 1; j >= 0 && types[j] == bidiET; j-- {
			types[j] = bidiEN
		}
		for j := i + 1; j < n && types[j] == bidiET; j++ {
			types[j] = bidiEN
		}
	}
	for i, t := range types { // W6
		if t == bidiWS || t == bidiES || t == bidiET || t == bidiCS {
			types[i] = bidiON
		}
	}
	last = sor
	for i, t := range types { // W7
		if t == bidiEN && last == bidiL {
			types[i] = bidiL
		}
		if t == bidiL || t == bidiR {
			last = t
		}
	}
}

// resolveNeutralTypes applies UAX #9 rules N1 and N2 to the types
// [resolveWeakTypes] left: a run of neutrals between two strong types of the
// same direction takes that direction, numbers reading as right to left and
// sor standing at both ends of the run, and every other neutral takes the
// direction of sor, the embedding direction.
func resolveNeutralTypes(types []bidiClass, sor bidiClass) {
	n := len(types)
	direction := func(t bidiClass) bidiClass {
		if t == bidiL {
			return bidiL
		}
		return bidiR
	}
	for i := 0; i < n; i++ {
		if types[i] != bidiON {
			continue
		}
		end := i
		for end < n && types[end] == bidiON {
			end++
		}
		before, after := sor, sor
		if i > 0 {
			before = direction(types[i-1])
		}
		if end < n {
			after = direction(types[end])
		}
		fill := sor // N2
		if before == after {
			fill = before // N1
		}
		for j := i; j < end; j++ {
			types[j] = fill
		}
		i = end - 1
	}
}

// reverseInts reverses a slice of ints in place.
func reverseInts(s []int) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// normalizeSoftHyphens drops every soft hyphen (U+00AD) and keeps the line
// break after one. A soft hyphen is a format character (Unicode category
// Cf) that marks where a word may break, not text: a /ToUnicode CMap that
// maps a code to it states that the glyph it draws carries no character of
// its own (ISO 32000-1, 9.10.3). PDF.js skips every format character its
// glyphs map to (src/core/evaluator.js, buildTextContentItem,
// isInvisibleFormatMark), the reference litho's extraction is measured
// against, so the two halves of a word hyphenated across lines stay on
// their lines. Rejoining them would guess at a hyphenation the page leaves
// visible. U+2010 HYPHEN and U+002D, the WinAnsiEncoding hyphen at octal 255
// included, are text and stay.
//
// # Corpus evidence
//
//   - govdocs thread_300_300867.pdf: a /ToUnicode CMap maps code 0x7F, which
//     draws the hyphen ending "treat-", to U+00AD, and PDF.js reads "treat"
//     and "ment" on two lines where litho rejoined "treatment".
//   - pdfjs issue9084.pdf, govdocs thread_000_000142.pdf: a mid-line soft
//     hyphen read as "-" ("SS-2541-03-M", "long-term") where PDF.js reads
//     nothing.
//   - govdocs thread_300_300483.pdf, thread_100_100791.pdf: a WinAnsi
//     octal 255 ending a line read as a soft hyphen and rejoined the word,
//     where PDF.js reads "commu-" and "non-".
//   - All corpora, with the WinAnsiEncoding hyphen at octal 255: words match
//     PDF.js on 21 more files (govdocs 18, pdfcpu 1, pdfjs 2) and characters
//     on 12 more, on none fewer.
func normalizeSoftHyphens(text string) string {
	return strings.ReplaceAll(text, "\u00AD", "")
}
