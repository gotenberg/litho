package mutations

import (
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestPageRanks pins how a selection naming pages again is grouped: every
// page's first naming in the first group, each later naming in the group of
// its rank, each group in output order.
func TestPageRanks(t *testing.T) {
	t.Parallel()
	a, b, c := &document.Page{}, &document.Page{}, &document.Page{}
	for _, tc := range []struct {
		pages []*document.Page
		want  [][]int
	}{
		{[]*document.Page{a, b, c}, [][]int{{0, 1, 2}}},
		{[]*document.Page{a, a}, [][]int{{0}, {1}}},
		{[]*document.Page{a, b, a, c, b, a}, [][]int{{0, 1, 3}, {2, 4}, {5}}},
	} {
		if got := pageRanks(tc.pages); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("pageRanks = %v, want %v", got, tc.want)
		}
	}
}

// TestOwnedByOnePlace pins which objects a repeated page cannot share with its
// first copy: those naming one page, one parent or one /ParentTree entry.
func TestOwnedByOnePlace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		dict ast.PdfDict
		want bool
	}{
		{"a font", ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1")}, false},
		{"a form", ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"), "BBox": ast.PdfArray{}}, false},
		{"a keyed form", ast.PdfDict{"Subtype": ast.PdfName("Form"), "StructParents": ast.PdfInt(3)}, true},
		{"a keyed image", ast.PdfDict{"Subtype": ast.PdfName("Image"), "StructParent": ast.PdfInt(3)}, true},
		{"an annotation without /Type", ast.PdfDict{"Subtype": ast.PdfName("Link"), "Rect": ast.PdfArray{}}, true},
		{"a field", ast.PdfDict{"FT": ast.PdfName("Tx")}, true},
		{"a structure element", ast.PdfDict{"S": ast.PdfName("P"), "P": ast.PdfIndirectRef{ObjectNum: 4}}, true},
		{"a page", ast.PdfDict{"Type": ast.PdfName("Page")}, true},
	} {
		if got := ownedByOnePlace(tc.dict); got != tc.want {
			t.Errorf("%s: ownedByOnePlace = %v, want %v", tc.name, got, tc.want)
		}
	}
}
