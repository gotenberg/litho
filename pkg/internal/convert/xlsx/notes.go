package xlsx

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// cellComments reads where the sheet prints the notes of its cells: the
// cellComments of its pageSetup (ECMA-376 Part 1, 18.3.1.63; 18.18.5
// ST_CellComments), none by default.
func cellComments(ps *ooxml.Element) sheet.PrintNotes {
	switch attr(ps, "cellComments") {
	case "atEnd":
		return sheet.NotesAtEnd
	case "asDisplayed":
		return sheet.NotesAsDisplayed
	}
	return sheet.NotesNone
}

// notes reads the notes of the sheet from its comments part (ECMA-376
// Part 1, 18.7.6 comments; 18.7.3 comment), when its page setup prints
// them: each note's cell and its text, its runs' text joined as Excel
// prints it, the author's name its application writes into the text
// included. A threaded comment (MS-XLSX, 2.6.204) prints through the note
// Excel writes for it, which holds the thread's text. The legacy drawing's
// shape of the note says whether the sheet shows it (Part 4, 14.4.2.97
// x:Visible).
func (w *sheetReader) notes(root *ooxml.Element) {
	r := w.r
	if w.sh.Page.Notes == sheet.NotesNone || !w.printed {
		return
	}
	part := r.pkg.RelatedPart(r.ctx, w.part, ooxml.RelComments)
	if part == "" {
		return
	}
	croot, err := r.pkg.ParseXML(r.ctx, part, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn(warn.PartMalformed, warn.Omitted, fmt.Sprintf("the notes of sheet %q do not parse: %v", w.sh.Name, err))
			return
		}
		r.stop(err)
		return
	}
	list := croot.Child(nsMain, "commentList")
	for _, c := range list.Kids() {
		if !c.Is(nsMain, "comment") {
			continue
		}
		row, col, ok := cellRef(attr(c, "ref"))
		if !ok {
			continue
		}
		if len(w.sh.Notes) >= sheet.MaxNotes {
			r.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("sheet %q holds more than %d notes; the rest are left out", w.sh.Name, sheet.MaxNotes))
			break
		}
		var sb strings.Builder
		noteText(c.Child(nsMain, "text"), &sb, 0)
		if w.sh.Page.Notes == sheet.NotesAsDisplayed {
			if w.noteRuns == nil {
				w.noteRuns = map[[2]int][]text.Run{}
			}
			w.noteRuns[[2]int{row, col}] = noteRuns(c.Child(nsMain, "text"))
		}
		text, cut := sheet.NoteText(sb.String())
		if cut {
			r.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("a note of sheet %q holds more than %d bytes; the rest is left out", w.sh.Name, sheet.MaxNoteText))
		}
		w.sh.Notes = append(w.sh.Notes, sheet.Note{Row: row, Column: col, Text: text})
	}
	if d := root.Child(nsMain, "legacyDrawing"); d != nil {
		id, _ := d.AttrNS(ooxml.NSR, "id")
		w.shownNotes(id)
	}
}

// noteText writes the text of a note's rich text e (18.7.7 text: its t,
// or the t of each of its runs r) to sb, leaving the phonetic runs out.
func noteText(e *ooxml.Element, sb *strings.Builder, depth int) {
	if depth > maxTextDepth {
		return
	}
	for _, k := range e.Kids() {
		switch {
		case k.Is(nsMain, "t"):
			sb.WriteString(k.Text)
		case k.Is(nsMain, "r"):
			noteText(k, sb, depth+1)
		}
	}
}

// shownNotes marks the notes whose shape in the legacy drawing id is
// visible (ECMA-376 Part 4, 14.4.2.97 x:Visible, and the shape's style
// visibility): the notes a sheet printed as displayed shows.
func (w *sheetReader) shownNotes(id string) {
	r := w.r
	rel, ok := r.pkg.RelationshipByID(r.ctx, w.part, id)
	if !ok || rel.External || rel.Type != relVMLDrawing || !r.pkg.Has(rel.Target) {
		return
	}
	root, err := r.pkg.ParseXML(r.ctx, rel.Target, understood)
	if err != nil {
		return
	}
	at := make(map[[2]int]int, len(w.sh.Notes))
	for i, n := range w.sh.Notes {
		at[[2]int{n.Row, n.Column}] = i
	}
	for _, sp := range root.Kids() {
		cd := sp.Child(nsExcelVML, "ClientData")
		if cd == nil || attr(cd, "ObjectType") != "Note" || cd.Child(nsExcelVML, "Visible") == nil {
			continue
		}
		row, err1 := intconv.Atoi32(clientText(cd, "Row"))
		col, err2 := intconv.Atoi32(clientText(cd, "Column"))
		if err1 != nil || err2 != nil {
			continue
		}
		i, ok := at[[2]int{row, col}]
		if !ok || ooxml.ParseVMLStyle(attr(sp, "style"))["visibility"] == "hidden" {
			continue
		}
		w.sh.Notes[i].Shown = true
		if w.sh.Page.Notes == sheet.NotesAsDisplayed {
			w.noteBox(sp, cd, w.sh.Notes[i])
		}
	}
}

