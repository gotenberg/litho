package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMergeCatalogFeature_Contract pins the three methods that let this
// feature be scheduled without any reference files. The runner drops a feature
// whose manifest golden path is empty, and LoadGoldens returning ErrNoGoldens
// would mark the cell "[no golden]" and keep it out of the baseline entirely.
func TestMergeCatalogFeature_Contract(t *testing.T) {
	t.Parallel()
	f := newMergeCatalogFeature()
	if f.Name() != "merge-catalog" {
		t.Errorf("Name = %q", f.Name())
	}
	if f.GoldenSource() != "litho-self" {
		t.Errorf("GoldenSource = %q, want litho-self", f.GoldenSource())
	}
	if err := f.LoadGoldens("ignored"); err != nil {
		t.Errorf("LoadGoldens: %v, want nil so the cell is scored", err)
	}
	if !f.HasGolden("anything") {
		t.Error("HasGolden returned false; every fixture is a candidate")
	}
}

// TestMergeCatalogFeature_Compare_SkippedPropagates: a document litho cannot
// merge is reported as Skipped by Run, and Compare must surface that rather
// than scoring it.
//
// This is the behaviour that keeps the cell meaningful. The runner turns a Run
// ERROR into a mismatch (run.go), so returning errors for unmergeable files
// would fill the cell with fixtures that never exercised the invariant.
func TestMergeCatalogFeature_Compare_SkippedPropagates(t *testing.T) {
	t.Parallel()
	f := newMergeCatalogFeature()
	out, _ := json.Marshal(mergeCatalogOutput{
		Skipped:    true,
		SkipReason: "litho cannot merge the input: boom",
	})
	v := f.Compare("doc", out)
	if !v.Skipped {
		t.Fatalf("Verdict: %+v, want Skipped", v)
	}
	if !strings.Contains(v.Reason, "cannot merge") {
		t.Errorf("reason: %q", v.Reason)
	}
	if len(v.Metrics) != 0 {
		t.Errorf("a skipped fixture reported metrics %v; it never ran the invariant", v.Metrics)
	}
}

// TestMergeCatalogFeature_Compare_PageCountWrong keeps the one structural
// check. A merge of a document with itself doubles its pages, and an output
// that failed that is not a document whose catalog says anything.
func TestMergeCatalogFeature_Compare_PageCountWrong(t *testing.T) {
	t.Parallel()
	f := newMergeCatalogFeature()
	out, _ := json.Marshal(mergeCatalogOutput{InputPages: 3, OutPages: 5})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "page count: got 5, want 6") {
		t.Errorf("reason: %q", v.Reason)
	}
	if len(v.Metrics) != 0 {
		t.Errorf("page-count failure reported metrics %v; the catalog was never compared", v.Metrics)
	}
}

// TestMergeCatalogFeature_Compare_CatalogViolationFails is the whole point of
// the feature: page count agrees, so only the catalog check can fail it. The
// input owns no field and carries no XFA, the only state in which a form can
// be said to have been invented.
func TestMergeCatalogFeature_Compare_CatalogViolationFails(t *testing.T) {
	t.Parallel()
	f := newMergeCatalogFeature()
	out, _ := json.Marshal(mergeCatalogOutput{
		InputPages:   1,
		OutPages:     2,
		catalogFacts: catalogFacts{OutAcroForm: true},
	})
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

// TestMergeCatalogFeature_Compare_RepairIsNotInvention is the control that
// this feature needs most, because it runs on a corpus that actually contains
// the shape.
//
// Measured on pdfjs: six fixtures reported a violation while the predicate
// asked HasAcroForm. Three own widgets that no usable /AcroForm lists
// (issue12963, issue14203, issue15597) and three are dynamic XFA
// (xfa_filled_imm1344e, xfa_issue13855, xfa_issue14071). Neither is a defect.
func TestMergeCatalogFeature_Compare_RepairIsNotInvention(t *testing.T) {
	t.Parallel()
	f := newMergeCatalogFeature()
	for _, tc := range []struct {
		name  string
		facts catalogFacts
	}{
		{"owns widgets no /AcroForm lists", catalogFacts{InFormFields: true, OutAcroForm: true}},
		{"dynamic XFA keeps its widgets", catalogFacts{InXFA: true, OutAcroForm: true}},
	} {
		out, _ := json.Marshal(mergeCatalogOutput{
			InputPages: 1, OutPages: 2, catalogFacts: tc.facts,
		})
		v := f.Compare("doc-"+tc.name, out)
		if !v.Match {
			t.Errorf("%s: reported as a violation: %s", tc.name, v.Reason)
		}
		if v.Metrics["acroform_invented"] != 0 {
			t.Errorf("%s: acroform_invented = %d", tc.name, v.Metrics["acroform_invented"])
		}
	}
}

// TestMergeCatalogFeature_Compare_SinglePageIsScored pins the reason this
// feature exists alongside the split one: a one-page document is merged
// against itself and fully scored, where feature_split skips it outright.
// Measured across the corpus, 71 of the 85 files carrying the triggering shape
// are single-page.
func TestMergeCatalogFeature_Compare_SinglePageIsScored(t *testing.T) {
	t.Parallel()
	f := newMergeCatalogFeature()
	out, _ := json.Marshal(mergeCatalogOutput{InputPages: 1, OutPages: 2})
	v := f.Compare("doc", out)
	if v.Skipped {
		t.Fatalf("single-page input was skipped: %+v", v)
	}
	if !v.Match {
		t.Fatalf("expected a clean single-page fixture to match, got %+v", v)
	}
	if v.Metrics["acroform_checked"] != 1 {
		t.Errorf("acroform_checked = %d, want 1", v.Metrics["acroform_checked"])
	}
}

// TestMergeCatalogFeature_Compare_CleanCarriesMetrics keeps the coverage
// counters honest on the happy path, so acroform_checked reports how many
// fixtures actually reached the invariant.
func TestMergeCatalogFeature_Compare_CleanCarriesMetrics(t *testing.T) {
	t.Parallel()
	f := newMergeCatalogFeature()
	out, _ := json.Marshal(mergeCatalogOutput{
		InputPages:   2,
		OutPages:     4,
		catalogFacts: catalogFacts{InFormFields: true, OutAcroForm: true},
	})
	v := f.Compare("doc", out)
	if !v.Match {
		t.Fatalf("expected match, got %+v", v)
	}
	if v.Metrics["acroform_checked"] != 1 {
		t.Errorf("acroform_checked = %d, want 1", v.Metrics["acroform_checked"])
	}
	if v.Metrics["acroform_invented"] != 0 {
		t.Errorf("acroform_invented = %d on a preserved form", v.Metrics["acroform_invented"])
	}
}
