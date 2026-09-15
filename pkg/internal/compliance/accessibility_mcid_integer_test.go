package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestMarkedContentMCIDIsAnInteger pins ISO 14289-1, 7.1 t3 and ISO 14289-2,
// 8.2.2 t1 on the /MCID of a BDC property list as veraPDF reads it:
// GFOpMarkedContent.getMCID takes the entry only as an integer, so content under
// an /MCID written as null, a name or a real is untagged real content, as is
// content under no /MCID at all. Every case was measured against veraPDF.
func TestMarkedContentMCIDIsAnInteger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name  string
		props string
		want  int
	}{
		{"an integer /MCID", "<< /MCID 0 >>", 0},
		{"an /MCID written as null", "<< /MCID null >>", 1},
		{"an /MCID that is a name", "<< /MCID /x >>", 1},
		{"an /MCID that is a real", "<< /MCID 0.0 >>", 1},
		{"no /MCID", "<< >>", 1},
	}
	for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
		rule := Rule_14289_1_7_1_t03
		if profile == models.PDFUA2 {
			rule = Rule_14289_2_8_2_2_t01
		}
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s: %s", profile, tt.name), func(t *testing.T) {
				t.Parallel()
				body := "/P " + tt.props + " BDC 0 0 10 10 re f EMC"
				doc, _ := objsDocPDF(t,
					"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 7 0 R /StructParents 0 >>",
					"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> /Namespaces [8 0 R] >>",
					"<< /Type /StructElem /S /Document /NS 8 0 R /P 4 0 R /K [6 0 R] >>",
					"<< /Type /StructElem /S /P /NS 8 0 R /P 5 0 R /Pg 3 0 R /K [0] >>",
					fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
					"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				)
				viols, err := ValidatePDFUA(ctx, doc, profile)
				if err != nil {
					t.Fatalf("ValidatePDFUA: %v", err)
				}
				if got := countRuleErrs(t, viols, rule); got != tt.want {
					t.Errorf("%s t%d fired %d times, want %d", rule.Clause, rule.TestNumber, got, tt.want)
				}
			})
		}
	}
}

// TestMarkedContentLangThroughMCID pins ISO 14289-1, 7.2 t34 on text whose only
// language source is the structure element its /MCID names: GFOpMarkedContent
// reads /MCID only as an integer, so under an /MCID written as null or a real no
// element supplies the /Lang and the text's language is undetermined. Every case
// was measured against veraPDF.
func TestMarkedContentLangThroughMCID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name  string
		props string
		want  int
	}{
		{"an integer /MCID", "<< /MCID 0 >>", 0},
		{"an /MCID written as null", "<< /MCID null >>", 1},
		{"an /MCID that is a real", "<< /MCID 0.0 >>", 1},
		{"no /MCID", "<< >>", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := "/P " + tt.props + " BDC BT /F1 12 Tf 10 10 Td (Hi) Tj ET EMC"
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 7 0 R /StructParents 0 /Resources << /Font << /F1 8 0 R >> >> >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> >>",
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K [0] /Lang (en) >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_2_t34); got != tt.want {
				t.Errorf("7.2 t34 fired %d times, want %d", got, tt.want)
			}
		})
	}
}

// TestPUAActualTextThroughMCID pins ISO 14289-2, 8.4.3 t1 on a character whose
// ToUnicode maps to the Private Use Area and whose only /ActualText is on the
// structure element an /MCID names. Only an integer /MCID names one, so under an
// /MCID written as null or a real, or under none at all, the character has no
// /ActualText. Every case was measured against veraPDF.
func TestPUAActualTextThroughMCID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /X def " +
		"1 begincodespacerange <00> <FF> endcodespacerange 1 beginbfchar <41> <E000> endbfchar " +
		"endcmap CMapName currentdict /CMap defineresource pop end end"

	tests := []struct {
		name  string
		props string
		want  int
	}{
		{"an integer /MCID", "<< /MCID 0 >>", 0},
		{"an /MCID written as null", "<< /MCID null >>", 1},
		{"an /MCID that is a real", "<< /MCID 0.0 >>", 1},
		{"no /MCID", "<< >>", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := "/P " + tt.props + " BDC BT /F1 12 Tf 10 10 Td (A) Tj ET EMC"
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 7 0 R /StructParents 0 /Resources << /Font << /F1 8 0 R >> >> >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> /Namespaces [10 0 R] >>",
				"<< /Type /StructElem /S /Document /NS 10 0 R /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /P /NS 10 0 R /P 5 0 R /Pg 3 0 R /K [0] /ActualText (x) >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode 9 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_4_3_t01); got != tt.want {
				t.Errorf("8.4.3 t1 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
