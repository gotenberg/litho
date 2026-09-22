package main

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/pdf"
)

// catalogFacts carries the catalog-level reads an output-correctness feature
// takes from both sides of a rewrite.
//
// The split and merge features compare page counts, page text and bookmarks,
// and none of that reopens the output CATALOG, so a document-level entry can
// be dropped or invented without any cell moving. These fields close that gap
// for both verbs from one place.
//
// Embedded anonymously, so the JSON stays flat and a feature's output shape is
// unchanged by adopting it.
type catalogFacts struct {
	InAcroForm   bool `json:"in_acroform"`
	OutAcroForm  bool `json:"out_acroform"`
	InPortfolio  bool `json:"in_portfolio"`
	OutPortfolio bool `json:"out_portfolio"`
	InLexicons   int  `json:"in_lexicons"`
	OutLexicons  int  `json:"out_lexicons"`
}

// readCatalogFacts reads both sides of a rewrite.
//
// Errors are not fatal: a reader that declines to answer leaves the field
// false or zero, which [checkCatalogInvariants] treats as "nothing to assert"
// rather than as a violation. That matches how feature_metadata.go reads
// IsPortfolio.
func readCatalogFacts(ctx context.Context, src, out *pdf.Reader) catalogFacts {
	var f catalogFacts
	f.InAcroForm = src.HasAcroForm(ctx)
	f.OutAcroForm = out.HasAcroForm(ctx)
	if v, err := src.IsPortfolio(ctx); err == nil {
		f.InPortfolio = v
	}
	if v, err := out.IsPortfolio(ctx); err == nil {
		f.OutPortfolio = v
	}
	if lx, err := src.ExtractPronunciationLexicons(ctx); err == nil {
		f.InLexicons = len(lx)
	}
	if lx, err := out.ExtractPronunciationLexicons(ctx); err == nil {
		f.OutLexicons = len(lx)
	}
	return f
}

// checkCatalogInvariants enforces the contract copyCatalogEntries states for
// itself: page-independent catalog entries describe the document, so a subset
// or a concatenation of its pages still carries them. verb names the operation
// in the failure message. Returns "" when every invariant holds.
//
// These are invariants rather than a catalog diff because both verbs
// legitimately drop page-KEYED entries (/StructTreeRoot, /PageLabels), so an
// exact catalog comparison would fail every tagged fixture by design.
//
// Both verbs carry catalog entries from one source: split from its only input,
// merge from sources[0] (merge.go:137). A merge of a document with itself
// therefore carries these entries ONCE, not twice, so the same comparisons
// hold for both. The doubling that a self-merge does cause belongs to the
// /EmbeddedFiles name tree, which these invariants do not read.
func checkCatalogInvariants(verb string, got catalogFacts) string {
	// An input with no interactive form must not acquire one. A markup
	// annotation carries /T as its pop-up title (ISO 32000-1, Table 170) while
	// a form field carries /T as its partial name (Table 220), so a field
	// collector keying on /T alone writes an /AcroForm onto documents that
	// never had a form.
	if !got.InAcroForm && got.OutAcroForm {
		return fmt.Sprintf("%s invented an /AcroForm on an input that carried none", verb)
	}
	if got.InPortfolio && !got.OutPortfolio {
		return fmt.Sprintf(
			"%s dropped /Collection, so the output holds the embedded files while denying it is a portfolio",
			verb)
	}
	if got.OutLexicons < got.InLexicons {
		return fmt.Sprintf("%s dropped /PronunciationLexicon entries: input %d, output %d",
			verb, got.InLexicons, got.OutLexicons)
	}
	return ""
}

// catalogInvariantMetrics reports how often each invariant was REACHABLE, not
// only how often it fired.
//
// A metric emitted only on violation sits at zero on a clean run and then
// disappears from the baseline entirely, which deletes its own bar: the next
// run cannot tell "measured, zero violations" from "no longer measured". The
// *_checked counters keep the coverage visible, so a corpus that carries no
// portfolio at all reads as untested rather than clean.
//
// The violation counters are registered in costMetrics (baseline.go) because
// more is worse. Their *_checked siblings are deliberately not, since a drop
// there means fewer fixtures exercised.
func catalogInvariantMetrics(got catalogFacts) map[string]int {
	m := map[string]int{
		"acroform_checked":  1,
		"acroform_invented": 0,
		"portfolio_checked": 0,
		"portfolio_dropped": 0,
		"lexicons_checked":  0,
		"lexicons_dropped":  0,
	}
	if !got.InAcroForm && got.OutAcroForm {
		m["acroform_invented"] = 1
	}
	if got.InPortfolio {
		m["portfolio_checked"] = 1
		if !got.OutPortfolio {
			m["portfolio_dropped"] = 1
		}
	}
	if got.InLexicons > 0 {
		m["lexicons_checked"] = 1
		if got.OutLexicons < got.InLexicons {
			m["lexicons_dropped"] = got.InLexicons - got.OutLexicons
		}
	}
	return m
}
