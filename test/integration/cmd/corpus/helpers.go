package main

import (
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

// perFileTimeout caps how long a single fixture may take through any
// feature. Large documents take a couple of seconds even on the happy
// path and parallel corpus runs add contention, so the ceiling sits
// well above the per-file happy-path cost.
const perFileTimeout = 20 * time.Second

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
