package compliance

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCellSpanFromAttributeClass pins that a table cell states /ColSpan and
// /RowSpan either in its own /A attributes or through the classes its /C names
// (ISO 32000-1, 14.7.5.2), with /A taking precedence.
//
// pdfjs bug1997343 is the witness: its row-1 header carries
// /C [/TH-col /colspan-2] and the StructTreeRoot /ClassMap defines /colspan-2
// as << /O /Table /ColSpan 2 >>. Reading only /A counted that row as one column
// against two in every other row, so ISO 14289-2, 8.2.5.26 t3 reported a
// regular table as irregular. veraPDF builds no SETable for that file and says
// nothing, which is why the row read as a litho-only clause.
func TestCellSpanFromAttributeClass(t *testing.T) {
	t.Parallel()

	classMap := ast.PdfDict{
		"colspan-2": ast.PdfDict{"O": ast.PdfName("Table"), "ColSpan": ast.PdfInt(2)},
		"rowspan-2": ast.PdfDict{"O": ast.PdfName("Table"), "RowSpan": ast.PdfInt(2)},
		"TH-col":    ast.PdfDict{"O": ast.PdfName("Table"), "Scope": ast.PdfName("Column")},
		"layout-2":  ast.PdfDict{"O": ast.PdfName("Layout"), "ColSpan": ast.PdfInt(2)},
	}

	cell := func(entries ast.PdfDict) ast.PdfDict {
		d := ast.PdfDict{"S": ast.PdfName("TH")}
		maps.Copy(d, entries)
		return d
	}

	tests := []struct {
		name     string
		cell     ast.PdfDict
		classMap ast.PdfDict
		wantCol  int
		wantRow  int
	}{
		{
			name: "no attributes at all defaults to one",
			cell: cell(nil), classMap: classMap, wantCol: 1, wantRow: 1,
		},
		{
			name:     "a class named by /C states the span",
			cell:     cell(ast.PdfDict{"C": ast.PdfName("colspan-2")}),
			classMap: classMap, wantCol: 2, wantRow: 1,
		},
		{
			// The shape bug1997343 actually uses.
			name:     "/C as an array of classes",
			cell:     cell(ast.PdfDict{"C": ast.PdfArray{ast.PdfName("TH-col"), ast.PdfName("colspan-2")}}),
			classMap: classMap, wantCol: 2, wantRow: 1,
		},
		{
			name: "/A wins over the classes /C names",
			cell: cell(ast.PdfDict{
				"A": ast.PdfDict{"O": ast.PdfName("Table"), "ColSpan": ast.PdfInt(3)},
				"C": ast.PdfName("colspan-2"),
			}),
			classMap: classMap, wantCol: 3, wantRow: 1,
		},
		{
			name: "an /A that states no span falls through to /C",
			cell: cell(ast.PdfDict{
				"A": ast.PdfDict{"O": ast.PdfName("Table"), "Scope": ast.PdfName("Column")},
				"C": ast.PdfName("colspan-2"),
			}),
			classMap: classMap, wantCol: 2, wantRow: 1,
		},
		{
			name: "/A as an array of attribute dictionaries",
			cell: cell(ast.PdfDict{"A": ast.PdfArray{
				ast.PdfDict{"O": ast.PdfName("Table"), "ColSpan": ast.PdfInt(4)},
			}}),
			classMap: classMap, wantCol: 4, wantRow: 1,
		},
		{
			// ISO 32000-1, Table 349 owns ColSpan to /Table, so a class that
			// states it under another owner does not supply it.
			name:     "a class owned by /Layout does not state a table span",
			cell:     cell(ast.PdfDict{"C": ast.PdfName("layout-2")}),
			classMap: classMap, wantCol: 1, wantRow: 1,
		},
		{
			name:     "a /C naming an unknown class",
			cell:     cell(ast.PdfDict{"C": ast.PdfName("no-such-class")}),
			classMap: classMap, wantCol: 1, wantRow: 1,
		},
		{
			name:     "a /C with no ClassMap to resolve it against",
			cell:     cell(ast.PdfDict{"C": ast.PdfName("colspan-2")}),
			classMap: nil, wantCol: 1, wantRow: 1,
		},
		{
			name:     "a class named by /C states the row span",
			cell:     cell(ast.PdfDict{"C": ast.PdfName("rowspan-2")}),
			classMap: classMap, wantCol: 1, wantRow: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
			state := &walkState{profile: "PDF/UA-2", doc: doc, classMap: tt.classMap}

			if got := cellColSpan(context.Background(), state, tt.cell); got != tt.wantCol {
				t.Errorf("cellColSpan = %d, want %d", got, tt.wantCol)
			}
			if got := cellRowSpan(context.Background(), state, tt.cell); got != tt.wantRow {
				t.Errorf("cellRowSpan = %d, want %d", got, tt.wantRow)
			}
		})
	}
}
