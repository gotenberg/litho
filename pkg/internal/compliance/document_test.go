package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestCheckNeedsRenderingKey(t *testing.T) {
	t.Parallel()
	present := ast.PdfDict{"NeedsRendering": ast.PdfBool(true)}

	if v := checkNeedsRenderingKey(present, 2); len(v) != 1 ||
		v[0].Rule.Clause != "6.4.2" || v[0].Rule.TestNumber != 2 ||
		v[0].Rule.Specification != "ISO_19005_2" {
		t.Errorf("part 2 with /NeedsRendering: got %v, want one ISO_19005_2 §6.4.2 t02 violation", v)
	}
	if v := checkNeedsRenderingKey(present, 4); len(v) != 1 || v[0].Rule.Specification != "ISO_19005_4" {
		t.Errorf("part 4 with /NeedsRendering: got %v, want a PDF/A-4 violation", v)
	}
	if v := checkNeedsRenderingKey(ast.PdfDict{}, 2); len(v) != 0 {
		t.Errorf("catalog without /NeedsRendering must not flag, got %v", v)
	}
}

// TestCheckVersionPart4_CatalogOnly pins that ISO 19005-4 §6.1.12 constrains
// only the catalog /Version key: an absent /Version conforms even when the file
// header is not 2.0 (that is §6.1.2 t1's job). Before this, litho fell back to
// the header version and fired §6.1.12 on every non-2.0 file. The UA-2 path
// keeps the header fallback.
func TestCheckVersionPart4_CatalogOnly(t *testing.T) {
	t.Parallel()
	// objsDocPDF writes a %PDF-1.7 header, so doc.Version == "1.7".
	doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")

	cases := []struct {
		name    string
		catalog ast.PdfDict
		wantFir bool
	}{
		{"absent /Version conforms despite 1.7 header", ast.PdfDict{}, false},
		{"catalog /Version 1.4 fires", ast.PdfDict{"Version": ast.PdfName("1.4")}, true},
		{"catalog /Version 2.0 conforms", ast.PdfDict{"Version": ast.PdfName("2.0")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkVersionPart4(doc, tc.catalog)
			if (len(got) > 0) != tc.wantFir {
				t.Errorf("checkVersionPart4 fired=%v, want %v (got %v)", len(got) > 0, tc.wantFir, got)
			}
		})
	}

	// UA-2 keeps the header fallback: an absent catalog /Version with a 1.7
	// header still fires.
	if got := checkVersionIs2(doc, ast.PdfDict{}, Rule_14289_2_6_2_t01, true); len(got) == 0 {
		t.Error("UA-2 version check should fall back to the header and fire on a 1.7 header")
	}
}
