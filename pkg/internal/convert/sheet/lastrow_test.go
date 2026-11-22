package sheet

import (
	"slices"
	"strings"
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

// TestConvert_TallWrappedTextFits locks that a wrapped cell whose text
// runs many pages' height below its row converts: its cell clips it.
//
// # Corpus evidence
//
//   - poi/xlsx spreadsheet/57523.xlsx: a page of its second sheet failed
//     the conversion; Excel prints the book on 27 pages.
func TestConvert_TallWrappedTextFits(t *testing.T) {
	long := strings.Repeat("word ", 400)
	sh := &Sheet{Name: "S", Columns: []Column{{First: 0, Last: 0, Width: 20}}, Rows: []Row{
		{Index: 0, Height: 15, CustomHeight: true, Cells: []Cell{textCell(0, long, &Style{Align: Alignment{Wrap: true}})}},
		{Index: 1, Cells: []Cell{textCell(0, "next", nil)}},
	}}
	out := convert(t, excelDoc(sh), office.Options{})
	if len(out.Pages) != 1 || !slices.Contains(texts(out.Pages[0]), "next") {
		t.Errorf("%d pages, want 1 holding the next row", len(out.Pages))
	}
}
