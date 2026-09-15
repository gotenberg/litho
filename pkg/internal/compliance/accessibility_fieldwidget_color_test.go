package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFieldWidgetByTOrParent pins which widgets ISO 14289-2, 8.10.1 t1 and
// 8.10.2.3 t1 judge, as veraPDF's isFieldWidget decides: a widget carrying the
// partial field name /T or a field /Parent, where /FT alone makes no field widget
// and a /Parent written as null is absent (ISO 32000-1, 7.3.7). Every case was
// measured against veraPDF.
func TestFieldWidgetByTOrParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name   string
		widget string
		encl   string
		rule   Rule
		want   int
	}{
		{"a widget with /FT only, enclosed by P", " /FT /Tx", "P", Rule_14289_2_8_10_1_t01, 0},
		{"a widget with /T only, enclosed by P", " /T (f)", "P", Rule_14289_2_8_10_1_t01, 1},
		{"a widget with a /Parent written as null, enclosed by P", " /Parent null", "P", Rule_14289_2_8_10_1_t01, 0},
		{"a widget with /FT and /T, enclosed by P", " /FT /Tx /T (f)", "P", Rule_14289_2_8_10_1_t01, 1},
		{"a widget with /FT only, in a Form without a label", " /FT /Tx", "Form", Rule_14289_2_8_10_2_3_t01, 0},
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
				"<< /S /"+tt.encl+" /NS 8 0 R /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
				"<< /Type /Annot /Subtype /Widget /Rect [10 10 100 30] /F 4 /StructParent 0"+tt.widget+" >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}

// TestAnnotColorNullKeys pins ISO 19005-1, 6.5.3 t3 on null colour entries as
// veraPDF reads them: containsC and containsIC ask for a non-null /C or /IC, so
// one written as null is absent (ISO 32000-1, 7.3.7). The document has no
// OutputIntent, so a real /C array is forbidden. Every case was measured against
// veraPDF.
func TestAnnotColorNullKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name  string
		extra string
		want  int
	}{
		{"a /C written as null", " /C null", 0},
		{"an /IC written as null", " /IC null", 0},
		{"a /C colour array", " /C [1 0 0]", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>",
				"<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4"+tt.extra+" >>",
			)
			viols, err := ValidatePDFA(ctx, doc, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_1_6_5_3_t03); got != tt.want {
				t.Errorf("6.5.3 t3 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
