package mutations

import (
	"strings"
	"unicode"
)

// MinNeedleRunes is the fewest letters or digits a removed run must hold for
// the redaction scan to look for it. Anti-noise threshold, not spec-mandated.
//
// A redaction removes one run per show-text operator, and a producer that kerns
// or positions glyph by glyph draws one character per operator. A run of one
// character, of whitespace, or of punctuation cannot identify what was
// removed: the scan matches it as a substring of every string and decoded
// stream, so a space or a "1" survives in nearly every document whatever the
// redaction removed. Refusing over such a copy refuses over nothing, and it is
// the dominant refusal of the fail-closed gate at scale.
//
// Three is the shortest run that names something: the shortest words, a
// three-digit amount, an initialism. A two-character run is a bare number, a
// unit or a state code, and a content stream spells every two-digit number in
// its operands, so it refuses as surely as a single character does while
// identifying as little.
//
// # Corpus evidence
//
//   - govdocs: a whole-page redaction of page 1 refuses 215,065 surviving
//     copies over 584 documents; 54.8% are one character, " " the commonest.
//   - govdocs, litho, pdfcpu: over a whole-page and a top-third redaction of
//     page 1 of each file, a threshold of three lets 289 redactions through
//     that refused before, every one over a survivor of two letters or digits
//     at most, and refuses none that passed. Two lets 232 through, four 324;
//     the 35 four adds over three are refusals over survivors of three
//     letters or digits, "and" beside "HUD", "512" and "34.1", where a word
//     cannot be told from an agency or an amount.
//   - litho cmapnotdef_2b.pdf: page 1 reads "A", and the redaction refused
//     over an "A" elsewhere in the file.
const MinNeedleRunes = 3

// RedactionNeedles turns the runs a redaction removed into the strings the
// verification scan and the document-wide scrubs look for.
//
// A run with at least [MinNeedleRunes] letters or digits is kept whole, as the
// scan has always read it. A run with fewer cannot identify the removed content
// on its own, so it is not looked for. It is not discarded either: consecutive
// short runs are joined in drawing order, the join is split at whitespace, and
// every word of it with at least [MinNeedleRunes] letters or digits is looked
// for. A word drawn glyph by glyph is one short run per glyph, and joining them
// is what keeps it a needle: "SECRET" drawn as six operators is looked for as
// "SECRET", not dropped as six letters.
//
// The safety property holds in two halves. A copy of any removed run with
// [MinNeedleRunes] letters or digits or more is still found, exactly as before
// the filter. A copy of any word with [MinNeedleRunes] letters or digits or more
// drawn across consecutive short runs is found too, because the join spells
// it. Only runs too short to name anything, and words too short to name
// anything, go unscanned. A Han or Hangul character counts as [MinNeedleRunes]
// on its own, because one ideograph can be a whole name.
//
// The result holds each needle once, in the order first met, so the same
// redaction drives the same scan every run.
func RedactionNeedles(runs []string) []string {
	var (
		needles []string
		seen    = make(map[string]bool)
		pending strings.Builder
	)
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			needles = append(needles, s)
		}
	}
	flush := func() {
		for _, word := range strings.FieldsFunc(pending.String(), unicode.IsSpace) {
			if word = strings.TrimFunc(word, isNeedleEdge); identifies(word) {
				add(word)
			}
		}
		pending.Reset()
	}
	for _, run := range runs {
		if identifies(run) {
			flush()
			add(run)
			continue
		}
		pending.WriteString(run)
	}
	flush()
	return needles
}

// identifies reports whether s holds enough letters or digits to name removed
// content (see [MinNeedleRunes]).
func identifies(s string) bool {
	n := 0
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hangul):
			n += MinNeedleRunes
		case unicode.IsLetter(r), unicode.IsDigit(r):
			n++
		}
		if n >= MinNeedleRunes {
			return true
		}
	}
	return false
}

// isNeedleEdge trims the punctuation and symbols that end a joined word, so
// "SECRET." drawn glyph by glyph is looked for as "SECRET" and a copy without
// the full stop is still found.
func isNeedleEdge(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}
