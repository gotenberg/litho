package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestMathMLFormulaAncestor pins ISO 14289-2, 8.2.5.29 t1 as the PDF/UA-2
// validation profile tests it: the subject is every element that stands for a
// MathML type through the role map (ISO 32000-2:2020, 14.8.6), and it passes
// when any ancestor stands for Formula or for a MathML type. Every case was
// measured against veraPDF.
func TestMathMLFormulaAncestor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		pdf2  = "11 0 R"
		mml   = "12 0 R"
		badNS = "14 0 R"
	)
	tests := []struct {
		name    string
		first   string // object 7, the Document's kid
		second  string // object 8
		third   string // object 13
		leaf    string // the element the ParentTree names for MCID 0
		roleMap string
		want    int
	}{
		{
			"math in the MathML namespace under P, with an mi kid",
			"<< /S /P /NS " + pdf2 + " /K [8 0 R] /P 6 0 R >>",
			"<< /S /math /NS " + mml + " /K [13 0 R] /P 7 0 R >>",
			"<< /S /mi /NS " + mml + " /Pg 3 0 R /K [0] /P 8 0 R >>", "13 0 R", "<< >>", 1,
		},
		{
			"mrow in the MathML namespace under P",
			"<< /S /P /NS " + pdf2 + " /K [8 0 R] /P 6 0 R >>",
			"<< /S /mrow /NS " + mml + " /Pg 3 0 R /K [0] /P 7 0 R >>",
			"<< >>", "8 0 R", "<< >>", 1,
		},
		{
			"a custom Math mapped to MathML math, under P",
			"<< /S /P /NS " + pdf2 + " /K [8 0 R] /P 6 0 R >>",
			"<< /S /Math /NS " + badNS + " /Pg 3 0 R /K [0] /P 7 0 R >>",
			"<< >>", "8 0 R", "<< >>", 1,
		},
		{
			"math without a namespace under P",
			"<< /S /P /NS " + pdf2 + " /K [8 0 R] /P 6 0 R >>",
			"<< /S /math /Pg 3 0 R /K [0] /P 7 0 R >>",
			"<< >>", "8 0 R", "<< >>", 0,
		},
		{
			"Math without a namespace under P",
			"<< /S /P /NS " + pdf2 + " /K [8 0 R] /P 6 0 R >>",
			"<< /S /Math /Pg 3 0 R /K [0] /P 7 0 R >>",
			"<< >>", "8 0 R", "<< >>", 0,
		},
		{
			"math under Formula",
			"<< /S /Formula /NS " + pdf2 + " /K [8 0 R] /P 6 0 R >>",
			"<< /S /math /NS " + mml + " /Pg 3 0 R /K [0] /P 7 0 R >>",
			"<< >>", "8 0 R", "<< >>", 0,
		},
		{
			"math under a name mapped to Formula",
			"<< /S /MyFormula /K [8 0 R] /P 6 0 R >>",
			"<< /S /math /NS " + mml + " /Pg 3 0 R /K [0] /P 7 0 R >>",
			"<< >>", "8 0 R", "<< /MyFormula /Formula >>", 0,
		},
		{
			"math under a Span inside Formula",
			"<< /S /Formula /NS " + pdf2 + " /K [8 0 R] /P 6 0 R >>",
			"<< /S /Span /NS " + pdf2 + " /K [13 0 R] /P 7 0 R >>",
			"<< /S /math /NS " + mml + " /Pg 3 0 R /K [0] /P 8 0 R >>", "13 0 R", "<< >>", 0,
		},
		{
			"math under a Div inside Formula",
			"<< /S /Formula /NS " + pdf2 + " /K [8 0 R] /P 6 0 R >>",
			"<< /S /Div /NS " + pdf2 + " /K [13 0 R] /P 7 0 R >>",
			"<< /S /math /NS " + mml + " /Pg 3 0 R /K [0] /P 8 0 R >>", "13 0 R", "<< >>", 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /StructParents 0"+
					" /Resources << /Font << /F1 9 0 R >> >> >>",
				streamObj("/P <</MCID 0>> BDC BT /F1 12 Tf 10 50 Td (Hi) Tj ET EMC"),
				"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 10 0 R /Namespaces [11 0 R 12 0 R 14 0 R]"+
					" /RoleMap "+tt.roleMap+" >>",
				"<< /S /Document /NS "+pdf2+" /K [7 0 R] /P 5 0 R >>",
				tt.first,
				tt.second,
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
				"<< /Nums [0 ["+tt.leaf+"]] >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /Namespace /NS (http://www.w3.org/1998/Math/MathML) >>",
				tt.third,
				"<< /Type /Namespace /NS (http://example.com/badns) /RoleMapNS << /Math [/math 12 0 R] >> >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			got := 0
			for _, e := range viols {
				if v, ok := e.(Violation); ok && v.Rule.Clause == "8.2.5.29" && v.Rule.TestNumber == 1 {
					got++
				}
			}
			if got != tt.want {
				t.Errorf("8.2.5.29 t1 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
