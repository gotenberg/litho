package main

import (
	"strings"
	"testing"
)

// TestBaselineFromScoreboard copies matched/mismatched/skipped + named
// metrics into the baseline shape and drops features that did not load
// goldens (NoGolden, LoadError). The latter would otherwise pin a
// "no golden" state and surface as a regression the moment a corpus
// gets provisioned.
func TestBaselineFromScoreboard(t *testing.T) {
	t.Parallel()
	board := &Scoreboard{
		Corpora: []*CorpusReport{
			{
				Name: "pdfcpu",
				Features: []*FeatureReport{
					{
						Name: "text", Matched: 32, Mismatched: 19,
						Metrics: map[string]int{"chars_match": 46, "words_match": 32},
					},
					{Name: "metadata", Matched: 51},
					{Name: "compliance", NoGolden: true},
					{Name: "remediation", LoadError: "unreadable"},
				},
			},
		},
	}
	bl := baselineFromScoreboard(board)
	corpus, ok := bl.Corpora["pdfcpu"]
	if !ok {
		t.Fatal("missing pdfcpu in baseline")
	}
	if _, ok := corpus.Features["compliance"]; ok {
		t.Error("compliance with NoGolden should be dropped from baseline")
	}
	if _, ok := corpus.Features["remediation"]; ok {
		t.Error("remediation with LoadError should be dropped from baseline")
	}
	text := corpus.Features["text"]
	if text.Matched != 32 {
		t.Errorf("text matched: got %d, want 32", text.Matched)
	}
	if text.Metrics["chars_match"] != 46 {
		t.Errorf("text chars_match: got %d, want 46", text.Metrics["chars_match"])
	}
	if bl.Version != baselineCurrentVersion {
		t.Errorf("version: got %d, want %d", bl.Version, baselineCurrentVersion)
	}
}

// TestDiffBaseline_Equal: identical baselines produce no signal.
func TestDiffBaseline_Equal(t *testing.T) {
	t.Parallel()
	a := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu": {Features: map[string]baselineFeature{
			"text": {Matched: 32, Metrics: map[string]int{"words_match": 32, "chars_match": 46}},
		}},
	}}
	regs, imps, missing := diffBaseline(a, a)
	if len(regs) != 0 || len(imps) != 0 || len(missing) != 0 {
		t.Errorf("identical: regs=%v imps=%v missing=%v", regs, imps, missing)
	}
}

// TestDiffBaseline_Regression: a matched count drop or a metric drop
// surfaces as a regression line naming the cell.
func TestDiffBaseline_Regression(t *testing.T) {
	t.Parallel()
	want := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu": {Features: map[string]baselineFeature{
			"text": {Matched: 32, Metrics: map[string]int{"words_match": 32}},
		}},
	}}
	got := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu": {Features: map[string]baselineFeature{
			"text": {Matched: 30, Metrics: map[string]int{"words_match": 30}},
		}},
	}}
	regs, imps, missing := diffBaseline(want, got)
	if len(imps) != 0 || len(missing) != 0 {
		t.Errorf("unexpected: imps=%v missing=%v", imps, missing)
	}
	if len(regs) != 2 {
		t.Fatalf("regressions: got %d, want 2 (matched + words_match)", len(regs))
	}
	joined := strings.Join(regs, "\n")
	if !strings.Contains(joined, "matched: got 30, baseline 32") {
		t.Errorf("matched regression line: %s", joined)
	}
	if !strings.Contains(joined, "words_match: got 30, baseline 32") {
		t.Errorf("metric regression line: %s", joined)
	}
}

// TestDiffBaseline_Improvement: a matched count or metric INCREASE
// is non-fatal and surfaces under improvements so the operator can
// decide whether to refresh the baseline.
func TestDiffBaseline_Improvement(t *testing.T) {
	t.Parallel()
	want := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu": {Features: map[string]baselineFeature{
			"text": {Matched: 30, Metrics: map[string]int{"words_match": 30}},
		}},
	}}
	got := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu": {Features: map[string]baselineFeature{
			"text": {Matched: 32, Metrics: map[string]int{"words_match": 32}},
		}},
	}}
	regs, imps, missing := diffBaseline(want, got)
	if len(regs) != 0 || len(missing) != 0 {
		t.Errorf("unexpected: regs=%v missing=%v", regs, missing)
	}
	if len(imps) != 2 {
		t.Fatalf("improvements: got %d, want 2", len(imps))
	}
}

