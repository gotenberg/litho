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

// reorderBidiIndices reorders element indices using the same RTL-run detection
// as reorderBidiLine. The caller uses the reordered indices to look up both
// text (in logical order) and spatial positions (for correct spacing).
// The texts slice provides the text content for each index position.
func reorderBidiIndices(texts []string, indices []int) []int {
	if len(indices) == 0 {
		return indices
	}

	hasRTL := false
	for _, t := range texts {
		for _, r := range t {
			if isRTL(r) {
				hasRTL = true
				break
			}
		}
		if hasRTL {
			break
		}
	}
	if !hasRTL {
		return indices
	}

	result := make([]int, len(indices))
	copy(result, indices)

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
			reverseInts(result[i:j])
			i = j
		} else {
			i++
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
// a newline rejoin the split word (the hyphen was discretionary and the word
// should be continuous). Discretionary hyphens (U+2010) at line ends are
// also removed to rejoin split words.
func normalizeSoftHyphens(text string) string {
	// First pass: remove soft hyphens followed by a newline, joining the word.
	text = strings.ReplaceAll(text, "\u00AD\n", "")

	// Second pass: remove discretionary hyphens (U+2010) at line boundaries.
	text = strings.ReplaceAll(text, "\u2010\n", "")

	// Convert remaining standalone soft hyphens to regular ASCII hyphens for searchability.
	text = strings.ReplaceAll(text, "\u00AD", "-")

	return text
}
