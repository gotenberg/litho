package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestActionTypesUnderOwnerTriggers pins ISO 19005-2, 6.5.1 t1 and ISO 19005-1,
// 6.6.1 t1 on which actions veraPDF judges. It reads an additional-actions
// dictionary under the trigger events its owner defines alone (ISO 32000-1,
// Tables 194 to 197): an annotation's E, X, D, U, Fo, Bl, PO, PC, PV and PI, a
// form field's K, F, V and C, a page's O and C. It never links a form field to
// an /A, judging a widget's /A on the annotation, so each JavaScript action is
// judged once or not at all. Every case was measured against veraPDF.
func TestActionTypesUnderOwnerTriggers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		w  = "/Type /Annot /Subtype /Widget /Rect [10 10 100 30] /F 4 /AP << /N 6 0 R >>"
		sq = "/Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 /AP << /N 6 0 R >>"
		js = "<< /S /JavaScript /JS (1) >>"
		p  = "<< /FT /Tx /T (p) /Kids [5 0 R] >>"
	)

	tests := []struct {
		name    string
		profile models.PDFAProfile
		fields  string // the /Fields entries
		annots  string // the page /Annots entries
		page    string // extra page entries
		o4, o5  string
		rule    Rule
		want    int
	}{
		{"a field widget's /AA /K", models.PDFA2b, "4 0 R", "4 0 R", "", "<< " + w + " /FT /Tx /T (f) /AA << /K " + js + " >> >>", "<< >>", Rule_19005_2_6_5_1_t01, 1},
		{"a field widget's /AA /E", models.PDFA2b, "4 0 R", "4 0 R", "", "<< " + w + " /FT /Tx /T (f) /AA << /E " + js + " >> >>", "<< >>", Rule_19005_2_6_5_1_t01, 1},
		{"a field widget's /A", models.PDFA2b, "4 0 R", "4 0 R", "", "<< " + w + " /FT /Tx /T (f) /A " + js + " >>", "<< >>", Rule_19005_2_6_5_1_t01, 1},
		{"a field widget's /A, PDF/A-1", models.PDFA1b, "4 0 R", "4 0 R", "", "<< " + w + " /FT /Tx /T (f) /A " + js + " >>", "<< >>", Rule_19005_1_6_6_1_t01, 1},
		{"a Square's /AA /K", models.PDFA2b, "", "4 0 R", "", "<< " + sq + " /AA << /K " + js + " >> >>", "<< >>", Rule_19005_2_6_5_1_t01, 0},
		{"a Square's /AA /E", models.PDFA2b, "", "4 0 R", "", "<< " + sq + " /AA << /E " + js + " >> >>", "<< >>", Rule_19005_2_6_5_1_t01, 1},
		{"a page's /AA /X", models.PDFA2b, "", "", " /AA << /X " + js + " >>", "<< >>", "<< >>", Rule_19005_2_6_5_1_t01, 0},
		{"a page's /AA /O", models.PDFA2b, "", "", " /AA << /O " + js + " >>", "<< >>", "<< >>", Rule_19005_2_6_5_1_t01, 1},
		{"a parent field's /A", models.PDFA2b, "4 0 R", "5 0 R", "", p[:len(p)-2] + " /A " + js + " >>", "<< " + w + " /Parent 4 0 R >>", Rule_19005_2_6_5_1_t01, 0},
		{"a parent field's /A, PDF/A-1", models.PDFA1b, "4 0 R", "5 0 R", "", p[:len(p)-2] + " /A " + js + " >>", "<< " + w + " /Parent 4 0 R >>", Rule_19005_1_6_6_1_t01, 0},
		{"a kid widget's /AA /K", models.PDFA2b, "4 0 R", "5 0 R", "", p, "<< " + w + " /Parent 4 0 R /AA << /K " + js + " >> >>", Rule_19005_2_6_5_1_t01, 0},
		{"a kid widget's /AA /K, PDF/A-1", models.PDFA1b, "4 0 R", "5 0 R", "", p, "<< " + w + " /Parent 4 0 R /AA << /K " + js + " >> >>", Rule_19005_1_6_6_1_t01, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields ["+tt.fields+"] >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots ["+tt.annots+"]"+tt.page+" >>",
				tt.o4,
				tt.o5,
				"<< /Type /XObject /Subtype /Form /BBox [0 0 90 20] /Length 0 >>\nstream\n\nendstream",
			)
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
