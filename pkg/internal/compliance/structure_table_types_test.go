package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestTableRegularity_ByStandardType pins that the table regularity rules read
// a table's rows and cells by standard type, as veraPDF's SETable does
// (GFSETable.getTR): the /RoleMap applies first, and Div, Part, and NonStruct
// wrappers are looked through.
//
// # Corpus evidence
//
//   - govdocs thread_100_100945.pdf: groups its rows in TableHead and
//     TableBody, which its role map maps to THead and TBody. litho found no
//     rows in them and passed three tables veraPDF reports under ISO 14289-2
//     §8.2.5.26 t3.
func TestTableRegularity_ByStandardType(t *testing.T) {
	t.Parallel()

	elem := func(s string, attrs ast.PdfDict, kids ...ast.PdfObject) ast.PdfDict {
		d := ast.PdfDict{"S": ast.PdfName(s)}
		if attrs != nil {
			d["A"] = attrs
		}
		if len(kids) > 0 {
			d["K"] = ast.PdfArray(kids)
		}
		return d
	}
	rowSpan2 := ast.PdfDict{"O": ast.PdfName("Table"), "RowSpan": ast.PdfInt(2)}
	// The first row is two columns wide; its RowSpan carries into the second,
	// which is then three wide.
	irregular := func(rowType string) []ast.PdfObject {
		return []ast.PdfObject{
			elem(rowType, nil, elem("TH", rowSpan2), elem("TH", nil)),
			elem(rowType, nil, elem("TH", nil), elem("TH", nil)),
		}
	}
	regular := []ast.PdfObject{
		elem("TR", nil, elem("TH", nil), elem("TH", nil)),
		elem("TR", nil, elem("TD", nil), elem("TD", nil)),
	}
	sections := map[ast.PdfName]ast.PdfName{"TableHead": "THead", "TableBody": "TBody", "Row": "TR"}

	tests := []struct {
		name    string
		roleMap map[ast.PdfName]ast.PdfName
		table   ast.PdfDict
		fires   bool
	}{
		{"rows in role-mapped sections", sections, elem("Table", nil, elem("TableHead", nil, irregular("TR")...)), true},
		{"role-mapped rows", sections, elem("Table", nil, elem("TBody", nil, irregular("Row")...)), true},
		{"rows wrapped in a Div", nil, elem("Table", nil, elem("Div", nil, irregular("TR")...)), true},
		{"a regular table in role-mapped sections", sections, elem("Table", nil, elem("TableBody", nil, regular...)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
			var results []Violation
			state := &walkState{results: &results, profile: "PDF/UA-2", doc: doc, roleMap: tt.roleMap}
			validateTableColumnConsistency(context.Background(), state, tt.table)
			if fired := hasRule(results, Rule_14289_2_8_2_5_26_t03); fired != tt.fires {
				t.Errorf("8.2.5.26 t3 fired = %v, want %v (%v)", fired, tt.fires, results)
			}
		})
	}
}
