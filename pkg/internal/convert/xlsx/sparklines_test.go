package xlsx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestSparklines_Read locks the sparkline groups of a worksheet's extLst
// (MS-XLSX, 2.6.90 and 2.6.88): a drawing of each sparkline's cell, the
// group's type, colours and flags, the values of its range read through
// the workbook when the page is drawn.
func TestSparklines_Read(t *testing.T) {
	t.Parallel()
	xml := `<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="B1"><v>-2</v></c><c r="C1"><v>3</v></c></row></sheetData>` +
		`<extLst><ext uri="{05C60535-1F16-4fd2-B633-F4F36F0B64E0}" xmlns:x14="` + nsX14 + `"><x14:sparklineGroups xmlns:xm="` + nsXM + `">` +
		`<x14:sparklineGroup type="column" negative="1" displayEmptyCellsAs="zero"><x14:colorSeries rgb="FF00FF00"/><x14:colorNegative rgb="FFFF0000"/>` +
		`<x14:sparklines><x14:sparkline><xm:f>S!A1:C1</xm:f><xm:sqref>D1</xm:sqref></x14:sparkline></x14:sparklines></x14:sparklineGroup>` +
		`</x14:sparklineGroups></ext></extLst>`
	doc, warnings := readBytes(t, oneSheet(t, xml))
	if len(warnings) > 0 {
		t.Errorf("warnings %v", warnings)
	}
	ds := doc.Sheets[0].Drawings
	if len(ds) != 1 || ds[0].From.Column != 3 || ds[0].From.Row != 0 || ds[0].To == nil || ds[0].To.Column != 4 || ds[0].To.Row != 1 {
		t.Fatalf("drawings %+v", ds)
	}
	var fills []typeset.Color
	for _, f := range ds[0].Picture.Vector(t.Context(), typeset.Rect{Width: 60, Height: 20}) {
		if p, ok := f.(*typeset.Path); ok && p.Fill != nil {
			fills = append(fills, p.Fill.Color)
		}
	}
	green, red := typeset.Color{G: 1, A: 1}, typeset.Color{R: 1, A: 1}
	if len(fills) != 3 || fills[0] != green || fills[1] != red || fills[2] != green {
		t.Errorf("columns %v", fills)
	}
}
