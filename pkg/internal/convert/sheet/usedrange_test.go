package sheet

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestUsedRange_FormattedColumns locks where the print range of formatted
// cells right of the last value stops: at the end of the formatted cells,
// unless 30 or more columns formatted alike follow, where it stops before
// them, as Calc's ScTable::GetPrintArea does.
func TestUsedRange_FormattedColumns(t *testing.T) {
	fill := &Style{Fill: Fill{Color: css.Color{R: 1, G: 1, A: 1}}}
	border := &Style{Border: [4]Border{{Line: LineSolid, Width: 1}}}
	tests := []struct {
		name  string
		cells func() []Cell
		want  int
	}{
		{"a short run of fills", func() []Cell {
			cells := []Cell{numCell(0, 1, nil)}
			for c := 1; c <= 5; c++ {
				cells = append(cells, Cell{Column: c, Style: fill})
			}
			return cells
		}, 5},
		{"a run of 40 fills", func() []Cell {
			cells := []Cell{numCell(0, 1, nil)}
			for c := 1; c <= 40; c++ {
				cells = append(cells, Cell{Column: c, Style: fill})
			}
			return cells
		}, 0},
		{"a border, then a run of 40 fills", func() []Cell {
			cells := []Cell{numCell(0, 1, nil), {Column: 2, Style: border}}
			for c := 3; c <= 42; c++ {
				cells = append(cells, Cell{Column: c, Style: fill})
			}
			return cells
		}, 2},
		{"fills without a value", func() []Cell {
			var cells []Cell
			for c := 0; c <= 260; c++ {
				cells = append(cells, Cell{Column: c, Style: fill})
			}
			return cells
		}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: tc.cells()}}}
			g := newGrid(newDoc(sh), sh)
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
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch f := f.(type) {
			case *compose.Group:
				walk(f.Content)
			case *compose.Path:
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
