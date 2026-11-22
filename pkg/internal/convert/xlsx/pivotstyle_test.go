package xlsx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestRead_PivotTableStyle checks the preset pivot table style painted
// over a pivot table's saved cells: PivotStyleLight16's header row filled
// and bold with a line under its last row, a row whose item has children
// bold with a line under it across the table, a leaf row plain, the grand
// total row filled and bold with a line over it, and the page field above
// the location filled with a line under it.
func TestRead_PivotTableStyle(t *testing.T) {
	t.Parallel()
	def := `<pivotTableDefinition ` + nsDecl + ` name="P1" cacheId="7"><location ref="E3:F6" firstHeaderRow="1" firstDataRow="1" firstDataCol="1" rowPageCount="1" colPageCount="1"/>` +
		`<pivotFields count="2"><pivotField axis="axisRow"><items count="1"><item x="0"/></items></pivotField><pivotField axis="axisPage"><items count="1"><item x="0"/></items></pivotField></pivotFields>` +
		`<rowFields count="2"><field x="0"/><field x="1"/></rowFields>` +
		`<rowItems count="3"><i><x/></i><i r="1"><x/></i><i t="grand"><x/></i></rowItems><colItems count="1"><i/></colItems>` +
		`<pageFields count="1"><pageField fld="1"/></pageFields>` +
		`<pivotTableStyleInfo name="PivotStyleLight16" showRowHeaders="1" showColHeaders="1" showRowStripes="0" showColStripes="0" showLastColumn="1"/></pivotTableDefinition>`
	doc, _ := readBytes(t, pivotPackage(t, def))
	sh := doc.Sheets[0]
	style := func(row, col int) sheet.Style {
		c, ok := cellAt(sh, row, col)
		if !ok || c.Style == nil {
			return sheet.Style{}
		}
		return *c.Style
	}
	accent := rgb(0x4472C4)
	band, line := tint(accent, 0.79998168889431442), tint(accent, 0.39997558519241921)
	for _, col := range []int{4, 5} {
		if h := style(2, col); !near(h.Fill.Color, band) || !h.Font.Bold || h.Border[css.Bottom].Line != sheet.LineSolid || !near(h.Border[css.Bottom].Color, line) {
			t.Errorf("header cell %d: %+v", col, h)
		}
		if s := style(3, col); !s.Font.Bold || s.Fill.Color.A != 0 || s.Border[css.Bottom].Line != sheet.LineSolid {
			t.Errorf("subheading cell %d: %+v", col, s)
		}
		if l := style(4, col); l.Font.Bold || l.Fill.Color.A != 0 || l.Border[css.Bottom].Line != sheet.LineNone {
			t.Errorf("leaf cell %d: %+v", col, l)
		}
		if g := style(5, col); !g.Font.Bold || !near(g.Fill.Color, band) || g.Border[css.Top].Line != sheet.LineSolid {
			t.Errorf("grand total cell %d: %+v", col, g)
		}
		if p := style(0, col); !near(p.Fill.Color, band) || p.Border[css.Bottom].Line != sheet.LineSolid {
			t.Errorf("page field cell %d: %+v", col, p)
		}
	}
	if o := style(2, 6); o.Fill.Color.A != 0 {
		t.Errorf("a cell beside the pivot table is filled: %+v", o)
	}
}
