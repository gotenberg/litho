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

// reorderBidiLine takes a slice of single-character text strings laid out in
// visual (PDF draw) order and returns them in logical reading order.
//
// PDF content streams store glyphs in the order they are painted, which is
// visual order. For RTL scripts this means the characters are reversed
// compared to their logical reading order. This function identifies
// contiguous RTL runs and reverses them in place so the output follows
// logical order.
func reorderBidiLine(chars []string) []string {
	if len(chars) == 0 {
		return chars
	}

	// Fast path: check if any character is RTL at all.
	hasRTL := false
	for _, ch := range chars {
		for _, r := range ch {
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
		return chars
	}

	// Identify runs: consecutive characters that are all RTL (or all non-RTL).
	// Then reverse each RTL run.
	result := make([]string, len(chars))
	copy(result, chars)

	i := 0
	for i < len(result) {
		// Determine direction of current character.
		firstRune := firstRuneOf(result[i])
		if isRTL(firstRune) {
			// Find end of RTL run.
			j := i + 1
			for j < len(result) {
				r := firstRuneOf(result[j])
				// Include neutral/weak characters (spaces, punctuation, digits)
				// that sit between RTL characters as part of the RTL run.
				if isRTL(r) || isNeutralOrWeak(r) {
					j++
				} else {
					break
				}
			}
			// Trim trailing neutrals — they belong to the next LTR run.
			for j > i+1 {
				r := firstRuneOf(result[j-1])
				if !isRTL(r) {
					j--
				} else {
					break
				}
			}
			// Reverse the RTL run.
			reverseStrings(result[i:j])
			i = j
		} else {
			i++
		}
	}

	return result
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

// reverseStrings reverses a slice of strings in place.
func reverseStrings(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// normalizeSoftHyphens converts soft hyphens (U+00AD) to regular hyphens
// (U+002D), matching PDF.js behavior. At line boundaries, soft hyphens
// followed by a newline rejoin the split word. Discretionary hyphens
// (U+2010) at line ends are also removed to rejoin split words.
func normalizeSoftHyphens(text string) string {
	// First pass: remove soft hyphens followed by a newline, joining the word.
	text = strings.ReplaceAll(text, "\u00AD\n", "")

	// Second pass: remove discretionary hyphens (U+2010) at line boundaries.
	text = strings.ReplaceAll(text, "\u2010\n", "")

	// Convert remaining standalone soft hyphens to regular hyphens (PDF.js behavior).
	text = strings.ReplaceAll(text, "\u00AD", "-")

	return text
}
