package bookmarks

import (
	"regexp"
	"strings"
)

// numberingMatch holds the parsed result of a section numbering prefix.
type numberingMatch struct {
	// Depth is the heading depth implied by the numbering (1 for "1", 2 for
	// "1.1", 3 for "1.1.1"). 1 for non-decimal forms (Roman, alpha, chapter).
	Depth int
	// Prefix is the literal numbering text including its trailing whitespace.
	Prefix string
	// Title is the heading text without the prefix.
	Title string
}

// Pre-compiled regexes for the numbering tiers.
//
//nolint:gochecknoglobals // immutable lookup tables
var (
	// Decimal: "1", "1.2", "1.2.3", followed by whitespace and word chars.
	// Requires the next token to be alphabetic to avoid matching "1.5 kg".
	reDecimal = regexp.MustCompile(`^(\d+(?:\.\d+)*)[\s\x{00a0}]+([\p{L}].+)$`)

	// Alpha: "A.", "A.1", "B.2.3", uppercase letter optionally with sub-numbers.
	reAlpha = regexp.MustCompile(`^([A-Z](?:\.\d+)*)\.[\s\x{00a0}]+([\p{L}].+)$`)

	// Roman: "I.", "IV.", "XII.", must be followed by space and word.
	reRoman = regexp.MustCompile(`^([IVXLCDM]+)\.[\s\x{00a0}]+([\p{L}].+)$`)

	// Section symbol: "§2", "§ 2.3", followed by space and word.
	reSection = regexp.MustCompile(`^§[\s\x{00a0}]*(\d+(?:\.\d+)*)[\s\x{00a0}]+([\p{L}].+)$`)

	// Chapter word: "Chapter 5", "Chapter 5:", "Chapter V."
	reChapter = regexp.MustCompile(`^(?i)(chapter|section|part|appendix|annex)[\s\x{00a0}]+([\dIVXLCDM]+)[:.]?[\s\x{00a0}]+([\p{L}].+)$`)
)

// parseNumbering attempts to parse a section number prefix from the given
// title. Returns nil if no numbering pattern matches.
func parseNumbering(title string) *numberingMatch {
	// Decimal first: most common and most informative for depth.
	if m := reDecimal.FindStringSubmatch(title); m != nil {
		titleText := strings.TrimSpace(m[2])
		// Reject likely unit patterns: "1.5 kg" where the title is a single
		// short word (≤ 2 chars). Real headings have longer titles.
		if !looksLikeHeadingTitle(titleText) {
			return nil
		}
		number := m[1]
		depth := strings.Count(number, ".") + 1
		return &numberingMatch{
			Depth:  depth,
			Prefix: number,
			Title:  titleText,
		}
	}
	if m := reSection.FindStringSubmatch(title); m != nil {
		number := m[1]
		depth := strings.Count(number, ".") + 1
		return &numberingMatch{
			Depth:  depth,
			Prefix: "§" + number,
			Title:  strings.TrimSpace(m[2]),
		}
	}
	if m := reAlpha.FindStringSubmatch(title); m != nil {
		// "A.1.2" → depth 3 (the leading letter counts as level 1)
		depth := strings.Count(m[1], ".") + 1
		return &numberingMatch{
			Depth:  depth,
			Prefix: m[1],
			Title:  strings.TrimSpace(m[2]),
		}
	}
	if m := reRoman.FindStringSubmatch(title); m != nil {
		return &numberingMatch{
			Depth:  1,
			Prefix: m[1],
			Title:  strings.TrimSpace(m[2]),
		}
	}
	if m := reChapter.FindStringSubmatch(title); m != nil {
		return &numberingMatch{
			Depth:  1,
			Prefix: m[1] + " " + m[2],
			Title:  strings.TrimSpace(m[3]),
		}
	}
	return nil
}

// looksLikeTOCEntry returns true if a line looks like a Table of Contents
// entry (numbering, ellipsis leaders, trailing page number) and should be
// filtered out from heading detection.
//
//nolint:gochecknoglobals // immutable lookup
var reTOCLine = regexp.MustCompile(`[.·•\s]{4,}\d+\s*$`)

func looksLikeTOCEntry(s string) bool {
	return reTOCLine.MatchString(s)
}

// looksLikeHeadingTitle rejects very short single-word titles that are
// likely measurement units (e.g., "kg", "mm") rather than real headings.
func looksLikeHeadingTitle(s string) bool {
	words := strings.Fields(s)
	if len(words) == 0 {
		return false
	}
	if len(words) == 1 && len(words[0]) <= 2 {
		return false
	}
	return true
}
