package main

import (
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

// perFileTimeout caps how long a single fixture may take through any
// feature. Anti-hang guard: large documents take a couple of seconds even
// on the happy path and parallel corpus runs add contention, so the
// ceiling sits well above the per-file happy-path cost.
const perFileTimeout = 20 * time.Second

// prepareFixtureTimeout caps one fixture's remediation inside a feature's
// prepare stage. It is an anti-hang guard, not a performance target: the
// scoring path bounds fixtures with [perFileTimeout], but the prepare
// goroutines ran unbounded, so a single non-returning remediate wedged the
// whole matrix run behind wg.Wait() with no ceiling and no diagnostic. Three
// minutes leaves two orders of magnitude of headroom over the average
// fixture, room for the slowest real document.
//
// # Corpus evidence
//
//   - All corpora: a whole corpus averages well under a second per fixture.
const prepareFixtureTimeout = 3 * time.Minute

// veraPDFBatchTimeout caps one batched VeraPDF invocation over a staging
// directory. Anti-hang guard: a wedged JVM or an unclosed stdout pipe would
// otherwise block forever, since the prepare context has no deadline.
//
// # Corpus evidence
//
//   - All corpora: the largest bucket validates in a couple of minutes.
const veraPDFBatchTimeout = 30 * time.Minute

// veraPDFSingleTimeout caps the one-file VeraPDF retry used when the batch
// report drops a job. Anti-hang guard: JVM startup alone is about half a
// second, so a minute is far above any real single-file validation.
const veraPDFSingleTimeout = time.Minute

// normalizePDFDate canonicalises a PDF date string for comparison.
// Accepts both the raw "D:YYYYMMDDHHmmSS+HH'MM'" form and ISO 8601;
// returns the input unchanged when parsing fails.
func normalizePDFDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, ok := parsePDFDate(s); ok {
		return t.UTC().Format(time.RFC3339)
	}
	return s
}

