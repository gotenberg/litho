package main

import "testing"

// TestGoldensCorpus_SkipsACorpusNothingGivesAPDF checks that the goldens step
// leaves a corpus without a fetch, committed PDFs or references alone: the
// image has no PDF to derive a golden from, and the corpus's features read
// what the build step wrote.
func TestGoldensCorpus_SkipsACorpusNothingGivesAPDF(t *testing.T) {
	t.Parallel()

	m := &Manifest{Name: "litho/stress", Inputs: &InputSpec{}}
	if hasPDFSource(m) {
		t.Fatal("hasPDFSource = true for a corpus with no fetch, committed PDFs or references")
	}
	if err := goldensCorpus(t.Context(), m, nil); err != nil {
		t.Errorf("goldensCorpus = %v, want nil: there is nothing to derive", err)
	}
	for name, with := range map[string]*Manifest{
		"committed PDFs": {CommittedPDFs: "pdfs"},
		"references":     {References: &ReferenceSpec{Dir: "references"}},
		"a fetch":        {Fetch: &FetchSpec{}},
	} {
		if !hasPDFSource(with) {
			t.Errorf("hasPDFSource = false for a corpus with %s", name)
		}
	}
}