// The face of a note's run that names none, and its size: Excel's note
// text.
const (
	noteFamily = "Tahoma"
	noteSize   = 9
)

// noteRuns returns the runs of a note's rich text e (18.7.7 text, 18.4.4
// r with its rPr): each run's text in its face, size, weight and colour,
// Tahoma 9 points by default, black.
func noteRuns(e *ooxml.Element) []text.Run {
	var runs []text.Run
	add := func(t string, rPr *ooxml.Element) {
		st := text.Style{Families: []string{noteFamily}, Size: noteSize, Weight: 400, Color: typeset.Black}
		if f := rPr.Child(nsMain, "rFont"); f != nil && attr(f, "val") != "" {
			st.Families = []string{attr(f, "val")}
		}
		if sz := rPr.Child(nsMain, "sz"); sz != nil {
			if v, err := strconv.ParseFloat(attr(sz, "val"), 64); err == nil && v > 0 && v < 410 {
				st.Size = v
			}
		}
		if b := rPr.Child(nsMain, "b"); b != nil && attr(b, "val") != "0" && attr(b, "val") != "false" {
			st.Weight = 700
		}
		if t != "" && len(runs) < text.MaxLayoutRuns {
			runs = append(runs, text.Run{Text: t, Style: st})
		}
	}
	for _, k := range e.Kids() {
		switch {
		case k.Is(nsMain, "t"):
			add(k.Text, nil)
		case k.Is(nsMain, "r"):
			add(textOf(k.Child(nsMain, "t")), k.Child(nsMain, "rPr"))
		}
	}
	return runs
}

// The measures of a note Excel prints as displayed, in points at its
// default size of 9 points.
//
// # Corpus evidence
//
//   - litho/xlsx: comments_as_displayed prints each shown note as its
//     shape at its x:Anchor: a black shadow 2.04 points right and down,
//     the box filled with its fillcolor #ffffe1 and outlined in black
//     0.75 points wide, its text from 1.85 points inside the left edge,
//     the first baseline 9.36 points below the top, the lines 11.78
//     points apart, wrapped 1.79 points inside each side, the author's
//     bold run first; a black line 0.14 points wide from the box's top
//     left corner to an arrowhead 6.6 points long whose tip is the top
//     right corner of the note's cell. A hidden note prints nothing.
const (
	noteShadow   = 2.04
	noteLine     = 0.75
	noteInsetX   = 1.85
	noteWrapX    = 1.79
	noteBaseline = 9.36
	notePitch    = 11.78
	noteArrowW   = 0.14
	noteHeadLen  = 6.6
	noteHeadHalf = 3
)

