package ods

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// note adds the note an office:annotation of the cell at row and col
// carries (ODF 1.3, Part 3, 14.1) to the sheet: its paragraphs, a line
// each, as Calc prints a note; whether Calc shows it on the sheet
// (office:display). The page style says whether the notes print
// (sheet.PageSetup Notes).
func (r *reader) note(sh *sheet.Sheet, a *odf.Element, row, col int) {
	if len(sh.Notes) >= sheet.MaxNotes {
		r.warn("notes", warn.LimitReached, warn.Omitted, fmt.Sprintf("sheet %q holds more than %d notes; the rest are left out", sh.Name, sheet.MaxNotes))
		return
	}
	var paras []string
	for _, p := range a.Children {
		if p.Is(odf.NSText, "p") || p.Is(odf.NSText, "h") {
			var sb strings.Builder
			noteRun(p, &sb, 0)
			paras = append(paras, sb.String())
		}
	}
	text, cut := sheet.NoteText(strings.Join(paras, "\n"))
	if cut {
		r.warn("note-text", warn.LimitReached, warn.Omitted, fmt.Sprintf("a note of sheet %q holds more than %d bytes; the rest is left out", sh.Name, sheet.MaxNoteText))
	}
	sh.Notes = append(sh.Notes, sheet.Note{Row: row, Column: col, Text: text, Shown: a.A(odf.NSOffice, "display") == "true"})
}

// noteRun writes the text of a note's paragraph el to sb: a text:s as its
// spaces, a text:tab as a tab, a text:line-break as a line feed (ODF 1.3,
// Part 3, 6.1), spans and links entered.
func noteRun(el *odf.Element, sb *strings.Builder, depth int) {
	if depth > maxTextDepth {
		return
	}
	for _, k := range el.Children {
		switch {
		case k.IsText():
			sb.WriteString(k.Text)
		case k.Is(odf.NSText, "s"):
			n := 1
			if v, err := intconv.Atoi(k.A(odf.NSText, "c")); err == nil && v > 1 {
				n = min(v, maxSpaces)
			}
			sb.WriteString(strings.Repeat(" ", n))
		case k.Is(odf.NSText, "tab"):
			sb.WriteByte('\t')
		case k.Is(odf.NSText, "line-break"):
			sb.WriteByte('\n')
		case k.Name.Space == odf.NSText:
			noteRun(k, sb, depth+1)
		}
	}
}
