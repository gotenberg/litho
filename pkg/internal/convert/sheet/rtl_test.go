package sheet

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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

// TestPlan_RightToLeftSheetOfDrawings locks the pages Calc prints of a
// right-to-left sheet that holds drawings and no value: those of its
// first band of columns alone (see [grid.holdsValue]). A value on the
// sheet, or a left-to-right sheet, prints the band further on too.
func TestPlan_RightToLeftSheetOfDrawings(t *testing.T) {
	for _, tc := range []struct {
		rtl, value bool
		want       int
	}{{true, false, 1}, {true, true, 2}, {false, false, 2}} {
		sh := &Sheet{Name: "S", Page: a4(), DefaultColumnWidth: 85, RightToLeft: tc.rtl}
		for _, c := range []int{1, 6} {
			sh.Drawings = append(sh.Drawings, Drawing{From: Anchor{Row: 2, Column: c}, Width: 40, Height: 20, Shape: true})
		}
		if tc.value {
			sh.Rows = []Row{{Index: 0, Cells: []Cell{numCell(0, 1, nil)}}}
		}
		if n := len(convert(t, newDoc(sh), office.Options{}).Pages); n != tc.want {
			t.Errorf("right to left %t, a value %t: %d pages, want %d", tc.rtl, tc.value, n, tc.want)
		}
	}
}

// TestConvert_RightToLeftPaintedDrawing locks where Calc prints a drawing
// whose painted box differs from its anchors' box on a right-to-left
// sheet: the painted box is mirrored, so a shape turned to paint right of
// its corner paints left of the mirrored corner.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf137082_RTL_cell_anchored.ods prints its
//     skewed and turned shape, anchored in C3 and painting 22.8 mm right
//     of its corner, from 72.2 to 95 mm left of the sheet's right edge,
//     where mirroring its anchors' box put it 7.4 mm further right.
func TestConvert_RightToLeftPaintedDrawing(t *testing.T) {
	var got []typeset.Rect
	pic := &layout.Picture{Width: 40, Height: 20, Vector: func(_ context.Context, r typeset.Rect) []typeset.Fragment {
		got = append(got, r)
		return nil
	}}
	sh := &Sheet{Name: "S", DefaultColumnWidth: 85, RightToLeft: true, Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "A1", nil)}}}}
	sh.Drawings = []Drawing{{
		From: Anchor{Row: 1, Column: 1, DX: 10}, To: &Anchor{Row: 1, Column: 1, DX: 50, DY: 20},
		Width: 40, Height: 20, Unbounded: true, Shape: true, Picture: pic,
		Painted: &typeset.Rect{X: 0, Y: 0, Width: 70, Height: 20},
	}}
	// The region of the printed columns A and B, 170 points wide, runs
	// from the right: the painted box ends 95 points left of its right
	// edge, where its anchors' box would start the drawing 30 points
	// further right.
	out := convert(t, newDoc(sh), office.Options{})
	if want := 170.0 - (85 + 10 + 70); len(got) != 1 || !near(got[0].X, want, 0.1) {
		t.Errorf("drawn at %+v on %d pages, want once from x %.2f in the region", got, len(out.Pages), want)
	}
}
