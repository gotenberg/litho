package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestWidgetTUOnItsFieldOnly pins where ISO 14289-1, 7.18.1 t3 reads a widget's
// /TU: on the widget when it is merged with its field (it carries /T), otherwise
// on its immediate /Parent, and nowhere above. /TU is not an inheritable field
// entry (ISO 32000-1, 12.7.3.1), and veraPDF's GFPDWidgetAnnot.getTU reads those
// two places alone. The widget (object 7) has parent field 9, whose parent is
// field 8. Every case was measured against veraPDF.
func TestWidgetTUOnItsFieldOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name                string
		widget, parent, top string // extra entries on objects 7, 9 and 8
		want                int
	}{
		{"a kid widget whose parent field has /TU", "", " /TU (parent)", "", 0},
		{"a kid widget whose only /TU is on its grandparent", "", "", " /TU (grand)", 1},
		{"a merged widget without /TU under a parent with one", " /T (w) /FT /Tx", " /TU (parent)", "", 1},
		{"a merged widget with its own /TU", " /T (w) /FT /Tx /TU (own)", "", "", 0},
		{"a parent /TU holding only a byte order mark", "", " /TU <FEFF>", " /TU (grand)", 1},
		{"a kid widget with only its own /TU", " /TU (own)", "", "", 1},
		{"a widget with /FT but no /T and its own /TU", " /FT /Tx /TU (own)", "", "", 1},
		{"a widget with /FT but no /T under a parent with /TU", " /FT /Tx", " /TU (parent)", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [8 0 R] >>"+
					" /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> >>",
				"<< /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /S /Form /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
				"<< /Type /Annot /Subtype /Widget /Rect [10 10 100 30] /F 4 /StructParent 0 /Parent 9 0 R"+tt.widget+" >>",
				"<< /T (g) /FT /Tx /Kids [9 0 R]"+tt.top+" >>",
				"<< /T (p) /Parent 8 0 R /Kids [7 0 R]"+tt.parent+" >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_18_1_t03); got != tt.want {
				t.Errorf("7.18.1 t3 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
