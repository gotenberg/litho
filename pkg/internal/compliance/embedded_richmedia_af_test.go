package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// richMediaAFPDF builds a one-page document whose only route to the file
// specification in object 6 is a /RichMedia annotation's
// /RichMediaContent/Assets name tree. ef adds the /EF key that makes the
// filespec an embedded file, and af puts the spec in the page's /AF array.
func richMediaAFPDF(t *testing.T, ef, af bool) (*document.Document, ast.PdfDict) {
	t.Helper()

	efEntry := ""
	if ef {
		efEntry = " /EF << /F 7 0 R >>"
	}
	afEntry := ""
	if af {
		afEntry = " /AF [6 0 R]"
	}

	return objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Annots [4 0 R]"+afEntry+" >>",
		"<< /Type /Annot /Subtype /RichMedia /Rect [0 0 10 10] /RichMediaContent 5 0 R >>",
		"<< /Type /RichMediaContent /Assets << /Names [(test.prc) 6 0 R] >> >>",
		"<< /Type /Filespec /F (test.prc) /UF (test.prc) /AFRelationship /Unspecified"+efEntry+" >>",
		"<< /Length 4 >>\nstream\ndata\nendstream",
	)
}

// TestCatalogAF_RichMediaAssetIsAnAnnotationReference pins that a file
// specification reached only through a RichMedia annotation's
// /RichMediaContent/Assets name tree is not thereby an associated file.
//
// ISO 19005-3, 6.8 t4 asks that every file specification dictionary be
// associated with the document or one of its parts (veraPDF's
// `isAssociatedFile == true`), which ISO 32000-2, 14.13 expresses with an /AF
// array on the catalog, a page, an XObject or a structure element. An
// annotation reference is not such an array, for an embedded filespec or an
// external one: an /Assets entry is a reference of the same kind as an
// annotation /FS, and veraPDF fails both (see
// [TestCatalogAF_AReferenceIsNoAssociation]).
//
// # Corpus evidence
//
//   - All corpora, 4868 files scanned: only four carry a RichMedia annotation
//     with /Assets, PDF_A-4e__veraPDF_test_suite_6-3-2-t01-fail-b.pdf and
//     three siblings. Measured against veraPDF 1.30.2 at PDF/A-3b, all four
//     fail 6.8 t4 there, and litho reported none of them.
func TestCatalogAF_RichMediaAssetIsAnAnnotationReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		ef    bool
		af    bool
		fires bool
	}{
		{"an embedded filespec needs a real /AF array", true, false, true},
		{"an external filespec needs a real /AF array too", false, false, true},
		{"an embedded filespec the page /AF names is associated", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, catalog := richMediaAFPDF(t, tc.ef, tc.af)
			specs := collectFileSpecs(ctx, doc, catalog)
			if len(specs) == 0 {
				t.Fatal("no file specification collected")
			}
			if got := len(checkCatalogAF(ctx, doc, catalog, specs, 3)) > 0; got != tc.fires {
				t.Errorf("6.8 t4 fired = %v, want %v", got, tc.fires)
			}
		})
	}
}
