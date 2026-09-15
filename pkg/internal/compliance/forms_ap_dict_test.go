package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFieldAppearanceDictionaryOnly pins ISO 19005-1, 6.9 t2 as veraPDF reads a
// widget's /AP: PDAnnotation.getCOSAP keeps /AP only when it is a dictionary, so
// an /AP written as null, a name or a stream is no appearance, on a merged field
// widget and on a kid widget alike, and a /Kids written as null makes no
// non-terminal field (ISO 32000-1, 7.3.7). Every case was measured against
// veraPDF.
func TestFieldAppearanceDictionaryOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const w = "/Type /Annot /Subtype /Widget /Rect [10 10 100 30] /F 4"

	tests := []struct {
		name   string
		annots string // the page /Annots entries
		field  string // object 4, listed in /Fields
		kid    string // object 5
		want   int
	}{
		{"an /AP written as null", "4 0 R", "<< " + w + " /FT /Tx /T (f) /AP null >>", "<< >>", 1},
		{"an /AP that is a name", "4 0 R", "<< " + w + " /FT /Tx /T (f) /AP /x >>", "<< >>", 1},
		{"an /AP that is a stream", "4 0 R", "<< " + w + " /FT /Tx /T (f) /AP 6 0 R >>", "<< >>", 1},
		{"a kid widget whose /AP is null", "5 0 R", "<< /FT /Tx /T (p) /Kids [5 0 R] >>", "<< " + w + " /Parent 4 0 R /AP null >>", 1},
		{"a /Kids written as null beside a null /AP", "4 0 R", "<< " + w + " /T (f) /Kids null /AP null >>", "<< >>", 1},
		{"an /AP dictionary", "4 0 R", "<< " + w + " /FT /Tx /T (f) /AP << /N 6 0 R >> >>", "<< >>", 0},
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
			viols, err := ValidatePDFA(ctx, doc, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_1_6_9_t02); got != tt.want {
				t.Errorf("6.9 t2 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
