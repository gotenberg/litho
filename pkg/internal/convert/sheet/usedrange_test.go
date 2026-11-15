package sheet

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestUsedRange_FormattedColumns locks where the print range of formatted
// cells right of the last value stops: at the end of the formatted cells,
// unless 30 or more columns formatted alike follow, where it stops before
// them, as Calc's ScTable::GetPrintArea does. The formatted cells sit in
// the second row: Calc counts none in the first row of a column without a
// value. Excel counts every formatted cell, whatever follows.
func TestUsedRange_FormattedColumns(t *testing.T) {
	fill := &Style{Fill: Fill{Color: css.Color{R: 1, G: 1, A: 1}}}
	border := &Style{Border: [4]Border{{Line: LineSolid, Width: 1}}}
	run := func(from, to int, st *Style) []Cell {
		var cells []Cell
		for c := from; c <= to; c++ {
			cells = append(cells, Cell{Column: c, Style: st})
		}
		return cells
	}
	tests := []struct {
		name   string
		first  []Cell
		second []Cell
		excel  bool
		want   int
	}{
		{"a short run of fills", []Cell{numCell(0, 1, nil)}, run(1, 5, fill), false, 5},
		{"a run of 40 fills", []Cell{numCell(0, 1, nil)}, run(1, 40, fill), false, 0},
		{"a run of 40 fills, Excel", []Cell{numCell(0, 1, nil)}, run(1, 40, fill), true, 40},
		{"a border, then a run of 40 fills", []Cell{numCell(0, 1, nil)}, append([]Cell{{Column: 2, Style: border}}, run(3, 42, fill)...), false, 2},
		{"fills without a value", nil, run(0, 260, fill), false, 0},
		{"fills in the first row", append([]Cell{numCell(0, 1, nil)}, run(1, 5, fill)...), nil, false, 0},
		{"fills in the first row, Excel", append([]Cell{numCell(0, 1, nil)}, run(1, 5, fill)...), nil, true, 5},
		{"an empty string, Excel", []Cell{numCell(0, 1, nil), {Column: 7, EmptyText: true}}, nil, true, 7},
		{"an empty string, Calc", []Cell{numCell(0, 1, nil), {Column: 7, EmptyText: true}}, nil, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: tc.first}}}
			if tc.second != nil {
				sh.Rows = append(sh.Rows, Row{Index: 1, Cells: tc.second})
			}
			doc := newDoc(sh)
			doc.ExcelRules = tc.excel
			g := newGrid(t.Context(), doc, sh)
			g.overflowBudget = maxOverflowCells
			u, ok := g.usedRange(false)
			if !ok || u.LastColumn != tc.want {
				t.Errorf("used range %+v, want last column %d", u, tc.want)
			}
		})
	}
}

// TestConvert_NoValues locks the page of a book whose cells hold no
// value: its first sheet's first page prints with the borders and fills
// its cells paint, as LibreOffice prints it.
func TestConvert_NoValues(t *testing.T) {
	border := &Style{Border: [4]Border{{Line: LineSolid, Width: 1}, {Line: LineSolid, Width: 1}, {Line: LineSolid, Width: 1}, {Line: LineSolid, Width: 1}}}
	sh := &Sheet{Name: "S", Rows: []Row{{Index: 3, Cells: []Cell{{Column: 2, Style: border}}}}}
	out := convert(t, newDoc(sh), office.Options{})
	if len(out.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(out.Pages))
	}
	strokes := 0
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch f := f.(type) {
			case *typeset.Group:
				walk(f.Content)
			case *typeset.Path:
				if f.Stroke != nil || f.Fill != nil {
					strokes++
				}
			}
		}
	}
	walk(out.Pages[0].Content)
	if strokes == 0 {
		t.Error("the borders of the cell are not drawn")
	}
}

// TestUsedRange_PaintedDrawing extends the print range to the box a
// drawing paints when it has one, not to the cell its To anchor names.
func TestUsedRange_PaintedDrawing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		painted *typeset.Rect
		want    int
	}{
		{"anchors", nil, 5},
		{"painted", &typeset.Rect{X: -10, Y: 0, Width: 70, Height: 20}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", DefaultColumnWidth: 50, Rows: []Row{{Index: 0, Cells: []Cell{numCell(0, 1, nil)}}}}
			sh.Drawings = []Drawing{{
				From: Anchor{Row: 1, Column: 0, DX: 5}, To: &Anchor{Row: 4, Column: 5, DX: 5},
				Unbounded: true, Shape: true, Painted: tc.painted,
			}}
			g := newGrid(t.Context(), newDoc(sh), sh)
			g.overflowBudget = maxOverflowCells
			u, ok := g.usedRange(false)
			if !ok || u.LastColumn != tc.want {
				t.Errorf("used range %+v, want last column %d", u, tc.want)
			}
		})
	}
}
