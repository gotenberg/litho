package bookmarks

import (
	"strings"
	"unicode"
)

// HeadingCandidate is the input to a [HeadingScorer]. It contains the
// reconstructed line text and structural features that distinguish headings
// from body text.
type HeadingCandidate struct {
	// Text is the reconstructed line text.
	Text string
	// FontSize is the page-space font size of the line.
	FontSize float64
	// BodySize is the detected body text size for the document (or 0 if
	// unknown).
	BodySize float64
	// PageNum is the 1-based page number where the line appears.
	PageNum int
	// CharCount is the number of characters in Text after normalization.
	CharCount int
	// IsBold indicates the line uses a bold font (heuristic from font name).
	IsBold bool
	// HasNumbering indicates the line starts with a section number prefix.
	HasNumbering bool
	// NumberingDepth is the depth implied by the numbering (1 for "1",
	// 2 for "1.1", etc.). 0 if no numbering.
	NumberingDepth int
	// Script is the dominant script of the text.
	Script scriptKind
}

// HeadingScorer assigns a heading-likelihood score to a candidate line.
// Scores are in [0, 1]; values >= 0.5 are treated as headings. Implementations
// may use any algorithm: rule-based, statistical, ML, or remote API.
//
// litho ships [HeuristicScorer] as the default. Users with specialized
// documents (legal, academic, technical) can plug in custom scorers.
type HeadingScorer interface {
	Score(candidate HeadingCandidate) float64
}

// HeuristicScorer is the default heading scorer. It combines several signals:
// font size relative to body, presence of numbering, line length, casing,
// trailing punctuation, and bold weight. The score is deterministic and
// requires no external dependencies.
type HeuristicScorer struct{}

// Score returns a heuristic heading score in [0, 1].
func (HeuristicScorer) Score(c HeadingCandidate) float64 {
	score := 0.0

	// Font size signal: log-style ratio bonus.
	if c.BodySize > 0 && c.FontSize > c.BodySize {
		ratio := c.FontSize / c.BodySize
		switch {
		case ratio >= 1.5:
			score += 0.5
		case ratio >= 1.25:
			score += 0.4
		case ratio >= 1.15:
			score += 0.25
		case ratio >= 1.05:
			score += 0.10
		}
	}

	// Bold bonus.
	if c.IsBold {
		score += 0.15
	}

	// Numbering bonus: very strong signal.
	if c.HasNumbering {
		score += 0.30
	}

	// Length penalty: long lines are usually body, not headings.
	if c.CharCount > 0 {
		switch {
		case c.CharCount <= 80:
			// no penalty
		case c.CharCount <= 150:
			score -= 0.10
		case c.CharCount <= 250:
			score -= 0.25
		default:
			score -= 0.40
		}
	}

	// Trailing punctuation penalty: headings rarely end with period.
	trimmed := strings.TrimRightFunc(c.Text, unicode.IsSpace)
	if strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, ",") ||
		strings.HasSuffix(trimmed, ";") {
		score -= 0.10
	}

	// Title case / all caps bonus (for Latin scripts).
	if c.Script == scriptLatin && c.CharCount > 0 {
		caps := titleCaseRatio(c.Text)
		switch {
		case caps >= 0.9:
			score += 0.15 // ALL CAPS
		case caps >= 0.5:
			score += 0.08 // Title Case
		}
	}

	// Language-specific pattern bonus.
	if matchLanguagePattern(c.Text, c.Script) {
		score += 0.30
	}

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

// titleCaseRatio returns the ratio of capitalized words to total words.
func titleCaseRatio(s string) float64 {
	words := strings.Fields(s)
	if len(words) == 0 {
		return 0
	}
	caps := 0
	for _, w := range words {
		if w == "" {
			continue
		}
		runes := []rune(w)
		if unicode.IsUpper(runes[0]) {
			caps++
		}
	}
	return float64(caps) / float64(len(words))
}
