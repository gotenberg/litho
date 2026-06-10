package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// fontResourcePDF builds a single-page PDF whose page references one font (object
// 4) through its /Resources /Font dictionary, plus any extra objects (object 5+).
func fontResourcePDF(font string, extra ...string) []byte {
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> >>",
		font,
	}
	bodies = append(bodies, extra...)
	return buildStructPDF(bodies...)
}

func TestMatchFontKeys(t *testing.T) {
	t.Parallel()
	// /Type (t1) across parts, and /BaseFont (t3, PDF/A-1 only).
	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_1_6_3_2_t01,
		compliance.Rule_19005_2_6_2_11_2_t01,
		compliance.Rule_19005_3_6_2_11_2_t01,
		compliance.Rule_19005_4_6_2_10_2_t01,
		compliance.Rule_19005_1_6_3_2_t03,
	} {
		if !matchFontKeys(compliance.Violation{Rule: rule}) {
			t.Errorf("must match font rule %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
	// Must NOT claim a PDF/A-2 §6.3.2 annotation violation (clause collision).
	if matchFontKeys(compliance.Violation{Rule: compliance.Rule_19005_2_6_3_2_t01}) {
		t.Error("must not claim the PDF/A-2 §6.3.2 annotation rule")
	}
}

func TestFixFontKeys_AddsTypeAndBaseFont(t *testing.T) {
	// Font missing both /Type and /BaseFont, with a FontDescriptor carrying /FontName.
	pdf := fontResourcePDF(
		"<< /Subtype /TrueType /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /ABCDEF+Sans >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFontKeys(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1")
	if err != nil {
		t.Fatalf("fixFontKeys: %v", err)
	}
	if len(descs) != 2 {
		t.Fatalf("expected 2 fix descriptions, got %v", descs)
	}
	patched := pendingDict(t, alloc, 4)
	if patched["Type"] != ast.PdfName("Font") {
		t.Errorf("/Type = %v, want Font", patched["Type"])
	}
	if patched["BaseFont"] != ast.PdfName("ABCDEF+Sans") {
		t.Errorf("/BaseFont = %v, want ABCDEF+Sans", patched["BaseFont"])
	}
}

func TestFixFontKeys_NoBaseFontWithoutFontName(t *testing.T) {
	// Missing /BaseFont and no FontDescriptor /FontName: /BaseFont must NOT be
	// synthesised (only /Type is added).
	pdf := fontResourcePDF("<< /Subtype /TrueType >>")
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixFontKeys(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1"); err != nil {
		t.Fatalf("fixFontKeys: %v", err)
	}
	patched := pendingDict(t, alloc, 4)
	if patched["Type"] != ast.PdfName("Font") {
		t.Errorf("/Type = %v, want Font", patched["Type"])
	}
	if _, has := patched["BaseFont"]; has {
		t.Errorf("/BaseFont must not be synthesised, got %v", patched["BaseFont"])
	}
}

func TestFixFontKeys_Type3ExemptFromBaseFont(t *testing.T) {
	// A Type 3 font does not require /BaseFont; only /Type is added.
	pdf := fontResourcePDF("<< /Subtype /Type3 /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /X >>")
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixFontKeys(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1"); err != nil {
		t.Fatalf("fixFontKeys: %v", err)
	}
	patched := pendingDict(t, alloc, 4)
	if patched["Type"] != ast.PdfName("Font") {
		t.Errorf("/Type = %v, want Font", patched["Type"])
	}
	if _, has := patched["BaseFont"]; has {
		t.Errorf("Type3 /BaseFont must not be added, got %v", patched["BaseFont"])
	}
}

func TestFixFontKeys_ConformantFontUntouched(t *testing.T) {
	// A font that already has /Type and /BaseFont is not modified (no pending edit).
	pdf := fontResourcePDF("<< /Type /Font /Subtype /TrueType /BaseFont /X >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFontKeys(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1")
	if err != nil {
		t.Fatalf("fixFontKeys: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("conformant font: got %v, want no fixes", descs)
	}
	if _, queued := alloc.Pending()[4]; queued {
		t.Error("conformant font must not be queued for replacement")
	}
}
