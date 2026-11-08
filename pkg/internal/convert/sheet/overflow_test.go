package sheet

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestUsedRange_Overflow locks how far a text cell extends the printed
// range into the empty columns right of it, the text needing 300 points
// in 64-point columns: four more columns left aligned, two when centred,
// none when right aligned, none past a cell holding a value (the value
// ending the range), none on a
// page of the sheet's content.
func TestUsedRange_Overflow(t *testing.T) {
	tests := []struct {
		name   string
		align  HAlign
		next   bool
		single bool
		want   int
	}{
		{"left", HGeneral, false, false, 6},
		{"centred", HCenter, false, false, 4},
		{"right", HRight, false, false, 2},
		{"stopped by a value", HLeft, true, false, 4},
		{"one page of content", HLeft, false, true, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cells := []Cell{numCell(0, 1, nil), textCell(2, "long", &Style{Align: Alignment{Horizontal: tc.align}})}
			if tc.next {
				cells = append(cells, numCell(4, 2, nil))
			}
			sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: cells}}}
			g := newGrid(newDoc(sh), sh)
			g.need = func(r, c int, cell *Cell) float64 {
				if cell.Value.Kind == Text {
					return 300
				}
				return 0
			}
			g.overflowBudget = maxOverflowCells
			u, ok := g.usedRange(tc.single)
			if !ok || u.LastColumn != tc.want {
				t.Errorf("used range %+v, want last column %d", u, tc.want)
			}
		})
	}
	if w := textWidthCache(10986.6); !near(w, 3122.28, 0.01) {
		t.Errorf("a 10,986.6-point text caches as %v points", w)
	}
}

// TestConvert_OverflowPages locks the pages of a text overflowing past
// a page's columns: the pages it reaches print, and the cells a page of
// them shows are the text's.
func TestConvert_OverflowPages(t *testing.T) {
	long := strings.Repeat("overflow ", 120)
	sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, long, nil)}}}}
	out := convert(t, newDoc(sh), office.Options{})
	if len(out.Pages) < 2 {
		t.Fatalf("%d pages, want the text's", len(out.Pages))
	}
	if rs := runs(out.Pages[1]); len(rs) == 0 {
		t.Error("the second page shows no text")
	}
}
