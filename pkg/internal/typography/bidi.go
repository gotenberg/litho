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
// triggered the visual→logical reversal, leading neutral elements
// (spaces, punctuation emitted at the left of the visual line) are
// moved to the tail so the logical reading order ends with them, as
// PDF.js's full UAX#9 bidi does for a Hebrew-only paragraph.
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
	// belong after the last RTL element in logical order — the visual
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

// normalizeSoftHyphens converts soft hyphens (U+00AD) to regular hyphens
// (U+002D) for searchability. At line boundaries, soft hyphens followed by
// a newline rejoin the split word: the hyphen was discretionary. Unicode
// U+2010 HYPHEN is preserved as-is because PDF.js's normalizeUnicode leaves
// it untouched, and removing it at line ends silently dropped real hyphens
// from compound words.
func normalizeSoftHyphens(text string) string {
	text = strings.ReplaceAll(text, "\u00AD\n", "")
	text = strings.ReplaceAll(text, "\u00AD", "-")
	return text
}
