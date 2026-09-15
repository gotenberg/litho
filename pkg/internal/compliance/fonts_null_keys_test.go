package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// nullKeyFontDoc builds a one-page document showing "AB" in a non-standard,
// non-embedded Type1 font whose dictionary carries fontExtra; object 6 is a
// ToUnicode CMap for codes A and B, and object 7 the null object.
func nullKeyFontDoc(t *testing.T, fontExtra string) []string {
	t.Helper()
	const body = "BT /F1 12 Tf 10 10 Td (AB) Tj ET"
	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /X def " +
		"1 begincodespacerange <00> <FF> endcodespacerange 2 beginbfchar <41> <0041> <42> <0042> endbfchar " +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Custom /FontDescriptor << /Type /FontDescriptor /FontName /Custom" +
			" /Flags 32 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>" +
			fontExtra + " >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
		"null",
	}
}

// TestFontFirstCharWrittenAsNull pins ISO 19005-1, 6.3.2 t4/t5 and ISO 19005-2,
// 6.2.11.2 t4/t5 on a non-standard simple font whose /FirstChar or /LastChar
// resolves to null: veraPDF reads the entry as an integer, so null is no entry
// (ISO 32000-1, 7.3.7 and 7.3.10) and the rule fires. Measured against veraPDF.
func TestFontFirstCharWrittenAsNull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name    string
		profile models.PDFAProfile
		extra   string
		rule    Rule
		want    int
	}{
		{"a /FirstChar written as null", models.PDFA1b, " /FirstChar null /LastChar 66 /Widths [500 500]", Rule_19005_1_6_3_2_t04, 1},
		{"a /FirstChar written as null, PDF/A-2", models.PDFA2b, " /FirstChar null /LastChar 66 /Widths [500 500]", Rule_19005_2_6_2_11_2_t04, 1},
		{"a /FirstChar naming a null object", models.PDFA1b, " /FirstChar 7 0 R /LastChar 66 /Widths [500 500]", Rule_19005_1_6_3_2_t04, 1},
		{"a /LastChar written as null", models.PDFA1b, " /FirstChar 65 /LastChar null /Widths [500 500]", Rule_19005_1_6_3_2_t05, 1},
		{"both present", models.PDFA1b, " /FirstChar 65 /LastChar 66 /Widths [500 500]", Rule_19005_1_6_3_2_t04, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, nullKeyFontDoc(t, tt.extra)...)
			viols, err := ValidatePDFA(ctx, doc, tt.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}

// TestFontToUnicodeWrittenAsNull pins ISO 19005-2, 6.2.11.7.2 t1 and ISO
// 14289-1, 7.21.7 t1 on a font whose glyph names give no Unicode value and
// whose /ToUnicode resolves to null: the entry supplies no mapping (ISO 32000-1,
// 7.3.7 and 7.3.10), so the glyphs have none, as with no /ToUnicode at all, and
// a real ToUnicode CMap still maps them. Every case was measured against
// veraPDF.
func TestFontToUnicodeWrittenAsNull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const enc = " /Encoding << /Type /Encoding /Differences [65 /g1 /g2] >> /FirstChar 65 /LastChar 66 /Widths [500 500]"

	for _, tt := range []struct {
		name   string
		extra  string
		failed bool
	}{
		{"a /ToUnicode written as null", enc + " /ToUnicode null", true},
		{"a /ToUnicode naming a null object", enc + " /ToUnicode 7 0 R", true},
		{"no /ToUnicode", enc, true},
		{"a ToUnicode CMap", enc + " /ToUnicode 6 0 R", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, nullKeyFontDoc(t, tt.extra)...)
			viols, err := ValidatePDFA(ctx, doc, models.PDFA2u)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_11_7_2_t01) > 0; got != tt.failed {
				t.Errorf("PDF/A-2u 6.2.11.7.2 t1 fired=%v, want %v", got, tt.failed)
			}
			uaDoc, _ := objsDocPDF(t, nullKeyFontDoc(t, tt.extra)...)
			uaViols, err := ValidatePDFUA(ctx, uaDoc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, uaViols, Rule_14289_1_7_21_7_t01) > 0; got != tt.failed {
				t.Errorf("PDF/UA-1 7.21.7 t1 fired=%v, want %v", got, tt.failed)
			}
		})
	}
}
