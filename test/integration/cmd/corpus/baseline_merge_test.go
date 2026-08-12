package main

import "testing"

func cell(matched int, metrics map[string]int) baselineFeature {
	return baselineFeature{Matched: matched, Metrics: metrics}
}

// TestMergeBaselinePreservesUnmeasuredCorpora pins that refreshing one corpus
// keeps the other six.
//
// runBaseline writes the whole file from the corpora present in the run, and
// resolveCorpora narrows those to the -corpus filter, so `baseline -corpus
// verapdf` would leave a file holding verapdf alone. Every other corpus would
// stop gating silently, because diffBaseline reports an absent cell as
// "missing" and only regressions set a non-zero exit.
func TestMergeBaselinePreservesUnmeasuredCorpora(t *testing.T) {
	t.Parallel()

	prev := &Baseline{
		Version:    baselineCurrentVersion,
		CapturedAt: "2026-01-01T00:00:00Z",
		Corpora: map[string]baselineCorpus{
			"verapdf": {Features: map[string]baselineFeature{"remediation": cell(24618, map[string]int{"fully_fixed": 24618})}},
			"pdfjs":   {Features: map[string]baselineFeature{"text": cell(1023, nil)}},
			"litho":   {Features: map[string]baselineFeature{"compliance": cell(542, nil)}},
		},
	}
	fresh := &Baseline{
		Version:    baselineCurrentVersion,
		CapturedAt: "2026-08-12T00:00:00Z",
		Corpora: map[string]baselineCorpus{
			"verapdf": {Features: map[string]baselineFeature{"remediation": cell(24622, map[string]int{"fully_fixed": 24622})}},
		},
	}

	got := mergeBaseline(prev, fresh)

	if len(got.Corpora) != 3 {
		t.Fatalf("merged corpora = %d, want 3 (the untouched corpora must survive)", len(got.Corpora))
	}
	if v := got.Corpora["verapdf"].Features["remediation"].Matched; v != 24622 {
		t.Errorf("verapdf/remediation matched = %d, want the fresh 24622", v)
	}
	if v := got.Corpora["verapdf"].Features["remediation"].Metrics["fully_fixed"]; v != 24622 {
		t.Errorf("verapdf/remediation fully_fixed = %d, want the fresh 24622", v)
	}
	if v := got.Corpora["pdfjs"].Features["text"].Matched; v != 1023 {
		t.Errorf("pdfjs/text matched = %d, want the preserved 1023", v)
	}
	if v := got.Corpora["litho"].Features["compliance"].Matched; v != 542 {
		t.Errorf("litho/compliance matched = %d, want the preserved 542", v)
	}
	if got.CapturedAt != "2026-08-12T00:00:00Z" {
		t.Errorf("captured_at = %q, want the fresh timestamp", got.CapturedAt)
	}
}

// TestMergeBaselineKeepsUnmeasuredFeatures checks the overlay is per feature, so
// refreshing one cell of a corpus does not drop its siblings.
func TestMergeBaselineKeepsUnmeasuredFeatures(t *testing.T) {
	t.Parallel()

	prev := &Baseline{
		Version: baselineCurrentVersion,
		Corpora: map[string]baselineCorpus{
			"verapdf": {Features: map[string]baselineFeature{
				"remediation": cell(24618, nil),
				"compliance":  cell(33813, nil),
			}},
		},
	}
	fresh := &Baseline{
		Version: baselineCurrentVersion,
		Corpora: map[string]baselineCorpus{
			"verapdf": {Features: map[string]baselineFeature{"remediation": cell(24622, nil)}},
		},
	}

	got := mergeBaseline(prev, fresh)

	if v := got.Corpora["verapdf"].Features["compliance"].Matched; v != 33813 {
		t.Errorf("verapdf/compliance matched = %d, want the preserved 33813", v)
	}
	if v := got.Corpora["verapdf"].Features["remediation"].Matched; v != 24622 {
		t.Errorf("verapdf/remediation matched = %d, want the fresh 24622", v)
	}
}

// TestMergeBaselineReplacesWholeCell checks a refreshed feature does not inherit
// a metric it no longer emits. A stale metric would keep gating forever.
func TestMergeBaselineReplacesWholeCell(t *testing.T) {
	t.Parallel()

	prev := &Baseline{
		Version: baselineCurrentVersion,
		Corpora: map[string]baselineCorpus{
			"verapdf": {Features: map[string]baselineFeature{
				"remediation": cell(24618, map[string]int{"fully_fixed": 24618, "retired_metric": 7}),
			}},
		},
	}
	fresh := &Baseline{
		Version: baselineCurrentVersion,
		Corpora: map[string]baselineCorpus{
			"verapdf": {Features: map[string]baselineFeature{
				"remediation": cell(24622, map[string]int{"fully_fixed": 24622}),
			}},
		},
	}

	got := mergeBaseline(prev, fresh)

	if _, stale := got.Corpora["verapdf"].Features["remediation"].Metrics["retired_metric"]; stale {
		t.Error("a metric the feature no longer emits survived the merge")
	}
}

// TestMergeBaselineDoesNotMutateInputs checks the merge leaves the loaded
// baseline alone, so a failure after merging cannot corrupt what was on disk.
func TestMergeBaselineDoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	prev := &Baseline{
		Version: baselineCurrentVersion,
		Corpora: map[string]baselineCorpus{
			"verapdf": {Features: map[string]baselineFeature{"remediation": cell(24618, nil)}},
		},
	}
	fresh := &Baseline{
		Version: baselineCurrentVersion,
		Corpora: map[string]baselineCorpus{
			"verapdf": {Features: map[string]baselineFeature{"remediation": cell(24622, nil)}},
		},
	}

	mergeBaseline(prev, fresh)

	if v := prev.Corpora["verapdf"].Features["remediation"].Matched; v != 24618 {
		t.Errorf("prev was mutated: matched = %d, want 24618", v)
	}
}