// noteBox adds the shape of the note n, shown, to the sheet's drawings, as
// Excel prints a note as displayed: its box at the shape's x:Anchor
// (ECMA-376 Part 4, 14.4.2.2) filled with its fillcolor, a shadow when its
// v:shadow is on, its text, and the arrow from the box to its cell,
// drawn first.
func (w *sheetReader) noteBox(sp, cd *ooxml.Element, n sheet.Note) {
	r := w.r
	from, to, ok := clientAnchor(clientText(cd, "Anchor"))
	if !ok || len(w.sh.Drawings)+2 > maxDrawings {
		return
	}
	fill := css.Color{R: 0xFF, G: 0xFF, B: 0xE1, A: 1}
	if c, ok := ooxml.VMLColor(attr(sp, "fillcolor")); ok {
		fill = c
	}
	shadow := false
	if s := sp.Child(ooxml.NSV, "shadow"); s != nil {
		shadow = ooxml.OnOff(attr(s, "on"), false)
	}
	tip := sheet.Anchor{Row: n.Row, Column: min(n.Column+1, maxColumns-1)}
	arrow := sheet.Drawing{Shape: true, Decorative: true, Name: "note arrow"}
	// The line runs from the box's top-left corner to the cell's top-right
	// one: the drawing spans the two, which way the line crosses it
	// following which corner is left and which above.
	left, right := tip, from
	if before(from.Column, from.DX, tip.Column, tip.DX) {
		left, right = from, tip
	}
	top, bottom := tip, from
	if before(from.Row, from.DY, tip.Row, tip.DY) {
		top, bottom = from, tip
	}
	arrow.From = sheet.Anchor{Column: left.Column, DX: left.DX, Row: top.Row, DY: top.DY}
	arrow.To = &sheet.Anchor{Column: right.Column, DX: right.DX, Row: bottom.Row, DY: bottom.DY}
	boxLeft, boxTop := left == from, top == from
	arrow.Picture = &layout.Picture{Width: 1, Height: 1, Vector: func(_ context.Context, b typeset.Rect) []typeset.Fragment {
		bx, by, tx, ty := b.X, b.Y, b.X+b.Width, b.Y+b.Height
		if !boxLeft {
			bx, tx = tx, bx
		}
		if !boxTop {
			by, ty = ty, by
		}
		return noteArrow(bx, by, tx, ty)
	}}
	runs := w.noteRuns[[2]int{n.Row, n.Column}]
	fonts := r.fonts
	box := sheet.Drawing{From: from, To: &to, Shape: true, Name: "note", Alt: n.Text}
	box.Picture = &layout.Picture{Width: 1, Height: 1, Vector: func(ctx context.Context, b typeset.Rect) []typeset.Fragment {
		var out []typeset.Fragment
		if shadow {
			out = append(out, filled(rectOps(typeset.Rect{X: b.X + noteShadow, Y: b.Y + noteShadow, Width: b.Width, Height: b.Height}), ctrlBlack))
		}
		out = append(out, &typeset.Path{Ops: rectOps(b), Fill: &typeset.Fill{Color: typesetColor(fill)}, Stroke: &typeset.Stroke{Color: ctrlBlack, Width: noteLine}})
		return append(out, noteLines(ctx, fonts, runs, b)...)
	}}
	w.sh.Drawings = append(w.sh.Drawings, arrow, box)
}

// before reports whether the place (cell a, offset da) comes before (cell
// b, offset db) along a sheet's axis.
func before(a int, da float64, b int, db float64) bool {
	return a < b || a == b && da < db
}

// noteArrow returns the line from the box corner (bx, by) to the cell's
// corner (tx, ty), ending in a filled arrowhead whose tip is the cell's
// corner.
func noteArrow(bx, by, tx, ty float64) []typeset.Fragment {
	dx, dy := tx-bx, ty-by
	l := math.Sqrt(float64(dx*dx) + float64(dy*dy))
	if l <= 0 {
		return nil
	}
	ux, uy := dx/l, dy/l
	baseX, baseY := tx-float64(ux*noteHeadLen), ty-float64(uy*noteHeadLen)
	line := stroked([]typeset.PathOp{moveOp(bx, by), lineOp(baseX, baseY)}, ctrlBlack)
	line.Stroke.Width = noteArrowW
	head := filled([]typeset.PathOp{
		moveOp(tx, ty),
		lineOp(baseX-float64(uy*noteHeadHalf), baseY+float64(ux*noteHeadHalf)),
		lineOp(baseX+float64(uy*noteHeadHalf), baseY-float64(ux*noteHeadHalf)),
		{Kind: typeset.ClosePath},
	}, ctrlBlack)
	return []typeset.Fragment{line, head}
}

// noteLines lays a note's runs out in its box b and returns their glyph
// runs: wrapped inside the box's sides, the first baseline and the line
// pitch at Excel's distances scaled by the first run's size.
func noteLines(ctx context.Context, fonts *text.Fonts, runs []text.Run, b typeset.Rect) []typeset.Fragment {
	width := b.Width - 2*noteWrapX
	if fonts == nil || len(runs) == 0 || width <= 0 {
		return nil
	}
	lines, _, err := fonts.Layout(ctx, runs, width, text.Options{Whitespace: text.Preserve})
	if err != nil {
		return nil
	}
	k := runs[0].Style.Size / noteSize
	var out []typeset.Fragment
	for i := range lines {
		y := b.Y + float64(noteBaseline*k) + float64(float64(i)*notePitch*k)
		if y > b.Y+b.Height {
			// The box clips the text it cannot hold.
			break
		}
		out = append(out, placeRuns(&lines[i], b.X+noteInsetX, y)...)
	}
	return out
}