// TestDiffBaseline_SplitCatalogViolationsAreCostMetrics pins the DIRECTION of
// the split catalog counters.
//
// They count violations, so an increase must land in regressions. The
// comparator's default rule is good-when-higher, which would report split
// inventing an /AcroForm on more fixtures as an improvement: the one reading
// that lets the defect these counters exist to catch pass as progress.
func TestDiffBaseline_SplitCatalogViolationsAreCostMetrics(t *testing.T) {
	t.Parallel()
	cell := func(invented, checked int) *Baseline {
		return &Baseline{Corpora: map[string]baselineCorpus{
			"pdfcpu": {Features: map[string]baselineFeature{
				"split": {Matched: 33, Metrics: map[string]int{
					"acroform_invented": invented,
					"acroform_checked":  checked,
				}},
			}},
		}}
	}

	regs, imps, _ := diffBaseline(cell(0, 33), cell(2, 33))
	if len(imps) != 0 {
		t.Errorf("more invented forms surfaced as an improvement: %v", imps)
	}
	if len(regs) != 1 {
		t.Fatalf("regressions: got %d (%v), want 1", len(regs), regs)
	}
	if !strings.Contains(regs[0], "acroform_invented: got 2, baseline 0") {
		t.Errorf("regression line: %s", regs[0])
	}

	regs, imps, _ = diffBaseline(cell(2, 33), cell(0, 33))
	if len(regs) != 0 {
		t.Errorf("fewer invented forms surfaced as a regression: %v", regs)
	}
	if len(imps) != 1 {
		t.Fatalf("improvements: got %d (%v), want 1", len(imps), imps)
	}

	// Control against over-correcting: the *_checked sibling is NOT a cost
	// metric, so losing coverage is the regression direction. Without this the
	// test would pass just as well if every split metric had been marked
	// good-when-lower.
	regs, imps, _ = diffBaseline(cell(0, 33), cell(0, 12))
	if len(imps) != 0 {
		t.Errorf("lost coverage surfaced as an improvement: %v", imps)
	}
	if len(regs) != 1 {
		t.Fatalf("checked-drop regressions: got %d (%v), want 1", len(regs), regs)
	}
	if !strings.Contains(regs[0], "acroform_checked: got 12, baseline 33") {
		t.Errorf("checked regression line: %s", regs[0])
	}
}

// TestDiffBaseline_MissingCorpus: a corpus in the baseline that did
// not run in the current invocation surfaces as missing. Each feature
// in the missing corpus gets its own line so the operator sees which
// scope was lost.
func TestDiffBaseline_MissingCorpus(t *testing.T) {
	t.Parallel()
	want := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu":  {Features: map[string]baselineFeature{"text": {Matched: 32}, "metadata": {Matched: 51}}},
		"govdocs": {Features: map[string]baselineFeature{"metadata": {Matched: 1084}}},
	}}
	got := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu": {Features: map[string]baselineFeature{"text": {Matched: 32}, "metadata": {Matched: 51}}},
	}}
	regs, imps, missing := diffBaseline(want, got)
	if len(regs) != 0 || len(imps) != 0 {
		t.Errorf("unexpected: regs=%v imps=%v", regs, imps)
	}
	if len(missing) != 1 {
		t.Fatalf("missing: got %d (%v), want 1", len(missing), missing)
	}
	if !strings.Contains(missing[0], "govdocs / metadata") {
		t.Errorf("missing line: %s", missing[0])
	}
}

// TestDiffBaseline_MissingFeature: a feature present in the baseline
// but filtered out of the current run surfaces as missing without
// failing the check.
func TestDiffBaseline_MissingFeature(t *testing.T) {
	t.Parallel()
	want := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu": {Features: map[string]baselineFeature{
			"text": {Matched: 32}, "metadata": {Matched: 51},
		}},
	}}
	got := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu": {Features: map[string]baselineFeature{
			"text": {Matched: 32},
		}},
	}}
	regs, _, missing := diffBaseline(want, got)
	if len(regs) != 0 {
		t.Errorf("regressions: %v", regs)
	}
	if len(missing) != 1 {
		t.Fatalf("missing: got %d, want 1", len(missing))
	}
	if !strings.Contains(missing[0], "pdfcpu / metadata") {
		t.Errorf("missing line: %s", missing[0])
	}
}

// TestDiffBaseline_NewCorpus: corpora that appear in the current run
// but not the baseline are silent: the baseline owns the surface, and
// new corpora are noise until promoted.
func TestDiffBaseline_NewCorpus(t *testing.T) {
	t.Parallel()
	want := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu": {Features: map[string]baselineFeature{"text": {Matched: 32}}},
	}}
	got := &Baseline{Corpora: map[string]baselineCorpus{
		"pdfcpu":     {Features: map[string]baselineFeature{"text": {Matched: 32}}},
		"new_corpus": {Features: map[string]baselineFeature{"text": {Matched: 5}}},
	}}
	regs, imps, missing := diffBaseline(want, got)
	if len(regs) != 0 || len(imps) != 0 || len(missing) != 0 {
		t.Errorf("new corpus should be silent: regs=%v imps=%v missing=%v", regs, imps, missing)
	}
}
