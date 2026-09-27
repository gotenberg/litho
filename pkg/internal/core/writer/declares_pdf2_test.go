package writer

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDeclaresPDF2 pins what counts as a PDF 2.0 output: the later of the
// header and a catalog /Version that is a version number (ISO 32000-1, 7.5.2).
func TestDeclaresPDF2(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		header  string
		version ast.PdfObject
		want    bool
	}{
		{name: "a 2.0 header", header: "2.0", want: true},
		{name: "a later header", header: "2.1", want: true},
		{name: "a 1.7 header", header: "1.7"},
		{name: "a 2.0 catalog over a 1.7 header", header: "1.7", version: ast.PdfName("2.0"), want: true},
		{name: "a 1.4 catalog under a 2.0 header", header: "2.0", version: ast.PdfName("1.4"), want: true},
		{name: "a catalog version that is not one", header: "1.7", version: ast.PdfName("two")},
		{name: "a catalog version that is not a name", header: "1.7", version: ast.PdfString("2.0")},
		{name: "a header that is not a version", header: "", version: ast.PdfName("2.0"), want: true},
		{name: "neither is a version", header: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			alloc := NewObjAllocator(0)
			catalog := ast.PdfDict{"Type": ast.PdfName("Catalog")}
			if tc.version != nil {
				catalog["Version"] = tc.version
			}
			trailer := ast.PdfDict{"Root": alloc.Alloc(catalog)}
			if got := DeclaresPDF2(alloc, trailer, tc.header); got != tc.want {
				t.Errorf("DeclaresPDF2(header %q, catalog %v) = %t, want %t", tc.header, tc.version, got, tc.want)
			}
		})
	}

	if !DeclaresPDF2(nil, nil, "2.0") {
		t.Error("DeclaresPDF2 with no catalog ignored a 2.0 header")
	}
}
