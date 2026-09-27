package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestMerge_NamesTheFirstDeclaredLanguage pins the natural language a merge
// names: the catalog's is the first source's that names one, so a first source
// naming none no longer leaves the output without a language (ISO 14289-1,
// 7.2), and each tagged source whose content would otherwise inherit another
// language states its own on its Document element: the language it names, or
// the empty identifier, which says the language is unknown (ISO 32000-1,
// 14.9.2), for a source naming none. A source's Document joins the first
// source's, so it inherits what that Document states before the catalog's.
func TestMerge_NamesTheFirstDeclaredLanguage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	undeclared := func(edits ...string) []byte {
		return taggedThreePagePDF("", append([]string{"/Lang () ", ""}, edits...)...)
	}
	german := taggedThreePagePDF("de-DE")
	// Every element under the Document names a language of its own, so no
	// content of the source inherits the Document's.
	covered := undeclared(
		"/S /Sect", "/S /Sect /Lang (en)",
		"/S /Link", "/S /Link /Lang (en)",
		"/S /Para /P 10 0 R /Pg 5 0 R", "/S /Para /P 10 0 R /Pg 5 0 R /Lang (en)",
	)
	for _, tc := range []struct {
		name    string
		sources [][]byte
		catalog ast.PdfObject
		// documents lists, for each source's Document element in the order
		// the output holds them, the /Lang it states, nil for none.
		documents []ast.PdfObject
	}{
		{"undeclared first", [][]byte{undeclared(), german}, ast.PdfString("de-DE"), []ast.PdfObject{ast.PdfString(""), ast.PdfString("de-DE")}},
		{"undeclared second", [][]byte{german, undeclared()}, ast.PdfString("de-DE"), []ast.PdfObject{nil, ast.PdfString("")}},
		{"none declared", [][]byte{undeclared(), undeclared()}, nil, []ast.PdfObject{nil, nil}},
		// The empty identifier a second source holds still says the language
		// is unknown, where no entry says nothing.
		{"unknown declared second", [][]byte{undeclared(), taggedThreePagePDF("")}, ast.PdfString(""), []ast.PdfObject{nil, nil}},
		{"undeclared but covered", [][]byte{covered, german}, ast.PdfString("de-DE"), []ast.PdfObject{nil, nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			readers := make([]*Reader, len(tc.sources))
			for i, data := range tc.sources {
				readers[i] = openDeclaration(t, data, false)
				defer readers[i].Close()
			}
			var out bytes.Buffer
			if err := Merge(ctx, readers, &out); err != nil {
				t.Fatalf("Merge: %v", err)
			}
			tree := readOutputTree(t, out.Bytes())
			catalog, err := document.ResolveCatalog(ctx, tree.doc)
			if err != nil {
				t.Fatalf("ResolveCatalog: %v", err)
			}
			if got := catalog.Dict["Lang"]; got != tc.catalog {
				t.Errorf("catalog /Lang = %v, want %v", got, tc.catalog)
			}
			var documents []ast.PdfObject
			for _, elem := range tree.elements() {
				if elem["S"] == ast.PdfName("Document") {
					documents = append(documents, elem["Lang"])
				}
			}
			if len(documents) != len(tc.documents) {
				t.Fatalf("the output holds %d Document elements, want %d", len(documents), len(tc.documents))
			}
			for i, want := range tc.documents {
				if documents[i] != want {
					t.Errorf("source %d's Document states /Lang %v, want %v", i+1, documents[i], want)
				}
			}
		})
	}
}
