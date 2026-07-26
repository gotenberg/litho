package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
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

	// PDF/A-2 emits its own ids for every test, t3 included: ISO 19005-2
	// §6.2.11.2 t3 states the same requirement as ISO 19005-1 §6.3.2 t3.
	if got := chk(ast.PdfDict{"Subtype": ast.PdfName("Type1")}, "Type1", 2); !hasRule(got, Rule_19005_2_6_2_11_2_t01) {
		t.Errorf("A-2 missing /Type: got %v, want §6.2.11.2 t1", got)
	}
	if got := chk(ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1")}, "Type1", 2); hasRule(got, Rule_19005_1_6_3_2_t03) {
		t.Errorf("A-2 missing /BaseFont wrongly emitted PDF/A-1 t3: %v", got)
	}
	if got := chk(ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1")}, "Type1", 2); !hasRule(got, Rule_19005_2_6_2_11_2_t03) {
		t.Errorf("A-2 missing /BaseFont: got %v, want §6.2.11.2 t3", got)
	}
	if got := chk(ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type3")}, "Type3", 2); hasRule(got, Rule_19005_2_6_2_11_2_t03) {
		t.Errorf("A-2 Type3 is exempt from the name requirement: %v", got)
	}

	// Non-PDF/A (part 0) is not checked.
	if got := chk(ast.PdfDict{}, "", 0); len(got) != 0 {
		t.Errorf("part 0: got %v, want none", got)
	}
}

func TestFontFileSubtype(t *testing.T) {
	ctx := context.Background()
	mk := func(ffSubtype string) (ast.PdfDict, *document.Document) {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /FontDescriptor /FontName /X /FontFile3 5 0 R >>",
			"<< /Subtype /"+ffSubtype+" /Length 3 >>\nstream\nabc\nendstream",
		)
		font := ast.PdfDict{
			"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"),
			"BaseFont": ast.PdfName("X"), "FontDescriptor": ref(4),
		}
		return font, doc
	}

	// PDF/A-1 forbids an OpenType FontFile3; PDF/A-2 permits it.
	font, doc := mk("OpenType")
	if got := checkFontRequiredKeys(ctx, doc, font, "Type1", 1); !hasRule(got, Rule_19005_1_6_3_2_t07) {
		t.Errorf("A-1 OpenType FontFile3: got %v, want §6.3.2 t7", got)
	}
	if got := checkFontRequiredKeys(ctx, doc, font, "Type1", 2); hasRule(got, Rule_19005_2_6_2_11_2_t07) {
		t.Errorf("A-2 OpenType FontFile3: got %v, want none", got)
	}

	// Type1C is permitted for every part.
	fontC, docC := mk("Type1C")
	if got := checkFontRequiredKeys(ctx, docC, fontC, "Type1", 1); hasRule(got, Rule_19005_1_6_3_2_t07) {
		t.Errorf("Type1C FontFile3: got %v, want none", got)
	}
}

func TestSimpleFontRequiredKeys(t *testing.T) {
	ctx := context.Background()
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	chk := func(fd ast.PdfDict, part int) []Violation { return checkSimpleFontRequiredKeys(ctx, doc, fd, part) }

	// A non-standard simple font missing /FirstChar, /LastChar, /Widths fails
	// §6.2.11.2 t4/t5/t6.
	bare := ast.PdfDict{"BaseFont": ast.PdfName("ABCDEF+Custom")}
	got := chk(bare, 2)
	for _, r := range []Rule{Rule_19005_2_6_2_11_2_t04, Rule_19005_2_6_2_11_2_t05, Rule_19005_2_6_2_11_2_t06} {
		if !hasRule(got, r) {
			t.Errorf("bare simple font: missing %+v in %v", r, got)
		}
	}

	// A /Widths array of the wrong size fails t6.
	wrongSize := ast.PdfDict{
		"BaseFont": ast.PdfName("X"), "FirstChar": ast.PdfInt(0), "LastChar": ast.PdfInt(2),
		"Widths": ast.PdfArray{ast.PdfInt(100)},
	}
	if got := chk(wrongSize, 2); !hasRule(got, Rule_19005_2_6_2_11_2_t06) {
		t.Errorf("wrong /Widths size: got %v, want §6.2.11.2 t6", got)
	}

	// A correctly-sized /Widths array (LastChar - FirstChar + 1) is conformant.
	okFont := ast.PdfDict{
		"BaseFont": ast.PdfName("X"), "FirstChar": ast.PdfInt(0), "LastChar": ast.PdfInt(2),
		"Widths": ast.PdfArray{ast.PdfInt(100), ast.PdfInt(200), ast.PdfInt(300)},
	}
	if got := chk(okFont, 2); len(got) != 0 {
		t.Errorf("conformant simple font: got %v, want none", got)
	}

	// The standard 14 fonts are Type1 programs; a Type1 font with a standard
	// name is exempt from FirstChar/LastChar/Widths.
	if got := chk(ast.PdfDict{"Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("Helvetica")}, 2); len(got) != 0 {
		t.Errorf("standard-14 Type1 font: got %v, want none (exempt)", got)
	}

	// A /TrueType font merely sharing a standard name is NOT a standard-14 font
	// (those are Type1), so it must carry /FirstChar, /LastChar, and /Widths,
	// matching veraPDF's isStandard (PDType1Font) escape.
	if got := chk(ast.PdfDict{"Subtype": ast.PdfName("TrueType"), "BaseFont": ast.PdfName("Helvetica")}, 2); !hasRule(got, Rule_19005_2_6_2_11_2_t04) {
		t.Errorf("TrueType standard-named font: want §6.2.11.2 t4, got %v", got)
	}

	// Non-PDF/A (part 0) is not checked.
	if got := chk(bare, 0); len(got) != 0 {
		t.Errorf("part 0: got %v, want none", got)
	}
}
