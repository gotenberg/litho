package models

// HeadingScript classifies a candidate's dominant script. Scorers can
// use it to pick script-specific heuristics (for example, CJK headings
// have different length and punctuation conventions than Latin).
// HeadingScript values are immutable and safe for concurrent use.
type HeadingScript string

// The scripts a heading candidate is classified by.
const (
	HeadingScriptLatin      HeadingScript = "Latin"
	HeadingScriptCJK        HeadingScript = "CJK"
	HeadingScriptCyrillic   HeadingScript = "Cyrillic"
	HeadingScriptArabic     HeadingScript = "Arabic"
	HeadingScriptDevanagari HeadingScript = "Devanagari"
	HeadingScriptThai       HeadingScript = "Thai"
	HeadingScriptOther      HeadingScript = "Other"
)

// HeadingCandidate is the input passed to a
// [github.com/gotenberg/litho/pkg/plugins.HeadingScorer]. It holds the
// reconstructed line text plus structural features that distinguish headings
// from body text. Concurrent reads of a value are safe; a write needs
// synchronization.
type HeadingCandidate struct {
	// Text is the reconstructed line text.
	Text string `json:"text"`
	// FontSize is the page-space font size of the line.
	FontSize float64 `json:"fontSize"`
	// BodySize is the detected body text size for the document (or 0
	// if unknown).
	BodySize float64 `json:"bodySize"`
	// PageNum is the 1-based page number where the line appears.
	PageNum int `json:"pageNum"`
	// CharCount is the number of characters in Text after normalization.
	CharCount int `json:"charCount"`
	// IsBold indicates the line uses a bold font.
	IsBold bool `json:"isBold"`
	// HasNumbering indicates the line starts with a section-number prefix.
	HasNumbering bool `json:"hasNumbering"`
	// NumberingDepth is the depth implied by the numbering (1 for "1",
	// 2 for "1.1", etc.). 0 if no numbering.
	NumberingDepth int `json:"numberingDepth"`
	// Script is the dominant script of the text.
	Script HeadingScript `json:"script"`
}
