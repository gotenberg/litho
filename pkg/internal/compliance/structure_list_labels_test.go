package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestListNumberingByStandardType pins ISO 14289-2 §8.2.5.25 t1 over the
// standard types of a list's items and their kids.
//
// veraPDF decides containsLabels on the L's kids and each LI's kids by
// standard type: the /RoleMap applies first, and Div, Part, and NonStruct are
// looked through.
//
// # Corpus evidence
//
//   - govdocs thread_300_300035.pdf: labels its items with LI_Label, which its
//     role map maps to Lbl; litho compared the names as written and missed
//     four lists veraPDF reports.
func TestListNumberingByStandardType(t *testing.T) {
	t.Parallel()

	elem := func(s string, kids ...ast.PdfObject) ast.PdfDict {
		d := ast.PdfDict{"S": ast.PdfName(s)}
		if len(kids) > 0 {
			d["K"] = ast.PdfArray(kids)
		}
		return d
	}
	numbered := elem("L", elem("LI", elem("Lbl"), elem("LBody")))
	numbered["A"] = ast.PdfDict{"O": ast.PdfName("List"), "ListNumbering": ast.PdfName("Decimal")}
	byClass := elem("L", elem("LI", elem("Lbl"), elem("LBody")))
	byClass["C"] = ast.PdfName("enumerate")
	inherited := elem("L", elem("LI", elem("Lbl"), elem("LBody")))
	inherited["P"] = ast.PdfDict{"S": ast.PdfName("Sect"), "A": ast.PdfDict{"O": ast.PdfName("List"), "ListNumbering": ast.PdfName("Decimal")}}
	otherOwner := elem("L", elem("LI", elem("Lbl"), elem("LBody")))
	otherOwner["A"] = ast.PdfDict{"O": ast.PdfName("Layout"), "ListNumbering": ast.PdfName("Decimal")}
	classMap := ast.PdfDict{"enumerate": ast.PdfDict{"O": ast.PdfName("List"), "ListNumbering": ast.PdfName("Ordered")}}

	tests := []struct {
		name    string
		roleMap map[ast.PdfName]ast.PdfName
		list    ast.PdfDict
		fires   bool
	}{
		{"a Lbl with no ListNumbering", nil, elem("L", elem("LI", elem("Lbl"), elem("LBody"))), true},
		{"a custom label mapped to Lbl", map[ast.PdfName]ast.PdfName{"LI_Label": "Lbl"}, elem("L", elem("LI", elem("LI_Label"), elem("LBody"))), true},
		{"an item wrapped in a Div", nil, elem("L", elem("Div", elem("LI", elem("Lbl")))), true},
		{"a Lbl the role map remaps", map[ast.PdfName]ast.PdfName{"Lbl": "Span"}, elem("L", elem("LI", elem("Lbl"), elem("LBody"))), false},
		{"a list with ListNumbering", nil, numbered, false},
		{"a ListNumbering from the list's attribute class", nil, byClass, false},
		{"a ListNumbering inherited from the parent", nil, inherited, false},
		{"a ListNumbering another owner states", nil, otherOwner, true},
		{"items with no label", nil, elem("L", elem("LI", elem("LBody"))), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
			var results []Violation
			state := &walkState{results: &results, profile: "PDF/UA-2", doc: doc, roleMap: tt.roleMap, classMap: classMap}
			validateListNumbering(context.Background(), state, tt.list)
			if fired := hasRule(results, Rule_14289_2_8_2_5_25_t01); fired != tt.fires {
				t.Errorf("8.2.5.25 t1 fired = %v, want %v (%v)", fired, tt.fires, results)
			}
		})
	}
}
