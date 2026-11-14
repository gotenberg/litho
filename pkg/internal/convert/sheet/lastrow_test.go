package sheet

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_DrawsTextTallerThanTheLastRow locks that the text of the
// last row of a page, set in a line taller than the row and aligned to
// the row's top or middle, is drawn, clipped to its cell, and not left
// out.
func TestConvert_DrawsTextTallerThanTheLastRow(t *testing.T) {
	for _, v := range []VAlign{VTop, VMiddle, VBottom} {
		st := &Style{Font: Font{Families: []string{"Liberation Sans"}, Size: 20}, Align: Alignment{Vertical: v}}
		sh := &Sheet{Name: "S", Rows: []Row{
			{Index: 0, Cells: []Cell{textCell(0, "first", nil)}},
			{Index: 1, Height: 10, CustomHeight: true, Cells: []Cell{textCell(0, "last", st)}},
		}}
		out := convert(t, newDoc(sh), office.Options{})
		if len(out.Pages) != 1 {
			t.Fatalf("vertical %d: %d pages, want 1", v, len(out.Pages))
		}
		find(t, out.Pages[0], "last")
	}
}
