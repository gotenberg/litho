package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestTextFieldRichValueNullRV pins ISO 14289-2, 8.10.3.3 t1 on null entries as
// veraPDF reads them: containsRV is a knownKey on /RV, so an /RV written as null
// is absent (ISO 32000-1, 7.3.7) and the rule does not apply, while a /V written
// as null beside a real /RV still fails, since the test requires V != null.
// Every case was measured against veraPDF.
func TestTextFieldRichValueNullRV(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const rv = `(<body xmlns="http://www.w3.org/1999/xhtml"><p>x</p></body>)`

	tests := []struct {
		name  string
		extra string // entries on the merged text field widget
		want  int
	}{
		{"an /RV written as null beside a /V", " /RV null /V (x)", 0},
		{"a /V written as null beside an /RV", " /RV " + rv + " /V null", 1},
		{"an /RV whose text equals /V", " /RV " + rv + " /V (x)", 0},
		{"an /RV whose text differs from /V", " /RV " + rv + " /V (y)", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [7 0 R] >>"+
					" /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> /Namespaces [8 0 R] >>",
				"<< /S /Document /NS 8 0 R /P 4 0 R /K [6 0 R] >>",
				"<< /S /Form /NS 8 0 R /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /TU (field) /Contents (field)"+
					" /Rect [10 10 100 30] /F 4 /StructParent 0"+tt.extra+" >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_10_3_3_t01); got != tt.want {
				t.Errorf("8.10.3.3 t1 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
