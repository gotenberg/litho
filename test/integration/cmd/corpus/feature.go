package main

import (
	"context"
	"encoding/json"
	"errors"
)

// Feature is one comparison dimension: a way to exercise a PDF with litho
// and score the result against a stored reference. Each feature pairs a
// litho entry point (text extraction, compliance validation, remediation)
// with one golden format.
//
// The runner calls [Feature.LoadGoldens] once per (corpus, feature) pair,
// then [Feature.Run] and [Feature.Compare] once per fixture. Run and
// Compare must be safe for concurrent calls once LoadGoldens returns.
type Feature interface {
	// Name returns the short identifier used in CLI flags and
	// scoreboard rows ("text", "metadata", "compliance", "remediation").
	Name() string

	// GoldenSource names the upstream that produces the reference
	// ("pdfjs", "verapdf", "litho-self").
	GoldenSource() string

	// LoadGoldens reads every reference file under goldenDir into the
	// feature's private index. Returns [ErrNoGoldens] when the directory
	// is missing or empty so the caller can show "[no golden]" without
	// failing.
	LoadGoldens(goldenDir string) error

	// HasGolden reports whether a golden is indexed for fixtureID.
	HasGolden(fixtureID string) bool

	// Run applies the feature's litho operation to pdfPath and returns
	// the output blob. The blob format is feature-private; Compare decodes
	// it. fixtureID is forwarded so features that need path-derived
	// metadata (profile, page hints) can extract it.
	Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error)

	// Compare scores output against the golden for fixtureID. When no
	// golden exists, implementations return a [Verdict] with Skipped=true.
	Compare(fixtureID string, output []byte) Verdict
}

// Verdict is the per-fixture outcome of one feature comparison. Score
// lives in [0, 1] and feeds scoreboard aggregation; implementations
// pick between a binary match (0 or 1) and a partial score (fraction of
// matching pages, fraction of violations fixed).
type Verdict struct {
	FixtureID string  `json:"fixture_id"`
	Match     bool    `json:"match"`
	Score     float64 `json:"score"`
	Reason    string  `json:"reason,omitempty"`
	// Skipped marks fixtures that did not count toward the denominator
	// (no golden, unreadable PDF, unsupported profile).
	Skipped bool `json:"skipped,omitempty"`
	// Metrics carries feature-specific breakdowns. Shape is feature-
	// private; the scoreboard reads only the keys it knows about.
	Metrics map[string]int `json:"metrics,omitempty"`
	// Mismatches carries structured diffs already serialised as JSON so
	// the scoreboard can relay them verbatim.
	Mismatches []json.RawMessage `json:"mismatches,omitempty"`
}

// ErrNoGoldens signals that a feature's golden directory is missing or
// empty. The runner treats this as "[no golden]" rather than a fatal error.
var ErrNoGoldens = errors.New("no goldens available")

// PerPDFFeature is an optional interface a [Feature] implements when its
// result depends on the PDF alone, never on the profile a fixture ID
// carries. The runner then scores each PDF once, under its basename,
// instead of once per profile an "expand" manifest fans it out to.
// Corpora that already list one fixture per PDF are unaffected.
type PerPDFFeature interface {
	OncePerPDF()
}

// GoldenOverrider is an optional interface a [Feature] implements when a
// corpus may correct its goldens with a committed override file (see
// [Manifest.GoldenOverrides]). The runner calls LoadOverrides after
// [Feature.LoadGoldens] succeeds.
type GoldenOverrider interface {
	LoadOverrides(path string) error
}

// BatchPreparer is an optional interface a [Feature] can implement when
// per-fixture Run calls would be too expensive. The runner invokes
// Prepare once after [Feature.LoadGoldens] and before any Run call,
// passing the full fixture list. Features that need to batch external
// tool invocations (remediation runs VeraPDF once per profile, not once
// per fixture) populate their internal state here so Run can degrade
// to a lookup.
type BatchPreparer interface {
	Prepare(ctx context.Context, fixtures []Fixture) error
}
