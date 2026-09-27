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
// 7.2), and each tagged source whose catalog names a language other than the
// one its content would inherit states its own on its Document element. A
// source naming none states nothing: its content inherits the output's
// language (ISO 32000-1, 14.9.2.1). A source's Document joins the first
// source's, so it inherits what that Document states before the catalog's.
func TestMerge_NamesTheFirstDeclaredLanguage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	undeclared := taggedThreePagePDF("", "/Lang () ", "")
	german := taggedThreePagePDF("de-DE")
	english := taggedThreePagePDF("en-US")
	for _, tc := range []struct {
		name    string
		sources [][]byte
		catalog ast.PdfObject
		// documents lists, for each source's Document element in the order
		// the output holds them, the /Lang it states, nil for none.
		documents []ast.PdfObject
	}{
		{"undeclared first", [][]byte{undeclared, german}, ast.PdfString("de-DE"), []ast.PdfObject{nil, nil}},
		{"undeclared second", [][]byte{german, undeclared}, ast.PdfString("de-DE"), []ast.PdfObject{nil, nil}},
		{"another declared second", [][]byte{german, english}, ast.PdfString("de-DE"), []ast.PdfObject{nil, ast.PdfString("en-US")}},
		{"another declared after an undeclared first", [][]byte{undeclared, german, english}, ast.PdfString("de-DE"), []ast.PdfObject{nil, nil, ast.PdfString("en-US")}},
		{"none declared", [][]byte{undeclared, undeclared}, nil, []ast.PdfObject{nil, nil}},
		// The empty identifier a second source holds still says the language
		// is unknown, where no entry says nothing.
		{"unknown declared second", [][]byte{undeclared, taggedThreePagePDF("")}, ast.PdfString(""), []ast.PdfObject{nil, nil}},
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
			assertDocumentLangs(t, tree, tc.documents)
		})
	}
}

// TestInsertPages_LetsAnUndeclaredSourceInheritTheLanguage pins that pages
// inserted from a tagged source naming no language join the target's tree
// without a /Lang of their own: their content inherits the target's language
// (ISO 32000-1, 14.9.2.1). The empty identifier written there said the
// language is unknown, which ISO 14289-1, 7.2 test 29 and ISO 14289-2, 8.4.4
// test 2 reject. A source naming another language still states it.
func TestInsertPages_LetsAnUndeclaredSourceInheritTheLanguage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		source    []byte
		documents []ast.PdfObject
	}{
		{"undeclared", taggedThreePagePDF("", "/Lang () ", ""), []ast.PdfObject{nil, nil}},
		{"another declared", taggedThreePagePDF("en-US"), []ast.PdfObject{nil, ast.PdfString("en-US")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := openBytes(t, tc.source)
			defer source.Close()
			out := writeLabelled(t, taggedThreePagePDF("de-DE"), func(w *Writer) error {
				return w.InsertPages(3, source)
			})
			assertDocumentLangs(t, readOutputTree(t, out), tc.documents)
		})
	}
}

// assertDocumentLangs checks that the Document elements of tree state, in the
// order the tree holds them, the /Lang entries want lists, nil for none.
func assertDocumentLangs(t *testing.T, tree *outputTree, want []ast.PdfObject) {
	t.Helper()
	var documents []ast.PdfObject
	for _, elem := range tree.elements() {
		if elem["S"] == ast.PdfName("Document") {
			documents = append(documents, elem["Lang"])
		}
	}
	if len(documents) != len(want) {
		t.Fatalf("the output holds %d Document elements, want %d", len(documents), len(want))
	}
	for i, lang := range want {
		if documents[i] != lang {
			t.Errorf("Document %d states /Lang %v, want %v", i+1, documents[i], lang)
		}
	}
}
