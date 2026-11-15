package sheet

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// notesSheet returns a sheet of two cells with notes on B1 and A2, printed
// as print says.
func notesSheet(print PrintNotes) *Sheet {
	sh := &Sheet{
		Name: "S",
		Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "a", nil), textCell(1, "b", nil)}}, {Index: 1, Cells: []Cell{textCell(0, "c", nil)}}},
		Notes: []Note{
			{Row: 0, Column: 1, Text: "On B1."},
			{Row: 1, Column: 0, Text: "On A2,\nin two lines."},
		},
	}
	sh.Page = a4()
	sh.Page.Notes = print
	return sh
}

// TestNotes_CalcPrintsThemAtTheEnd holds the page of notes Calc prints
// after a sheet whose page style prints annotations: column by column,
// each cell's name at the left margin and its text calcNoteIndent right of
// it, a line a paragraph, calcNoteGap between two notes; nothing without.
func TestNotes_CalcPrintsThemAtTheEnd(t *testing.T) {
	t.Parallel()
	out := convert(t, newDoc(notesSheet(NotesAtEnd)), office.Options{})
	if len(out.Pages) != 2 {
		t.Fatalf("%d pages, want the sheet's and one of notes", len(out.Pages))
	}
	pg := out.Pages[1]
	a2, b1 := find(t, pg, "A2:"), find(t, pg, "B1:")
	text := find(t, pg, "On A2,")
	second := find(t, pg, "in two lines.")
	if !near(a2.x, 36, 0.01) || !near(text.x, 36+calcNoteIndent, 0.01) || !near(a2.y, text.y, 0.01) {
		t.Errorf("A2: at %.2f, its text at %.2f:\n%s", a2.x, text.x, dump(pg))
	}
	if b1.y >= second.y || !near(second.y-b1.y, calcNoteGap+(a2.y-second.y), 0.05) {
		t.Errorf("B1: at %.2f, after the line at %.2f:\n%s", b1.y, second.y, dump(pg))
	}
	if plain := convert(t, newDoc(notesSheet(NotesNone)), office.Options{}); len(plain.Pages) != 1 {
		t.Errorf("%d pages without the notes printed", len(plain.Pages))
	}
}

// TestNotes_ExcelPrintsThemAtTheEnd holds the page of notes Excel prints
// after a sheet whose page setup asks for them at the end: row by row,
// the labels Cell: and Comment: right aligned on the wider, the name and
// the text after them, a line a row, an empty line between two notes.
func TestNotes_ExcelPrintsThemAtTheEnd(t *testing.T) {
	t.Parallel()
	doc := newDoc(notesSheet(NotesAtEnd))
	doc.ExcelRules = true
	out := convert(t, doc, office.Options{})
	if len(out.Pages) != 2 {
		t.Fatalf("%d pages, want the sheet's and one of notes", len(out.Pages))
	}
	pg := out.Pages[1]
	var order []string
	for _, r := range runs(pg) {
		order = append(order, r.text)
	}
	if got := strings.Join(order, "|"); got != "Cell:|B1|Comment:|On B1.|Cell:|A2|Comment:|On A2,|in two lines." {
		t.Errorf("notes %q", got)
	}
	cell, comment, b1 := find(t, pg, "Cell:"), find(t, pg, "Comment:"), find(t, pg, "B1")
	if !near(comment.x, 36, 0.01) || !near(cell.endX, comment.endX, 0.5) || b1.x <= comment.endX {
		t.Errorf("labels end at %.2f and %.2f, the name starts at %.2f:\n%s", cell.endX, comment.endX, b1.x, dump(pg))
	}
	pitch := cell.y - comment.y
	if !near(pitch, 12.78, 0.01) || !near(comment.y-find(t, pg, "A2").y, 2*pitch, 0.01) {
		t.Errorf("lines %.2f apart:\n%s", pitch, dump(pg))
	}
}
