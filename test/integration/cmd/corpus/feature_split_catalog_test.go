package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCheckCatalogInvariants_CleanIsSilent pins the default shape. Every
// fixture that carries no form, no portfolio and no lexicon leaves these
// fields at their zero values, and a zero-valued document must not read as a
// violation: otherwise the invariant would fail the whole corpus on its first
// run.
func TestCheckCatalogInvariants_CleanIsSilent(t *testing.T) {
	t.Parallel()
	if reason := checkCatalogInvariants("split", catalogFacts{}); reason != "" {
		t.Errorf("zero value: got %q, want empty", reason)
	}
}

// TestCheckCatalogInvariants_RejectsInventedAcroForm covers the defect this
// invariant exists for. /T is the pop-up title on a markup annotation
// (ISO 32000-1, Table 170) and the partial field name on a form field
// (Table 220), so a collector keying on /T alone turns an annotated page into
// a form and writes an /AcroForm onto a document that never had one.
//
// The input here owns no field and carries no XFA, which is the only state in
// which a form can be said to have been invented.
func TestCheckCatalogInvariants_RejectsInventedAcroForm(t *testing.T) {
	t.Parallel()
	reason := checkCatalogInvariants("split", catalogFacts{
		InFormFields: false,
		InXFA:        false,
		OutAcroForm:  true,
	})
	if !strings.Contains(reason, "invented an /AcroForm") {
		t.Errorf("got %q, want an 'invented an /AcroForm' reason", reason)
	}
}

// TestCheckCatalogInvariants_OrphanedWidgetsAreRepairNotInvention is the first
// of three regression controls for false positives this predicate produced
// while it asked HasAcroForm instead of HasFormFields.
//
// A document can own real fields that no /AcroForm lists. Measured on pdfjs:
// issue12963 carries 27 such widgets with no /AcroForm at all, issue14203 has
// 10 parentless widgets with /T and /FT, and issue15597 spells the key /Field
// so /Fields is absent and HasAcroForm is correctly false. Rebuilding the form
// for fields the document already owns is repair.
func TestCheckCatalogInvariants_OrphanedWidgetsAreRepairNotInvention(t *testing.T) {
	t.Parallel()
	reason := checkCatalogInvariants("merge", catalogFacts{
		InFormFields: true,
		InXFA:        false,
		OutAcroForm:  true,
	})
	if reason != "" {
		t.Errorf("orphaned-widget repair reported as a violation: %q", reason)
	}
}

// TestCheckCatalogInvariants_XFADowngradeIsNotInvention is the second control.
//
// HasAcroForm returns false for a dynamic XFA document because its catalog
// sets /NeedsRendering and the AcroForm there wraps XFA rather than describing
// widgets. A merge keeps the widgets and drops the XFA layer, so the output
// reports a form the input appeared not to have. That is the documented
// downgrade [pdf.Reader.HasXFAForm] exists to name, not an invention.
// Measured on pdfjs xfa_filled_imm1344e, xfa_issue13855 and xfa_issue14071.
func TestCheckCatalogInvariants_XFADowngradeIsNotInvention(t *testing.T) {
	t.Parallel()
	reason := checkCatalogInvariants("merge", catalogFacts{
		InFormFields: false,
		InXFA:        true,
		OutAcroForm:  true,
	})
	if reason != "" {
		t.Errorf("XFA downgrade reported as an invented form: %q", reason)
	}
}

// TestCheckCatalogInvariants_NamesTheVerb pins the verb in the message. Both
// split and merge report through this predicate, and a failure line that named
// the wrong operation would send a reader to the wrong code path.
func TestCheckCatalogInvariants_NamesTheVerb(t *testing.T) {
	t.Parallel()
	facts := catalogFacts{InFormFields: false, InXFA: false, OutAcroForm: true}
	if r := checkCatalogInvariants("merge", facts); !strings.HasPrefix(r, "merge ") {
		t.Errorf("merge reason: got %q", r)
	}
	if r := checkCatalogInvariants("split", facts); !strings.HasPrefix(r, "split ") {
		t.Errorf("split reason: got %q", r)
	}
}

