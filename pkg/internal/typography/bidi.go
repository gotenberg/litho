package typography

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/bidi"
)

// isRTL returns true if the rune belongs to a right-to-left script (Arabic,
// Hebrew, Syriac, Thaana, NKo, and other RTL Unicode blocks).
func isRTL(r rune) bool {
	p, _ := bidi.LookupRune(r)
	c := p.Class()
	return c == bidi.R || c == bidi.AL || c == bidi.AN
}

// firstRuneOf returns the first rune from a string, or 0 if empty.
func firstRuneOf(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// isNeutralOrWeak returns true for characters that are directionally neutral
// or weak (spaces, punctuation, common digits) according to the bidi algorithm.
func isNeutralOrWeak(r rune) bool {
	if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return true
	}
	p, _ := bidi.LookupRune(r)
	c := p.Class()
	return c == bidi.WS || c == bidi.ON || c == bidi.ES || c == bidi.ET ||
		c == bidi.CS || c == bidi.NSM || c == bidi.BN || c == bidi.EN
}

// reorderBidiIndices reorders element indices so that RTL runs stored in
// visual order (X decreasing in content-stream order) are reversed to
// logical order. RTL runs already in logical order (X increasing) are
// left untouched. The xPositions slice provides the X coordinate for
// each element so the heuristic can detect visual vs logical order.
//
// When a line's only strong direction is RTL and at least one run
// triggered the visual-to-logical reversal, leading neutral elements
// (spaces, punctuation emitted at the left of the visual line) are
// moved to the tail so the logical reading order ends with them, as
// UAX#9 prescribes for a pure-RTL paragraph.
func reorderBidiIndices(texts []string, indices []int, xPositions []float64) []int {
	if len(indices) == 0 {
		return indices
	}

	hasRTL := false
	hasLTR := false
	for _, t := range texts {
		for _, r := range t {
			if isRTL(r) {
				hasRTL = true
			} else if !isNeutralOrWeak(r) {
				hasLTR = true
			}
		}
	}
	if !hasRTL {
		return indices
	}

	result := make([]int, len(indices))
	copy(result, indices)

	reversedAnyRun := false
	i := 0
	for i < len(result) {
		firstRune := firstRuneOf(texts[result[i]])
		if isRTL(firstRune) {
			j := i + 1
			for j < len(result) {
				r := firstRuneOf(texts[result[j]])
				if isRTL(r) || isNeutralOrWeak(r) {
					j++
				} else {
					break
				}
			}
			for j > i+1 {
				r := firstRuneOf(texts[result[j-1]])
				if !isRTL(r) {
					j--
				} else {
					break
				}
			}
			// Only reverse if the run is in visual order. RTL text reads
			// right-to-left, so logical order has decreasing X (first
			// logical char is rightmost). Visual order has increasing X
			// (leftmost glyph first in stream, which is the last
			// logical character). Reverse when X increases across the run.
			if len(xPositions) > 0 {
				firstX := xPositions[result[i]]
				lastX := xPositions[result[j-1]]
				if firstX < lastX {
					reverseInts(result[i:j])
					reversedAnyRun = true
				}
			}
			i = j
		} else {
			i++
		}
	}

	// RTL-paragraph neutral shift: when the line is purely RTL (no LTR
	// strong characters) and at least one run was in visual order, any
	// neutrals the content stream placed before the first RTL element
	// belong after the last RTL element in logical order: the visual
	// "left of line" is the logical "end of line" for Hebrew/Arabic.
	if reversedAnyRun && !hasLTR {
		k := 0
		for k < len(result) && !isRTL(firstRuneOf(texts[result[k]])) {
			k++
		}
		if k > 0 && k < len(result) {
			leading := append([]int(nil), result[:k]...)
			result = append(result[k:], leading...)
		}
	}

	return result
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
