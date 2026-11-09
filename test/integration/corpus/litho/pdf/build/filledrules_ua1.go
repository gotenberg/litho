package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("filledrules_ua1", filledrulesUa1) }

// filledrulesUa1 builds filledrules_ua1.pdf: a table of wide cells ruled with filled hairline
// rectangles, and two columns of prose under filled bars as the control.
//
// ISO 14289-1, 7.5 reserves Table for content whose meaning is tabular, and 7.2
// asks the structure tree to follow the reading order. Auto-tagging builds the
// tree from the page, and reads a grid whose cells are lines of running text as
// columns of prose unless a rule crosses its gutter. Page 1 is a table of two
// columns of wide cells, each a single line of text, ruled the way Word draws a
// table's borders: every border is a row of filled rectangles half a point
// tall, one per cell and one square at each corner (`re f`), none of which
// crosses the gutter alone, and the column borders are filled the same way. The
// ruling is filled, not stroked, so the grid read as two columns of prose. ISO
// 32000-1, 8.5.3.3 paints a filled path as it does a stroked one, and the page
// now reads as one Table > TR > TD.
//
// Page 2 is the control: two columns of prose set line by line on shared
// baselines, as on prosecolumns_ua1.pdf, under a page title a full-width filled
// bar underlines, more than a line above the columns, with a light shaded panel
// behind both columns that crosses the gutter but is as tall as the text. Each
// column still reads as one P.
//
// The file is untagged on purpose, so remediation to PDF/UA reaches the
// auto-tagger. It declares PDF/UA-1, carries a title, a language and
// DisplayDocTitle, so the rules that fire before remediation are the untagged
// pages and the non-embedded font, and the font is the one residual remediation
// leaves.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func filledrulesUa1() ([]byte, error) {
	table := [][2]string{
		{
			"The structure tree follows the reading order",
			"Clause 7.2, logical structure and order",
		},
		{
			"Tables are tagged with their rows and cells",
			"Clause 7.5, table rows, headers and cells",
		},
		{
			"Every figure carries an alternative text",
			"Clause 7.3, figures and their descriptions",
		},
		{
			"Running heads and folios are artifacts",
			"Clause 7.8, page headers and page footers",
		},
		{
			"The document states its natural language",
			"Clause 7.2, the Lang entry of the catalog",
		},
	}

	left := []string{
		"Accessible documents carry a structure tree",
		"that names what each part of a page is: a",
		"heading, a paragraph, a list, a table or a",
		"figure. Assistive technology reads that tree",
		"rather than the order in which a producer",
		"happened to draw the text, so a page set in",
	}

	right := []string{
		"Some producers set such a page line by line,",
		"the first line of the left column and then",
		"the first line of the right one, on the same",
		"baseline. A bar under the title and a panel",
		"behind the text are no rules between rows,",
		"so each column still reads as one paragraph.",
	}

	// Word's half-point border, and the cell edges of the table on page 1.
	const rule = 0.48
	edges := []float64{66.0, 314.0, 546.0}

	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	num := pyG

	// border is a horizontal border filled cell by cell, a square at every
	// corner.
	border := func(y float64) []byte {
		var out []byte
		for i, x := range edges {
			out = fmt.Appendf(out, "%s %s %s %s re f*\n", num(x), num(y), num(rule), num(rule))
			if i+1 < len(edges) {
				start := x + rule
				out = fmt.Appendf(out, "%s %s %s %s re f*\n",
					num(start), num(y), num(edges[i+1]-start), num(rule))
			}
		}
		return out
	}

	// tablePage is page 1: a title over a table of wide cells ruled by filled
	// hairlines.
	tablePage := func() []byte {
		out := []byte("BT /F1 16 Tf 72 720 Td (A table ruled with filled hairlines) Tj ET\n")
		top := 694.0
		pitch := 20.0
		out = append(out, border(top)...)
		for row, cells := range table {
			y := int(680 - pitch*float64(row))
			out = fmt.Appendf(out, "BT /F1 10 Tf 72 %d Td (%s) Tj ET\n", y, cells[0])
			out = fmt.Appendf(out, "BT /F1 10 Tf 320 %d Td (%s) Tj ET\n", y, cells[1])
			out = append(out, border(top-pitch*float64(row+1))...)
		}
		bottom := top - pitch*float64(len(table))
		for _, x := range edges {
			out = fmt.Appendf(out, "%s %s %s %s re f*\n",
				num(x), num(bottom+rule), num(rule), num(top-bottom-rule))
		}
		return out
	}

	// columnsPage is page 2: a barred title over two prose columns on a
	// shaded panel.
	columnsPage := func() []byte {
		out := []byte("q 0.92 g 66 600 480 92 re f Q\n" +
			"BT /F1 16 Tf 72 720 Td (Two columns of prose under a barred title) Tj ET\n" +
			"72 712 468 1 re f\n")
		for line := range left {
			y := 680 - 12*line
			out = fmt.Appendf(out, "BT /F1 10 Tf 72 %d Td (%s) Tj ET\n", y, left[line])
			out = fmt.Appendf(out, "BT /F1 10 Tf 320 %d Td (%s) Tj ET\n", y, right[line])
		}
		return out
	}

	for _, content := range [][]byte{tablePage(), columnsPage()} {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("Font", d("F1", font)),
			"Contents", pdf.Stream(content),
		))
		pdf.AddPage(page)
	}

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	return save(pdf, "1.7")
}