// TestCheckCatalogInvariants_AllowsFormWithNoSurvivingWidget documents a case
// that is deliberately NOT an invariant, so a later reader does not "complete"
// the set by adding it.
//
// rebuildAcroForm re-attaches the form to the fields whose widgets survived.
// Splitting out pages that carry no widget correctly yields no /AcroForm, so
// asserting In implies Out would fail honest fixtures.
func TestCheckCatalogInvariants_AllowsFormWithNoSurvivingWidget(t *testing.T) {
	t.Parallel()
	reason := checkCatalogInvariants("split", catalogFacts{
		InFormFields: true,
		OutAcroForm:  false,
	})
	if reason != "" {
		t.Errorf("form with no surviving widget: got %q, want empty", reason)
	}
}

// TestCheckCatalogInvariants_RejectsDroppedPortfolio pins /Collection. The
// embedded files travel through rebuildEmbeddedFiles independently, so
// dropping only the marker leaves an output holding the contents of a
// container while denying it is one.
func TestCheckCatalogInvariants_RejectsDroppedPortfolio(t *testing.T) {
	t.Parallel()
	reason := checkCatalogInvariants("split", catalogFacts{InPortfolio: true, OutPortfolio: false})
	if !strings.Contains(reason, "/Collection") {
		t.Errorf("got %q, want a '/Collection' reason", reason)
	}
}

// TestCheckCatalogInvariants_RejectsDroppedLexicons pins
// /PronunciationLexicon, which describes the whole document's pronunciation
// (ISO 32000-2, 14.9.6) and is named by no page.
//
// The count comparison holds for merge as well as split: merge carries catalog
// entries from sources[0] only (merge.go:137), so a self-merge carries them
// once rather than doubling them.
func TestCheckCatalogInvariants_RejectsDroppedLexicons(t *testing.T) {
	t.Parallel()
	reason := checkCatalogInvariants("split", catalogFacts{InLexicons: 3, OutLexicons: 1})
	if !strings.Contains(reason, "input 3, output 1") {
		t.Errorf("got %q, want the input/output counts", reason)
	}
	if reason := checkCatalogInvariants("split", catalogFacts{InLexicons: 2, OutLexicons: 2}); reason != "" {
		t.Errorf("preserved lexicons: got %q, want empty", reason)
	}
}

// TestCatalogInvariantMetrics_KeysSurviveACleanRun is the guard against the
// vanishing-bar trap: a metric emitted only when it fires sits at zero on a
// clean run, drops out of the baseline, and the next run cannot distinguish
// "measured, no violations" from "no longer measured". Every key must be
// present even when nothing fired.
func TestCatalogInvariantMetrics_KeysSurviveACleanRun(t *testing.T) {
	t.Parallel()
	m := catalogInvariantMetrics(catalogFacts{})
	for _, key := range []string{
		"acroform_checked", "acroform_invented",
		"portfolio_checked", "portfolio_dropped",
		"lexicons_checked", "lexicons_dropped",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("metric %q absent on a clean run, so its bar disappears", key)
		}
	}
	if m["acroform_invented"] != 0 {
		t.Errorf("acroform_invented = %d on a clean run", m["acroform_invented"])
	}
}

// TestCatalogInvariantMetrics_AgreeWithThePredicate keeps the counter and the
// verdict from drifting apart: every shape that the predicate clears must
// report zero inventions, and the one it rejects must report one.
func TestCatalogInvariantMetrics_AgreeWithThePredicate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		facts catalogFacts
		want  int
	}{
		{"invented", catalogFacts{OutAcroForm: true}, 1},
		{"orphaned widgets", catalogFacts{InFormFields: true, OutAcroForm: true}, 0},
		{"xfa downgrade", catalogFacts{InXFA: true, OutAcroForm: true}, 0},
		{"no form either side", catalogFacts{}, 0},
	}
	for _, tc := range cases {
		m := catalogInvariantMetrics(tc.facts)
		reason := checkCatalogInvariants("merge", tc.facts)
		if m["acroform_invented"] != tc.want {
			t.Errorf("%s: acroform_invented = %d, want %d", tc.name, m["acroform_invented"], tc.want)
		}
		if (reason != "") != (tc.want == 1) {
			t.Errorf("%s: predicate says %q but metric says %d", tc.name, reason, m["acroform_invented"])
		}
	}
}

