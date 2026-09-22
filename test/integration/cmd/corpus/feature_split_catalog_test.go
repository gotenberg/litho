package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCheckSplitCatalog_CleanIsSilent pins the default shape. Every fixture
// that carries no form, no portfolio and no lexicon leaves these fields at
// their zero values, and a zero-valued document must not read as a violation:
// otherwise the invariant would fail the whole corpus on its first run.
func TestCheckSplitCatalog_CleanIsSilent(t *testing.T) {
	t.Parallel()
	if reason := checkSplitCatalog(splitOutput{}); reason != "" {
		t.Errorf("zero value: got %q, want empty", reason)
	}
}

// TestCheckSplitCatalog_RejectsInventedAcroForm covers the defect this
// invariant exists for. /T is the pop-up title on a markup annotation
// (ISO 32000-1, Table 170) and the partial field name on a form field
// (Table 220), so a collector keying on /T alone turns an annotated page into
// a form and writes an /AcroForm onto a document that never had one.
func TestCheckSplitCatalog_RejectsInventedAcroForm(t *testing.T) {
	t.Parallel()
	reason := checkSplitCatalog(splitOutput{InAcroForm: false, OutAcroForm: true})
	if !strings.Contains(reason, "invented an /AcroForm") {
		t.Errorf("got %q, want an 'invented an /AcroForm' reason", reason)
	}
}

// TestCheckSplitCatalog_AllowsPreservedForm is the control for the test above.
// A genuine form surviving the split must not be reported.
func TestCheckSplitCatalog_AllowsPreservedForm(t *testing.T) {
	t.Parallel()
	if reason := checkSplitCatalog(splitOutput{InAcroForm: true, OutAcroForm: true}); reason != "" {
		t.Errorf("preserved form: got %q, want empty", reason)
	}
}

// TestCheckSplitCatalog_AllowsFormWithNoSurvivingWidget documents a case that
// is deliberately NOT an invariant, so a later reader does not "complete" the
// set by adding it.
//
// rebuildAcroForm re-attaches the form to the fields whose widgets survived.
// Splitting out pages that carry no widget correctly yields no /AcroForm, so
// asserting In implies Out would fail honest fixtures.
func TestCheckSplitCatalog_AllowsFormWithNoSurvivingWidget(t *testing.T) {
	t.Parallel()
	if reason := checkSplitCatalog(splitOutput{InAcroForm: true, OutAcroForm: false}); reason != "" {
		t.Errorf("form with no surviving widget: got %q, want empty", reason)
	}
}

// TestCheckSplitCatalog_RejectsDroppedPortfolio pins /Collection. The embedded
// files travel through rebuildEmbeddedFiles independently, so dropping only
// the marker leaves an output holding the contents of a container while
// denying it is one.
func TestCheckSplitCatalog_RejectsDroppedPortfolio(t *testing.T) {
	t.Parallel()
	reason := checkSplitCatalog(splitOutput{InPortfolio: true, OutPortfolio: false})
	if !strings.Contains(reason, "/Collection") {
		t.Errorf("got %q, want a '/Collection' reason", reason)
	}
}

// TestCheckSplitCatalog_RejectsDroppedLexicons pins /PronunciationLexicon,
// which describes the whole document's pronunciation (ISO 32000-2, 14.9.6) and
// is named by no page.
func TestCheckSplitCatalog_RejectsDroppedLexicons(t *testing.T) {
	t.Parallel()
	reason := checkSplitCatalog(splitOutput{InLexicons: 3, OutLexicons: 1})
	if !strings.Contains(reason, "input 3, output 1") {
		t.Errorf("got %q, want the input/output counts", reason)
	}
	if reason := checkSplitCatalog(splitOutput{InLexicons: 2, OutLexicons: 2}); reason != "" {
		t.Errorf("preserved lexicons: got %q, want empty", reason)
	}
}

// TestSplitCatalogMetrics_KeysSurviveACleanRun is the guard against the
// vanishing-bar trap: a metric emitted only when it fires sits at zero on a
// clean run, drops out of the baseline, and the next run cannot distinguish
// "measured, no violations" from "no longer measured". Every key must be
// present even when nothing fired.
func TestSplitCatalogMetrics_KeysSurviveACleanRun(t *testing.T) {
	t.Parallel()
	m := splitCatalogMetrics(splitOutput{})
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

// TestSplitCatalogMetrics_ReachabilityIsSeparateFromViolation checks the
// *_checked counters report whether the corpus could exercise the invariant at
// all. No corpus file carries /Collection, so portfolio_dropped=0 must read as
// "never tested" rather than as "tested and clean".
func TestSplitCatalogMetrics_ReachabilityIsSeparateFromViolation(t *testing.T) {
	t.Parallel()
	none := splitCatalogMetrics(splitOutput{})
	if none["portfolio_checked"] != 0 {
		t.Errorf("portfolio_checked = %d with no portfolio present", none["portfolio_checked"])
	}

	held := splitCatalogMetrics(splitOutput{InPortfolio: true, OutPortfolio: true})
	if held["portfolio_checked"] != 1 || held["portfolio_dropped"] != 0 {
		t.Errorf("portfolio kept: checked=%d dropped=%d, want 1 and 0",
			held["portfolio_checked"], held["portfolio_dropped"])
	}

	lost := splitCatalogMetrics(splitOutput{InPortfolio: true, OutPortfolio: false})
	if lost["portfolio_checked"] != 1 || lost["portfolio_dropped"] != 1 {
		t.Errorf("portfolio lost: checked=%d dropped=%d, want 1 and 1",
			lost["portfolio_checked"], lost["portfolio_dropped"])
	}

	lex := splitCatalogMetrics(splitOutput{InLexicons: 4, OutLexicons: 1})
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
		InputPages:  2,
		NumPages:    2,
		InputFirst:  "First",
		InputLast:   "Last",
		OutFirst:    "First",
		OutLast:     "Last",
		InAcroForm:  false,
		OutAcroForm: true,
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
