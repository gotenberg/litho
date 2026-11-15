package sheet

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// PrintNotes is where a sheet prints the notes of its cells.
type PrintNotes uint8

const (
	// NotesNone prints no note, the default of both formats (ECMA-376
	// Part 1, 18.18.5 ST_CellComments none; ODF 1.3 Part 3, 20.325
	// style:print without annotations).
	NotesNone PrintNotes = iota
	// NotesAtEnd prints the notes on pages of their own after the sheet's
	// pages (ST_CellComments atEnd; style:print with annotations, the only
	// way Calc prints them).
	NotesAtEnd
	// NotesAsDisplayed prints the notes the sheet shows where they show
	// (ST_CellComments asDisplayed).
	NotesAsDisplayed
)

// Note is the note of a cell (ECMA-376 Part 1, 18.7.3 comment; ODF 1.3
// Part 3, 14.1 office:annotation in a table cell).
type Note struct {
	// Row and Column are the cell's 0-based indexes.
	Row, Column int
	// Text is the note's text as the application prints it, its
	// paragraphs parted by line feeds: a workbook's note with the author's
	// name its application writes into the text.
	Text string
	// Shown marks a note the sheet shows beside its cell (ECMA-376 Part 4,
	// 14.4.2.97 x:Visible; ODF office:display).
	Shown bool
}

// MaxNotes bounds the notes of one sheet. Anti-DoS guard, not
// spec-mandated: each note prints a few lines at the end of its sheet,
// and a reviewed sheet holds tens of notes.
const MaxNotes = 10_000

// MaxNoteText bounds the bytes of a note's text. Anti-DoS guard, not
// spec-mandated: Excel holds at most 32,767 characters in a note.
const MaxNoteText = 32 << 10

// The measures of the pages of notes Calc prints at the end of a sheet
// (ScPrintFunc::PrintNotes): the cell's name and a colon at the left
// margin, the text from a fixed offset, in the default cell font, the
// notes apart by a fixed gap, column by column.
//
// # Corpus evidence
//
//   - probe, ods: style:print with annotations prints, on pages after the
//     sheet's, under its header and above its footer, "A2:" then "B1:",
//     each name at the 2 cm left margin and its text 47.8 points right of
//     it in Liberation Sans 10 points, a note's lines 11.15 points apart,
//     the next note 10 points below the last line of the one before, a
//     long note wrapped at the right margin, the author left out; the
//     shown and the hidden notes alike, none of them drawn on the sheet's
//     own pages. Without annotations in style:print no note prints.
const (
	calcNoteIndent = 47.8
	calcNoteGap    = 10
)

// notePages lays out the notes of the sheet of sp on pages of the sheet's
// paper and margins, appended to its pages, when its page setup prints
// them at the end: as Excel prints a workbook's under its rules
// ([builder.excelNotes]), else as Calc prints them.
func (b *builder) notePages(sp *sheetPlan) error {
	sh := sp.g.sh
	if sh.Page.Notes != NotesAtEnd || len(sh.Notes) == 0 {
		return nil
	}
	if sp.single {
		b.warn(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("sheet %s on one page prints no page of notes", quote(sh.Name)))
		return nil
	}
	notes := slices.Clone(sh.Notes)
	var tree *layout.Node
	if b.doc.ExcelRules {
		// Excel prints the notes row by row.
		slices.SortStableFunc(notes, func(x, y Note) int { return cmp.Or(cmp.Compare(x.Row, y.Row), cmp.Compare(x.Column, y.Column)) })
		tree = b.excelNotes(sp, notes)
	} else {
		slices.SortStableFunc(notes, func(x, y Note) int { return cmp.Or(cmp.Compare(x.Column, y.Column), cmp.Compare(x.Row, y.Row)) })
		tree = b.calcNotes(notes)
	}
	res, err := layout.Layout(b.ctx, tree, b.fonts, layout.Options{
		Page:     layout.PageBox{Width: sp.width, Height: sp.height, Margin: sp.margin},
		Lang:     b.lang,
		Fallback: b.fallback(),
		MaxPages: maxSheetPages,
	})
	if err != nil {
		return fmt.Errorf("laying the notes of sheet %s out: %w", quote(sh.Name), err)
	}
	for i, pg := range res.Pages {
		p := &pagePlan{sp: sp, notes: pg}
		if i == 0 {
			p.noteTags = &typeset.StructElem{Role: "Div", Kids: res.Structure.Kids}
		}
		sp.pages = append(sp.pages, p)
	}
	return nil
}

// noteStyle returns a block of the font f, its lines h points apart.
func (b *builder) noteStyle(f font, h float64) *css.Style {
	st := b.style()
	f.apply(st)
	st.WhiteSpace = css.PreWrap
	st.OverflowWrap = css.BreakWord
	st.LineHeight = css.Pts(h)
	return st
}