// TestCatalogInvariantMetrics_ReachabilityIsSeparateFromViolation checks the
// *_checked counters report whether the corpus could exercise the invariant at
// all. No corpus file carries /Collection, so portfolio_dropped=0 must read as
// "never tested" rather than as "tested and clean".
func TestCatalogInvariantMetrics_ReachabilityIsSeparateFromViolation(t *testing.T) {
	t.Parallel()
	none := catalogInvariantMetrics(catalogFacts{})
	if none["portfolio_checked"] != 0 {
		t.Errorf("portfolio_checked = %d with no portfolio present", none["portfolio_checked"])
	}

	held := catalogInvariantMetrics(catalogFacts{InPortfolio: true, OutPortfolio: true})
	if held["portfolio_checked"] != 1 || held["portfolio_dropped"] != 0 {
		t.Errorf("portfolio kept: checked=%d dropped=%d, want 1 and 0",
			held["portfolio_checked"], held["portfolio_dropped"])
	}

	lost := catalogInvariantMetrics(catalogFacts{InPortfolio: true, OutPortfolio: false})
	if lost["portfolio_checked"] != 1 || lost["portfolio_dropped"] != 1 {
		t.Errorf("portfolio lost: checked=%d dropped=%d, want 1 and 1",
			lost["portfolio_checked"], lost["portfolio_dropped"])
	}

	lex := catalogInvariantMetrics(catalogFacts{InLexicons: 4, OutLexicons: 1})
	if lex["lexicons_checked"] != 1 || lex["lexicons_dropped"] != 3 {
		t.Errorf("lexicons: checked=%d dropped=%d, want 1 and 3",
			lex["lexicons_checked"], lex["lexicons_dropped"])
	}
}

// TestSplitFeature_Compare_CatalogViolationFailsTheFixture walks the whole
// Compare path, not just the predicate: page count, text and bookmarks all
// agree, so only the catalog check can produce the mismatch.
func TestSplitFeature_Compare_CatalogViolationFailsTheFixture(t *testing.T) {
	t.Parallel()
	f := newSplitFeature()
	out, _ := json.Marshal(splitOutput{
		InputPages:   2,
		NumPages:     2,
		InputFirst:   "First",
		InputLast:    "Last",
		OutFirst:     "First",
		OutLast:      "Last",
		catalogFacts: catalogFacts{OutAcroForm: true},
	})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if !strings.Contains(v.Reason, "invented an /AcroForm") {
		t.Errorf("reason: got %q", v.Reason)
	}
	if v.Metrics["acroform_invented"] != 1 {
		t.Errorf("acroform_invented = %d, want 1", v.Metrics["acroform_invented"])
	}
}

// TestSplitFeature_Compare_CleanFixtureStillCarriesMetrics makes sure the
// happy path reports the counters too. Without this, the metrics would exist
// only on failing fixtures and the coverage numbers would be meaningless.
func TestSplitFeature_Compare_CleanFixtureStillCarriesMetrics(t *testing.T) {
	t.Parallel()
	f := newSplitFeature()
	out, _ := json.Marshal(splitOutput{
		InputPages: 2,
		NumPages:   2,
		InputFirst: "First",
		InputLast:  "Last",
		OutFirst:   "First",
		OutLast:    "Last",
	})
	v := f.Compare("doc", out)
	if !v.Match {
		t.Fatalf("expected match, got %+v", v)
	}
	if _, ok := v.Metrics["acroform_checked"]; !ok {
		t.Error("a matching fixture reported no acroform_checked metric")
	}
}

// TestSplitFeature_Compare_EarlyFailureCarriesNoMetrics pins the counting
// rule: acroform_checked means the catalog invariant RAN. A fixture that
// failed page count never reached it, so counting it as checked would inflate
// the coverage number the *_checked counters exist to report honestly.
func TestSplitFeature_Compare_EarlyFailureCarriesNoMetrics(t *testing.T) {
	t.Parallel()
	f := newSplitFeature()
	out, _ := json.Marshal(splitOutput{InputPages: 3, NumPages: 3})
	v := f.Compare("doc", out)
	if v.Match {
		t.Fatalf("expected mismatch, got %+v", v)
	}
	if len(v.Metrics) != 0 {
		t.Errorf("page-count failure reported metrics %v; the catalog was never compared", v.Metrics)
	}
}