func parsePDFDate(s string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	raw := strings.TrimPrefix(s, "D:")
	raw = strings.ReplaceAll(raw, "'", "")
	// Strip a trailing "0000" after a literal Z: some producers emit
	// "Z00'00'" which becomes "Z0000" after quote removal and no Go
	// layout accepts that form.
	if idx := strings.Index(raw, "Z"); idx != -1 && idx == len(raw)-5 {
		raw = raw[:idx+1]
	}
	for _, layout := range []string{
		"20060102150405-0700",
		"20060102150405Z0700",
		"20060102150405Z",
		"20060102150405",
		"200601021504",
		"2006010215",
		"20060102",
		"200601",
		"2006",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// normalizeWhitespace collapses runs of whitespace to a single space
// and trims leading/trailing whitespace. Used before word-level text
// comparison.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// verticalForms maps each vertical presentation form PDF.js substitutes to
// the character it substitutes it for. PDF.js draws an unembedded vertical
// font with a system font, and to make that font draw a punctuation mark or
// bracket upright in a column it replaces the character by its vertical form
// (src/core/fonts.js, the missingFile && vertical branch, which reads
// getVerticalPresentationForm in src/core/fonts_utils.js). The text it
// reports carries the replacement, while the font maps the code to the base
// character. Each form is the base character in the Unicode compatibility
// sense (UnicodeData.txt, `<vertical>` decompositions, which NFKC applies),
// except U+FE34, which PDF.js substitutes for U+FE4F where Unicode decomposes
// it to U+005F. The table is the inverse of getVerticalPresentationForm.
//
//nolint:gochecknoglobals // static lookup table mirroring PDF.js
var verticalForms = map[rune]rune{
	0xFE10: 0xFF0C, // FULLWIDTH COMMA
	0xFE11: 0x3001, // IDEOGRAPHIC COMMA
	0xFE12: 0x3002, // IDEOGRAPHIC FULL STOP
	0xFE13: 0xFF1A, // FULLWIDTH COLON
	0xFE14: 0xFF1B, // FULLWIDTH SEMICOLON
	0xFE15: 0xFF01, // FULLWIDTH EXCLAMATION MARK
	0xFE16: 0xFF1F, // FULLWIDTH QUESTION MARK
	0xFE17: 0x3016, // LEFT WHITE LENTICULAR BRACKET
	0xFE18: 0x3017, // RIGHT WHITE LENTICULAR BRACKET
	0xFE19: 0x2026, // HORIZONTAL ELLIPSIS
	0xFE30: 0x2025, // TWO DOT LEADER
	0xFE31: 0x2014, // EM DASH
	0xFE32: 0x2013, // EN DASH
	0xFE34: 0xFE4F, // WAVY LOW LINE
	0xFE35: 0xFF08, // FULLWIDTH LEFT PARENTHESIS
	0xFE36: 0xFF09, // FULLWIDTH RIGHT PARENTHESIS
	0xFE39: 0x3014, // LEFT TORTOISE SHELL BRACKET
	0xFE3A: 0x3015, // RIGHT TORTOISE SHELL BRACKET
	0xFE3B: 0x3010, // LEFT BLACK LENTICULAR BRACKET
	0xFE3C: 0x3011, // RIGHT BLACK LENTICULAR BRACKET
	0xFE3D: 0x300A, // LEFT DOUBLE ANGLE BRACKET
	0xFE3E: 0x300B, // RIGHT DOUBLE ANGLE BRACKET
	0xFE3F: 0x3008, // LEFT ANGLE BRACKET
	0xFE40: 0x3009, // RIGHT ANGLE BRACKET
	0xFE41: 0x300C, // LEFT CORNER BRACKET
	0xFE42: 0x300D, // RIGHT CORNER BRACKET
	0xFE43: 0x300E, // LEFT WHITE CORNER BRACKET
	0xFE44: 0x300F, // RIGHT WHITE CORNER BRACKET
}

// foldVerticalForms replaces each vertical presentation form PDF.js
// substitutes by the character it stands for (see [verticalForms]). The word
// comparison folds both sides, so a form a file's own ToUnicode maps to stays
// equal to itself. The content comparison needs no fold: NFKC already sends
// each form and its base to the same string.
func foldVerticalForms(s string) string {
	return strings.Map(func(r rune) rune {
		if base, ok := verticalForms[r]; ok {
			return base
		}
		return r
	}, s)
}

// comparePageTextContent returns true when two texts match after all
// whitespace and zero-width characters are stripped and the remaining
// runes are NFKC-normalised.
func comparePageTextContent(goldenText, lithoText string) bool {
	return normalizeContent(goldenText) == normalizeContent(lithoText)
}

func normalizeContent(s string) string {
	return norm.NFKC.String(stripAllSpaces(s))
}

func stripAllSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r <= ' ':
			continue
		case r == 0xA0:
			continue
		case r >= 0x2000 && r <= 0x200F:
			continue
		case r == 0x00AD:
			continue
		case r == 0xFEFF:
			continue
		case r == 0x202A || r == 0x202B || r == 0x202C:
			continue
		case r == 0x2028 || r == 0x2029:
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// textDiffSummary finds the first differing rune position and returns
// context snippets around it. Indexing is on rune offsets so multi-byte
// UTF-8 characters are never sliced mid-codepoint.
func textDiffSummary(expected, got string, maxLen int) (pos int, expSnippet, gotSnippet string) {
	expRunes := []rune(expected)
	gotRunes := []rune(got)
	minLen := min(len(expRunes), len(gotRunes))
	pos = minLen
	for i := range minLen {
		if expRunes[i] != gotRunes[i] {
			pos = i
			break
		}
	}
	start := max(pos-40, 0)
	endExp := min(pos+maxLen, len(expRunes))
	endGot := min(pos+maxLen, len(gotRunes))
	expSnippet = "..." + string(expRunes[start:endExp]) + "..."
	gotSnippet = "..." + string(gotRunes[start:endGot]) + "..."
	if len(got) == 0 {
		gotSnippet = "(empty)"
	}
	return pos, expSnippet, gotSnippet
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

// cachedRun holds a memoised [Feature.Run] result. Features that see the
// same PDFPath under multiple fixture IDs (one per target profile) share
// one entry so extraction runs once per PDF.
type cachedRun struct {
	output []byte
	err    error
}

// stripUndefinedControls removes the bytes PDFDocEncoding leaves
// undefined (ISO 32000-1 Table D.2: 0x00-0x08, 0x0B, 0x0E-0x17, 0x7F)
// from a string. Litho's document.DecodePdfString drops these bytes
// at decode time, so litho-extracted /Info dict fields come out
// clean ("Title" rather than "Title\x00"). PDF.js passes them through
// as raw runes (U+0000, etc.) and the captured golden carries the
// junk. Applying the same drop on both sides in the metadata
// comparison lets the two converge without weakening the test for
// genuine field differences.
//
// Mirrors the four defined low controls (HT, LF, FF, CR) that
// document's pdfDocEncodingUndefined preserves: they round-trip through both
// engines and remain meaningful inside strings.
func stripUndefinedControls(s string) string {
	if s == "" {
		return s
	}
	keep := func(r rune) bool {
		switch {
		case r <= 0x08:
			return false
		case r == 0x0B:
			return false
		case r >= 0x0E && r <= 0x17:
			return false
		case r == 0x7F:
			return false
		}
		return true
	}
	// Fast path: pure ASCII with no controls to strip.
	clean := true
	for _, r := range s {
		if !keep(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if keep(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// baseFixtureID strips the leading profile directory from a fixture ID
// when present. "PDF_A-2b/foo" becomes "foo"; "foo" stays "foo". Lets
// profile-agnostic features (text, metadata) key their goldens on a
// single per-PDF basename while compliance retains the profile-qualified
// form its VeraPDF reports produce.
func baseFixtureID(fixtureID string) string {
	head, rest, ok := strings.Cut(fixtureID, "/")
	if !ok {
		return fixtureID
	}
	if isProfileDir(head) {
		return rest
	}
	return fixtureID
}
