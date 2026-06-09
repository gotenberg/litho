package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestFontRequiredKeys(t *testing.T) {
	ctx := context.Background()
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	chk := func(fd ast.PdfDict, subtype string, part int) []Violation {
		return checkFontRequiredKeys(ctx, doc, fd, subtype, part)
	}

	// §6.3.2 t1: /Type must be Font.
	if got := chk(ast.PdfDict{"Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("X")}, "Type1", 1); !hasRule(got, Rule_19005_1_6_3_2_t01) {
		t.Errorf("missing /Type: got %v, want §6.3.2 t1", got)
	}
	// §6.3.2 t2: invalid /Subtype.
	if got := chk(ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Bogus"), "BaseFont": ast.PdfName("X")}, "Bogus", 1); !hasRule(got, Rule_19005_1_6_3_2_t02) {
		t.Errorf("invalid /Subtype: got %v, want §6.3.2 t2", got)
	}
	// §6.3.2 t3: missing /BaseFont on a non-Type3 font.
	if got := chk(ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1")}, "Type1", 1); !hasRule(got, Rule_19005_1_6_3_2_t03) {
		t.Errorf("missing /BaseFont: got %v, want §6.3.2 t3", got)
	}
	// Type 3 fonts are exempt from /BaseFont.
	if got := chk(ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type3")}, "Type3", 1); hasRule(got, Rule_19005_1_6_3_2_t03) {
		t.Errorf("Type3 without /BaseFont wrongly flagged: %v", got)
	}

	// A conformant font dict yields no violations.
	good := ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("Helvetica")}
	if got := chk(good, "Type1", 1); len(got) != 0 {
		t.Errorf("conformant font: got %v, want none", got)
	}

	// PDF/A-2 emits its own t1/t2 ids; t3/BaseFont is PDF/A-1 only.
	if got := chk(ast.PdfDict{"Subtype": ast.PdfName("Type1")}, "Type1", 2); !hasRule(got, Rule_19005_2_6_2_11_2_t01) {
		t.Errorf("A-2 missing /Type: got %v, want §6.2.11.2 t1", got)
	}
	if got := chk(ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1")}, "Type1", 2); hasRule(got, Rule_19005_1_6_3_2_t03) {
		t.Errorf("A-2 missing /BaseFont wrongly emitted PDF/A-1 t3: %v", got)
	}

	// Non-PDF/A (part 0) is not checked.
	if got := chk(ast.PdfDict{}, "", 0); len(got) != 0 {
		t.Errorf("part 0: got %v, want none", got)
	}
}
