package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCaptionKidPositions pins ISO 14289-2 §8.2.5.27 t1 over the standard types
// of a structure element's kids.
//
// veraPDF tests kidsStandardTypes, which applies the /RoleMap before taking a
// standard name as itself and looks through Div, Part, and NonStruct kids.
//
// # Corpus evidence
//
//   - govdocs thread_100_100184.pdf: maps Caption to P and places those
//     elements mid-Sect; litho judged the name as written and reported them,
//     while veraPDF reports nothing.
func TestCaptionKidPositions(t *testing.T) {
	t.Parallel()

	elem := func(s string, kids ...ast.PdfObject) ast.PdfDict {
		d := ast.PdfDict{"S": ast.PdfName(s)}
		if len(kids) > 0 {
			d["K"] = ast.PdfArray(kids)
		}
		return d
	}
	tests := []struct {
		name    string
		roleMap map[ast.PdfName]ast.PdfName
		kids    []ast.PdfObject
		fires   bool
	}{
		{"a middle Caption", nil, []ast.PdfObject{elem("P"), elem("Caption"), elem("P")}, true},
		{"a first Caption", nil, []ast.PdfObject{elem("Caption"), elem("P"), elem("P")}, false},
		{"a last Caption", nil, []ast.PdfObject{elem("P"), elem("P"), elem("Caption")}, false},
		{"a Caption the role map remaps", map[ast.PdfName]ast.PdfName{"Caption": "P"}, []ast.PdfObject{elem("P"), elem("Caption"), elem("P")}, false},
		{"a custom type mapped to Caption", map[ast.PdfName]ast.PdfName{"MyCaption": "Caption"}, []ast.PdfObject{elem("P"), elem("MyCaption"), elem("P")}, true},
		{"a Caption wrapped in a Div", nil, []ast.PdfObject{elem("P"), elem("Div", elem("Caption")), elem("P")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
			var results []Violation
			state := &walkState{results: &results, profile: "PDF/UA-2", doc: doc, roleMap: tt.roleMap}
			validateCaptionKidPositions(context.Background(), state, elem("Sect", tt.kids...))
			if fired := hasRule(results, Rule_14289_2_8_2_5_27_t01); fired != tt.fires {
				t.Errorf("8.2.5.27 t1 fired = %v, want %v (%v)", fired, tt.fires, results)
			}
		})
	}
}
