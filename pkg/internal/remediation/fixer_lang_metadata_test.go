package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestRemediatePDFUA_CatalogLangHoldsAfterOneRound pins that one pass of the
// PDF/UA-1 remediation engine declares the catalog /Lang the text it writes
// needs, on a document whose source gives catalog-lang nothing to answer.
//
// The page holds no text and the document no XMP packet, so the source fails
// no ISO 14289-1, 7.2 test. xmp-metadata then generates a packet whose
// dc:title is a language alternative with an x-default entry, which 7.2 test
// 33 requires the catalog /Lang to determine. catalog-lang claimed the 7.2
// findings alone, so it did not run, and only a second remediation round, run
// on the first round's output, added the /Lang.
func TestRemediatePDFUA_CatalogLangHoldsAfterOneRound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))
	before, err := compliance.ValidatePDFUA(ctx, doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	answered := false
	for _, v := range toViolations(before) {
		if v.Rule.Clause == "7.2" {
			t.Fatalf("precondition: the source already fails %s t%d, so catalog-lang runs on its own findings", v.Rule.Clause, v.Rule.TestNumber)
		}
		answered = answered || matchXMP(v)
	}
	if !answered {
		t.Fatalf("precondition: no violation runs xmp-metadata, so no packet is generated; violations = %v", before)
	}

	if _, err := RemediatePDFUA(ctx, doc, alloc, models.PDFUA1, Options{}); err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	out := rewriteAndReopen(t, doc, alloc)
	errs, err := compliance.ValidatePDFUA(ctx, out, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA on the output: %v", err)
	}
	for _, v := range toViolations(errs) {
		if v.Rule.Clause == "7.2" {
			t.Errorf("after one round: %v", v)
		}
	}
	catalog, err := document.ResolveCatalog(ctx, out)
	if err != nil {
		t.Fatalf("resolving the output catalog: %v", err)
	}
	if lang, ok := catalog.Dict["Lang"].(ast.PdfString); !ok || document.DecodePdfString(lang) != "en" {
		t.Errorf("output catalog /Lang = %v, want (en)", catalog.Dict["Lang"])
	}
}

// TestMatchCatalogLang_ClaimsTheViolationsOfLanguageTextFixers pins the scope
// of the claim: under PDF/UA-1 catalog-lang runs wherever a fixer that writes
// language text does, and nowhere else it did not already. Under PDF/A and
// PDF/UA-2 it keeps its own clauses, and marked-content-lang, a content-tier
// fixer, never takes the wider claim.
func TestMatchCatalogLang_ClaimsTheViolationsOfLanguageTextFixers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		rule compliance.Rule
		want bool
	}{
		{compliance.Rule_14289_1_7_1_t08, true},                                                // no metadata stream: xmp-metadata
		{compliance.Rule_14289_1_7_3_t01, true},                                                // Figure without /Alt: figure-alt
		{compliance.Rule{Specification: "ISO_19005_1", Clause: "6.7.3", TestNumber: 2}, false}, // xmp-metadata under PDF/A
		{compliance.Rule{Specification: "ISO_14289_2", Clause: "8.11", TestNumber: 1}, false},  // xmp-metadata under PDF/UA-2
		{compliance.Rule{Specification: "ISO_14289_1", Clause: "7.21.8", TestNumber: 1}, false},
	} {
		v := compliance.Violation{Rule: tc.rule}
		if got := matchCatalogLang(v); got != tc.want {
			t.Errorf("matchCatalogLang(%s %s t%d) = %v, want %v", tc.rule.Specification, tc.rule.Clause, tc.rule.TestNumber, got, tc.want)
		}
		if tc.want && matchMarkedContentLang(v) {
			t.Errorf("marked-content-lang claims %s %s t%d, which only the catalog /Lang answers", tc.rule.Specification, tc.rule.Clause, tc.rule.TestNumber)
		}
	}
}
