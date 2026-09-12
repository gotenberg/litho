package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestLIContentByStandardType pins ISO 14289-2 §8.2.5.25 t2 over an element's
// standard type and the content items veraPDF attributes to it.
//
// veraPDF builds its SELI object for an element whose standard type is LI, and
// its hasContentItems counts a content item in a Div, Part, or NonStruct kid
// as the LI's own. litho compared the LI name as written and read only the
// LI's direct /K entries.
func TestLIContentByStandardType(t *testing.T) {
	t.Parallel()

	elem := func(s string, kids ...ast.PdfObject) ast.PdfDict {
		d := ast.PdfDict{"S": ast.PdfName(s), "P": ast.PdfName("parent")}
		if len(kids) > 0 {
			d["K"] = ast.PdfArray(kids)
		}
		return d
	}
	tests := []struct {
		name    string
		roleMap map[ast.PdfName]ast.PdfName
		item    ast.PdfDict
		fires   bool
	}{
		{"an MCID directly in an LI", nil, elem("LI", ast.PdfInt(0)), true},
		{"an MCID in a custom item mapped to LI", map[ast.PdfName]ast.PdfName{"ListItem": "LI"}, elem("ListItem", ast.PdfInt(0)), true},
		{"an MCID in a Div inside an LI", nil, elem("LI", elem("Div", ast.PdfInt(0))), true},
		{"an MCID in an LBody", nil, elem("LI", elem("LBody", ast.PdfInt(0))), false},
		{"an LI the role map remaps", map[ast.PdfName]ast.PdfName{"LI": "Span"}, elem("LI", ast.PdfInt(0)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
			var results []Violation
			state := &walkState{results: &results, profile: "PDF/UA-2", doc: doc, roleMap: tt.roleMap, seen: make(map[int]bool)}
			walkElement(context.Background(), state, tt.item, "L", 0, 0)
			if fired := hasRule(results, Rule_14289_2_8_2_5_25_t02); fired != tt.fires {
				t.Errorf("8.2.5.25 t2 fired = %v, want %v (%v)", fired, tt.fires, results)
			}
		})
	}
}
