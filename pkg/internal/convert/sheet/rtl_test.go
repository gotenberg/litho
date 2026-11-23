package sheet

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_RightToLeftSheet locks Calc's print of a right-to-left
// sheet (see [grid.mirrored], which holds the measurements): column A at
// the area's right edge, the next columns to its left, each cell's text
// aligned in its own cell as in a left-to-right sheet. Under Excel's
// rules the sheet stays left to right.
func TestConvert_RightToLeftSheet(t *testing.T) {
	right := &Style{Align: Alignment{Horizontal: HRight}}
	sheetOf := func() *Sheet {
		return &Sheet{Name: "S1", DefaultColumnWidth: 80, RightToLeft: true, Rows: []Row{
			{Index: 0, Cells: []Cell{textCell(0, "AAA", nil), textCell(1, "BBB", nil), textCell(2, "CCC", right)}},
		}}
	}
	pg := convert(t, newDoc(sheetOf()), office.Options{}).Pages[0]
	areaRight := 595.28 - 36
	a, b, c := find(t, pg, "AAA"), find(t, pg, "BBB"), find(t, pg, "CCC")
	// Left-aligned text starts a point of padding inside its cell; right
	// aligned text ends one inside.
	if !near(a.x, areaRight-80+1, 0.6) || !near(b.x, areaRight-160+1, 0.6) || !near(c.endX, areaRight-160-1, 0.6) {
		t.Errorf("AAA at %.2f, BBB at %.2f, CCC ends %.2f; want %.2f, %.2f, %.2f\n%s", a.x, b.x, c.endX, areaRight-79, areaRight-159, areaRight-161, dump(pg))
	}
	doc := newDoc(sheetOf())
	doc.ExcelRules = true
	pg = convert(t, doc, office.Options{}).Pages[0]
	if a, b := find(t, pg, "AAA"), find(t, pg, "BBB"); a.x > b.x {
		t.Errorf("under Excel's rules AAA at %.2f right of BBB at %.2f", a.x, b.x)
	}
}

// TestConvert_RightToLeftCellDirection locks that a cell of a
// right-to-left sheet sets its text right to left (see [grid.mirrored]):
// the period ending a Hebrew text is drawn at its left end.
func TestConvert_RightToLeftCellDirection(t *testing.T) {
	for _, rtl := range []bool{false, true} {
		sh := &Sheet{Name: "S1", DefaultColumnWidth: 80, RightToLeft: rtl, Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "אב גד.", nil)}}}}
		var drawn strings.Builder
		for _, r := range runs(convert(t, newDoc(sh), office.Options{}).Pages[0]) {
			drawn.WriteString(r.text)
		}
		// The runs' text is in drawing order, left to right.
		if got := strings.HasPrefix(drawn.String(), "."); got != rtl {
			t.Errorf("right to left %v: drawn %q", rtl, drawn.String())
		}
	}
}
