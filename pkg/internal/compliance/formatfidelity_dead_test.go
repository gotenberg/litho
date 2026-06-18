package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestStreamEOLDiscriminator(t *testing.T) {
	eol := []string{
		"stream keyword followed by CR only (must be CR+LF or LF)",
		"stream keyword not followed by EOL",
	}
	for _, m := range eol {
		if !isStreamEOLViolation(m) {
			t.Errorf("isStreamEOLViolation(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"stream missing /Length key", "stream length does not match /Length"} {
		if isStreamEOLViolation(m) {
			t.Errorf("isStreamEOLViolation(%q) = true, want false (Length defect)", m)
		}
	}

	for profile, want := range map[string]Rule{
		"PDF/A-1b": Rule_19005_1_6_1_7_t02,
		"PDF/A-2b": Rule_19005_2_6_1_7_1_t02,
		"PDF/A-3b": Rule_19005_3_6_1_7_1_t02,
	} {
		if got := streamEOLRule(profile); got != want {
			t.Errorf("streamEOLRule(%s) = %+v, want %+v", profile, got, want)
		}
	}
	if got := streamEOLRule("PDF/A-4"); got.Specification != "" {
		t.Errorf("streamEOLRule(PDF/A-4) = %+v, want zero Rule", got)
	}
}

func TestMixedHeadingT03(t *testing.T) {
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	// Per ISO 14289-1 §7.4.4, a document uses either the generic [H] heading
	// or the numbered [H1]..[Hn] headings, not both. The first heading in
	// document order fixes the structure mode, so only the conflicting kind
	// offends and exactly one rule fires on it.
	//
	// Weak-first ([H, H1]): the numbered H1 offends => §7.4.4 t3 only.
	weakFirst := ast.PdfDict{"K": ast.PdfArray{
		ast.PdfDict{"S": ast.PdfName("H"), "P": ast.PdfName("x")},
		ast.PdfDict{"S": ast.PdfName("H1"), "P": ast.PdfName("x")},
	}}
	var weak []Violation
	walkStructureTree(context.Background(), doc, weakFirst, "PDF/UA-1", &weak)
	if !hasRule(weak, Rule_14289_1_7_4_4_t03) {
		t.Errorf("weak-first mixed headings: missing §7.4.4 t3: %v", weak)
	}
	if hasRule(weak, Rule_14289_1_7_4_4_t02) {
		t.Errorf("weak-first mixed headings: unexpected §7.4.4 t2: %v", weak)
	}

	// Strong-first ([H1, H]): the generic H offends => §7.4.4 t2 only.
	strongFirst := ast.PdfDict{"K": ast.PdfArray{
		ast.PdfDict{"S": ast.PdfName("H1"), "P": ast.PdfName("x")},
		ast.PdfDict{"S": ast.PdfName("H"), "P": ast.PdfName("x")},
	}}
	var strong []Violation
	walkStructureTree(context.Background(), doc, strongFirst, "PDF/UA-1", &strong)
	if !hasRule(strong, Rule_14289_1_7_4_4_t02) {
		t.Errorf("strong-first mixed headings: missing §7.4.4 t2: %v", strong)
	}
	if hasRule(strong, Rule_14289_1_7_4_4_t03) {
		t.Errorf("strong-first mixed headings: unexpected §7.4.4 t3: %v", strong)
	}
}
