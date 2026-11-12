package odp

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
)

// TestRead_TableGridCap locks the cell cap on the grid a table lays out,
// every row padded to the widest: rows of no cells, and short rows under
// more columns, count the cells they are padded to.
func TestRead_TableGridCap(t *testing.T) {
	t.Parallel()
	for name, table := range map[string]string{
		"empty rows": `<table:table-column/>` +
			`<table:table-row table:number-rows-repeated="100000"/><table:table-row table:number-rows-repeated="100000"/>`,
		"short rows": `<table:table-column table:number-columns-repeated="2"/>` +
			`<table:table-row table:number-rows-repeated="100000"><table:table-cell/></table:table-row>`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, warnings := read(t, flat{body: page(`<draw:frame svg:width="8cm" svg:height="3cm"><table:table>` + table + `</table:table></draw:frame>`)}.bytes())
			if len(doc.Slides) != 1 || len(doc.Slides[0].Shapes) != 1 {
				t.Fatalf("slides = %+v", doc.Slides)
			}
			s := doc.Slides[0].Shapes[0]
			tb := s.Table
			if s.Kind != slides.KindTable || tb == nil {
				t.Fatal("no table read")
			}
			cells := 0
			for _, row := range tb.Rows {
				cells += max(len(row.Cells), len(tb.ColumnWidths))
			}
			if cells > maxTableCells {
				t.Errorf("the grid holds %d cells, want at most %d", cells, maxTableCells)
			}
			if !hasWarning(warnings, "table-cells") {
				t.Errorf("warnings = %+v, want table-cells", warnings)
			}
			if tb.Rows[0].Cells[0].Covered || tb.Rows[0].Cells[0].ColSpan != 1 {
				t.Errorf("first cell = %+v", tb.Rows[0].Cells[0])
			}
		})
	}
}
