package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestWidgetLabelWithTypelessKid pins ISO 14289-2, 8.10.2.3 t1 on a Lbl whose
// only kid is a structure element with no /S: veraPDF's getChildren counts it,
// so the Lbl has content and labels its widget, as a Lbl holding a Span does.
// Every case was measured against veraPDF.
func TestWidgetLabelWithTypelessKid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, kid := range []string{
		"<< /Type /StructElem /NS 10 0 R /P 8 0 R >>",
		"<< /Type /StructElem /S /Span /NS 10 0 R /P 8 0 R >>",
	} {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [7 0 R] >> /MarkInfo << /Marked true >> /Lang (en) >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] /Tabs /S >>",
			"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> /Namespaces [10 0 R] >>",
			"<< /Type /StructElem /S /Document /NS 10 0 R /P 4 0 R /K [6 0 R] >>",
			"<< /Type /StructElem /S /Form /NS 10 0 R /P 5 0 R /Pg 3 0 R /K [8 0 R << /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
			"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /TU (field) /Rect [10 10 100 30] /F 4 /StructParent 0 >>",
			"<< /Type /StructElem /S /Lbl /NS 10 0 R /P 6 0 R /K [9 0 R] >>",
			kid,
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		)
		viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		if got := countRuleErrs(t, viols, Rule_14289_2_8_10_2_3_t01); got != 0 {
			t.Errorf("Lbl kid %s: 8.10.2.3 t1 fired %d times, want 0", kid, got)
		}
	}
}
