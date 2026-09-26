package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestCatalogAF_AReferenceIsNoAssociation pins ISO 19005-3, 6.8 t4 for a file
// specification that names an external file: only an /AF array associates it
// with the document or one of its parts (ISO 32000-2, 14.13). An annotation's
// /FS, a remote go-to action's /F and a RichMedia /Assets entry refer to the
// file without associating it, whether the filespec is indirect or written
// inline. veraPDF 1.30 fails the rule on each of those routes and passes the
// same filespec once the catalog /AF lists it, measured on a crafted file per
// route.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-3-2-t01-fail-p.pdf and
//     6-3-3-t01-fail-p.pdf: a FileAttachment's /FS names the indirect
//     external filespec 21 0 R, which no /AF lists, and veraPDF fails the
//     rule on both at PDF/A-3a, 3b and 3u while litho passed them.
func TestCatalogAF_AReferenceIsNoAssociation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		ext   = "<< /Type /Filespec /F (other.pdf) /UF (other.pdf) /AFRelationship /Unspecified >>"
		annot = "/Type /Annot /Rect [10 10 30 30] /F 4 "
	)

	tests := []struct {
		name    string
		catalog string
		annot   string
		spec    string // object 5
		want    int
	}{
		{"an indirect filespec named by an annotation /FS", "", "<< " + annot + "/Subtype /FileAttachment /FS 5 0 R >>", ext, 1},
		{"a direct filespec written in an annotation /FS", "", "<< " + annot + "/Subtype /FileAttachment /FS " + ext + " >>", "<< >>", 1},
		{"a direct filespec in a remote go-to action", "", "<< " + annot + "/Subtype /Link /A << /S /GoToR /F " + ext + " /D [0 /Fit] >> >>", "<< >>", 1},
		{"an indirect filespec in a remote go-to action", "", "<< " + annot + "/Subtype /Link /A << /S /GoToR /F 5 0 R /D [0 /Fit] >> >>", ext, 1},
		{"an indirect filespec in a RichMedia /Assets tree", "", "<< " + annot + "/Subtype /RichMedia /RichMediaContent << /Assets << /Names [(a.prc) 5 0 R] >> >> >>", ext, 1},
		{"an annotation /FS the catalog /AF also lists", " /AF [5 0 R]", "<< " + annot + "/Subtype /FileAttachment /FS 5 0 R >>", ext, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R"+tt.catalog+" >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>",
				tt.annot,
				tt.spec,
			)
			viols, err := ValidatePDFA(ctx, doc, models.PDFA3b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_3_6_8_t04); got != tt.want {
				t.Errorf("6.8 t4 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
