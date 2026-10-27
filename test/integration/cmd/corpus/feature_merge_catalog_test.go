package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// mergeCatalogFixture builds the two-page merge output every test here starts
// from: page count, per-page text and both bookmark multisets already agree,
// so the catalog check is the only thing left that can decide the verdict.
//
// The pdfcpu golden must be populated or Compare returns Skipped before
// reaching the catalog check, and the test would pass while asserting nothing.
func mergeCatalogFixture(t *testing.T, facts catalogFacts) (*mergeFeature, []byte) {
	t.Helper()
	f := newMergeFeature()
	f.pdfcpuGolds["doc"] = []normalizedBookmark{
		{Title: "Intro", Page: 1},
		{Title: "Intro", Page: 3},
	}
	out, err := json.Marshal(mergeOutput{
		InputPages:   2,
		OutPages:     4,
		InputText:    []string{"A", "B"},
		OutText:      []string{"A", "B", "A", "B"},
		InputBM:      []bookmarkLeaf{{Title: "Intro", Page: 1}},
		LithoOutBM:   []bookmarkLeaf{{Title: "Intro", Page: 1}, {Title: "Intro", Page: 3}},
		catalogFacts: facts,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return f, out
}

// TestMergeFeature_Compare_CatalogViolationFailsTheFixture walks the whole
// Compare path. Everything else agrees, so only the catalog check can produce
// the mismatch, and the message must name merge rather than split.
func TestMergeFeature_Compare_CatalogViolationFailsTheFixture(t *testing.T) {
	t.Parallel()
	f, out := mergeCatalogFixture(t, catalogFacts{OutAcroForm: true})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "merge invented an /AcroForm") {
		t.Errorf("reason: got %q, want it to name merge", v.Reason)
	}
	if v.Metrics["acroform_invented"] != 1 {
		t.Errorf("acroform_invented = %d, want 1", v.Metrics["acroform_invented"])
	}
}

// TestMergeFeature_Compare_CleanFixtureCarriesMetrics keeps the coverage
// counters honest on the happy path. Without this they would exist only on
// failing fixtures and acroform_checked would be meaningless.
func TestMergeFeature_Compare_CleanFixtureCarriesMetrics(t *testing.T) {
	t.Parallel()
	f, out := mergeCatalogFixture(t, catalogFacts{})
	v := f.Compare("doc", out)
	if !v.Match {
		t.Fatalf("expected match, got %+v", v)
	}
	if v.Metrics["acroform_checked"] != 1 {
		t.Errorf("acroform_checked = %d, want 1", v.Metrics["acroform_checked"])
	}
	if v.Metrics["acroform_invented"] != 0 {
		t.Errorf("acroform_invented = %d on a clean fixture", v.Metrics["acroform_invented"])
	}
}

// TestMergeFeature_Compare_SinglePageFixtureIsExercised is the reason this
// invariant lives on merge at all.
//
// feature_split skips inputs under two pages, so a single-page document never
// reaches its catalog check. runMerge has no such floor: it merges the file
// against itself and skips only empty input or one past mergeMaxPages.
//
// # Corpus evidence
//
//   - All corpora: 71 of the 85 files carrying the shape that trips the
//     field collector are single-page.
//   - pdfjs issue20062.pdf: the fixture isFieldRoot's godoc names as the
//     reproducer is one of those single-page files.
func TestMergeFeature_Compare_SinglePageFixtureIsExercised(t *testing.T) {
	t.Parallel()
	f := newMergeFeature()
	f.pdfcpuGolds["doc"] = []normalizedBookmark{}
	out, err := json.Marshal(mergeOutput{
		InputPages:  1,
		OutPages:    2,
		InputText:   []string{"A"},
		OutText:     []string{"A", "A"},
		OutAcroForm: true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	v := f.Compare("doc", out)
	if v.Skipped {
		t.Fatalf("single-page input was skipped; merge has no two-page floor: %+v", v)
	}
	if v.Match {
		t.Fatalf("expected the catalog violation to fail the fixture, got %+v", v)
	}
	if !strings.Contains(v.Reason, "invented an /AcroForm") {
		t.Errorf("reason: got %q", v.Reason)
	}
}

// TestMergeFeature_Compare_EarlyFailureCarriesNoMetrics pins the counting
// rule: acroform_checked means the catalog invariant RAN. A fixture failing
// page count never reached it, so counting it would inflate the coverage
// number the *_checked counters exist to report honestly.
func TestMergeFeature_Compare_EarlyFailureCarriesNoMetrics(t *testing.T) {
	t.Parallel()
	f := newMergeFeature()
	f.pdfcpuGolds["doc"] = []normalizedBookmark{}
	out, _ := json.Marshal(mergeOutput{
		InputPages: 2,
		OutPages:   3, // want 2 * 2
		InputText:  []string{"A", "B"},
		OutText:    []string{"A", "B", "A"},
	})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if len(v.Metrics) != 0 {
		t.Errorf("page-count failure reported metrics %v; the catalog was never compared", v.Metrics)
	}
}

// TestMergeFeature_Compare_PreservedFormIsNotReported is the control against
// the invariant firing on every form-bearing document. A document that already
// owns fields must pass whether or not an /AcroForm listed them: a merge that
// rebuilds the form for widgets no usable /AcroForm entry lists is repair.
//
// # Corpus evidence
//
//   - pdfjs: three fixtures own widgets with no usable /AcroForm entry, and a
//     merge rebuilds the form for them.
func TestMergeFeature_Compare_PreservedFormIsNotReported(t *testing.T) {
	t.Parallel()
	f, out := mergeCatalogFixture(t, catalogFacts{InFormFields: true, OutAcroForm: true})
	v := f.Compare("doc", out)
	if !v.Match {
		t.Fatalf("a preserved form was reported as a violation: %+v", v)
	}
}