// cellName returns the name of the cell at row and col, as A1.
func cellName(row, col int) string {
	return columnName(col) + strconv.Itoa(row+1)
}

// calcNotes returns the notes as Calc prints them: each a line holding the
// cell's name and a colon, then the note's text from [calcNoteIndent],
// [calcNoteGap] below the note before.
func (b *builder) calcNotes(notes []Note) *layout.Node {
	f := resolveFont(b.doc.Default.Font, Font{})
	h := b.m.lineHeight(f)
	root := &layout.Node{Kind: layout.Element, Style: b.style()}
	for i, n := range notes {
		row := b.noteStyle(f, h)
		row.Position = css.Relative
		row.Padding[css.Left] = css.Pts(calcNoteIndent)
		if i > 0 {
			row.Margin[css.Top] = css.Pts(calcNoteGap)
		}
		label := b.noteStyle(f, h)
		label.Position = css.Absolute
		label.Left, label.Top = css.Pts(0), css.Pts(0)
		label.WhiteSpace = css.Pre
		text := b.noteStyle(f, h)
		root.Children = append(root.Children, &layout.Node{Kind: layout.Element, Style: row, Role: "P", Children: []*layout.Node{
			{Kind: layout.Element, Style: label, Children: []*layout.Node{{Kind: layout.Text, Style: label, Text: cellName(n.Row, n.Column) + ":"}}},
			{Kind: layout.Element, Style: text, Children: []*layout.Node{{Kind: layout.Text, Style: text, Text: n.Text}}},
		}})
	}
	return root
}

// The labels of the notes Excel prints at the end of a sheet, in its
// English interface.
const (
	excelCellLabel = "Cell:"
	excelNoteLabel = "Comment:"
)

// excelNotes returns the notes as Excel prints them at the end of a
// sheet: two lines a note, the bold label Cell: before the cell's name
// and the bold label Comment: before the note's text, the labels right
// aligned on the end of the wider, the cell's name and the text a space
// after it, in the book's default font, a line a row of the sheet's
// default height, an empty line between two notes.
//
// # Corpus evidence
//
//   - litho/xlsx: comments_at_end and threaded_comments print, after the
//     sheet's page, a page of notes in Calibri 11 points, B2, C5, A9 and
//     D11 in that order: "Cell:" and "Comment:" bold, ending 48.02 points
//     right of the 0.7 inch margin, the name and the text 2.52 points
//     further, the lines 14.52 points apart as the sheet's rows, two
//     lines from one note to the next; the note's text as it stores it,
//     author's name and line feeds included, a run's colour and weight
//     left out; a threaded comment prints its note's text, which Excel
//     writes for older versions to read.
func (b *builder) excelNotes(sp *sheetPlan, notes []Note) *layout.Node {
	f := resolveFont(b.doc.Default.Font, Font{})
	bold := f
	bold.bold = true
	h := sp.g.sh.DefaultRowHeight
	if h <= 0 {
		h = b.m.lineHeight(f)
	}
	labelW := max(b.noteWidth(bold, excelNoteLabel), b.noteWidth(bold, excelCellLabel))
	gap := b.noteWidth(f, "\u00a0")
	root := &layout.Node{Kind: layout.Element, Style: b.style()}
	line := func(lbl, value string, last bool) *layout.Node {
		row := b.noteStyle(f, h)
		row.Position = css.Relative
		row.Padding[css.Left] = css.Pts(labelW + gap)
		if last {
			row.Margin[css.Bottom] = css.Pts(h)
		}
		label := b.noteStyle(bold, h)
		label.Position = css.Absolute
		label.Left, label.Top = css.Pts(0), css.Pts(0)
		label.Width = css.Pts(labelW)
		label.TextAlign = css.AlignRight
		label.WhiteSpace = css.Pre
		text := b.noteStyle(f, h)
		return &layout.Node{Kind: layout.Element, Style: row, Role: "P", Children: []*layout.Node{
			{Kind: layout.Element, Style: label, Children: []*layout.Node{{Kind: layout.Text, Style: label, Text: lbl}}},
			{Kind: layout.Element, Style: text, Children: []*layout.Node{{Kind: layout.Text, Style: text, Text: value}}},
		}}
	}
	for i, n := range notes {
		root.Children = append(root.Children, line(excelCellLabel, cellName(n.Row, n.Column), false), line(excelNoteLabel, n.Text, i < len(notes)-1))
	}
	return root
}

// NoteText returns s cut to [MaxNoteText] bytes at a line or character
// boundary and whether it cut: the text a reader keeps of a note.
func NoteText(s string) (string, bool) {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= MaxNoteText {
		return s, false
	}
	cut := MaxNoteText
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut], true
}

// noteWidth returns the advance of s in the font f.
func (b *builder) noteWidth(f font, s string) float64 {
	return b.m.runsWidth([]text.ParagraphRun{{Text: s, Style: f.textStyle(b.m.lang, b.m.fallback)}})
}
