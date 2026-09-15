package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFormActionEntriesAsVeraPDFReadsThem pins ISO 19005-1, 6.6.1 t3 and 6.6.2
// t2 and ISO 19005-2, 6.4.1 t1 and t2 as veraPDF reads them. containsA and
// containsAA are knownKey tests, so an /A or /AA written as null is absent
// (ISO 32000-1, 7.3.7). The field rule judges only veraPDF's form fields: every
// /Fields entry, and a kid only when it carries a non-null /T, so a kid widget
// with /FT and no /T, a kid whose /T is null, and a kid that is no widget and
// has no /T are judged as annotations at most. Every case was measured against
// veraPDF.
func TestFormActionEntriesAsVeraPDFReadsThem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		w  = "/Type /Annot /Subtype /Widget /Rect [10 10 100 30] /F 4 /AP << /N 6 0 R >>"
		js = " /AA << /K << /S /JavaScript /JS (1) >> >>"
		p  = "<< /FT /Tx /T (p) /Kids [5 0 R] >>"
	)

	tests := []struct {
		name    string
		profile models.PDFAProfile
		annots  string // the page /Annots entries
		field   string // object 4, listed in /Fields
		kid     string // object 5
		rule    Rule
		want    int
	}{
		{"an /AA written as null, widget rule", models.PDFA2b, "4 0 R", "<< " + w + " /FT /Tx /T (f) /AA null >>", "<< >>", Rule_19005_2_6_4_1_t01, 0},
		{"an /AA written as null, field rule", models.PDFA2b, "4 0 R", "<< " + w + " /FT /Tx /T (f) /AA null >>", "<< >>", Rule_19005_2_6_4_1_t02, 0},
		{"an /AA written as null, PDF/A-1 field rule", models.PDFA1b, "4 0 R", "<< " + w + " /FT /Tx /T (f) /AA null >>", "<< >>", Rule_19005_1_6_6_2_t02, 0},
		{"an /A written as null", models.PDFA2b, "4 0 R", "<< " + w + " /FT /Tx /T (f) /A null >>", "<< >>", Rule_19005_2_6_4_1_t01, 0},
		{"an /A written as null, PDF/A-1", models.PDFA1b, "4 0 R", "<< " + w + " /FT /Tx /T (f) /A null >>", "<< >>", Rule_19005_1_6_6_1_t03, 0},
		{"an /AA on a merged field widget", models.PDFA2b, "4 0 R", "<< " + w + " /FT /Tx /T (f)" + js + " >>", "<< >>", Rule_19005_2_6_4_1_t02, 1},
		{"an /AA on a kid widget with /FT and no /T", models.PDFA2b, "5 0 R", p, "<< " + w + " /Parent 4 0 R /FT /Tx" + js + " >>", Rule_19005_2_6_4_1_t02, 0},
		{"an /AA on a kid widget whose /T is null", models.PDFA2b, "5 0 R", p, "<< " + w + " /Parent 4 0 R /T null" + js + " >>", Rule_19005_2_6_4_1_t02, 0},
		{"an /AA on a kid that is no widget and has no /T", models.PDFA2b, "", p, "<< /Parent 4 0 R" + js + " >>", Rule_19005_2_6_4_1_t02, 0},
		{"an /AA on a top-level widget with neither /T nor /FT", models.PDFA2b, "4 0 R", "<< " + w + js + " >>", "<< >>", Rule_19005_2_6_4_1_t02, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots ["+tt.annots+"] >>",
				tt.field,
				tt.kid,
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
