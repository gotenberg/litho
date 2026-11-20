package plugins

import "github.com/gotenberg/litho/pkg/models"

// HeadingScorer assigns a heading-likelihood score to a candidate line.
// Scores are in [0, 1]; values >= 0.5 are treated as headings.
// Implementations may use any algorithm (rule-based, statistical, ML).
// Implementations must be safe for concurrent use.
type HeadingScorer interface {
	// Score returns the heading likelihood of candidate, in [0, 1].
	Score(candidate models.HeadingCandidate) float64
}
