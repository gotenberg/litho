package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("prosecolumns_ua1", prosecolumnsUa1) }

// prosecolumnsUa1 builds prosecolumns_ua1.pdf: two columns of prose set line by line on shared
// baselines, and a borderless table as the control.
//
// ISO 14289-1, 7.2 asks the structure tree to follow the reading order, and 7.5
// reserves Table for content whose meaning is tabular. Auto-tagging builds the
// tree from the page. Page 1 sets two columns of running text line by line
// across both, each line a text object of its own and every right-hand line on
// the baseline of its left-hand neighbour, with no rule anywhere. Every row of
// the two aligned, so the table pass tagged the columns Table > TR > TD, a grid
// of half-sentences read across the gutter. Each column now reads as one P, left
// then right, as on readcolumns_ua1.pdf, whose baselines sit apart.
//
// Page 2 is the control: a borderless table of three columns of short cells,
// an item, a quantity and a price, still tagged Table > TR > TD.
//
// The file is untagged on purpose, so remediation to PDF/UA reaches the
// auto-tagger. It declares PDF/UA-1, carries a title, a language and
// DisplayDocTitle, so the rules that fire before remediation are the untagged
// pages and the non-embedded font, and the font is the one residual remediation
// leaves.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func prosecolumnsUa1() ([]byte, error) {
	left := []string{
		"Accessible documents carry a structure tree",
		"that names what each part of a page is: a",
		"heading, a paragraph, a list, a table or a",
		"figure. Assistive technology reads that tree",
		"rather than the order in which a producer",
		"happened to draw the text, so a page set in",
		"two columns reads one column after the other",
		"when its tree follows the reading order.",
	}

	right := []string{
		"Some producers set such a page line by line,",
		"the first line of the left column and then",
		"the first line of the right one, on the same",
		"baseline. Rows of the two columns then line",
		"up as the rows of a table do, yet nothing on",
		"the page is tabular: no rule parts the rows,",
		"and every line runs on into the next one of",
		"its own column, never across the gutter.",
	}

	table := [][3]string{
		{"Item", "Quantity", "Price"},
		{"Paper", "12", "4.50"},
		{"Toner", "2", "38.00"},
		{"Staples", "5", "1.20"},
		{"Folders", "30", "9.90"},
	}

	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// columnsPage is page 1: a title over two prose columns sharing their
	// baselines.
	columnsPage := func() string {
		var out strings.Builder
		out.WriteString("BT /F1 16 Tf 72 720 Td (Two columns of prose on shared baselines) Tj ET\n")
		for line := range left {
			y := 680 - 12*line
			fmt.Fprintf(&out, "BT /F1 10 Tf 72 %d Td (%s) Tj ET\n", y, left[line])
			fmt.Fprintf(&out, "BT /F1 10 Tf 320 %d Td (%s) Tj ET\n", y, right[line])
		}
		return out.String()
	}

	// tablePage is page 2: a title over a borderless table of short cells.
	tablePage := func() string {
		var out strings.Builder
		out.WriteString("BT /F1 16 Tf 72 720 Td (A borderless table of short cells) Tj ET\n")
		for row, cells := range table {
			y := 680 - 16*row
			for i, x := range []int{72, 220, 340} {
				fmt.Fprintf(&out, "BT /F1 10 Tf %d %d Td (%s) Tj ET\n", x, y, cells[i])
			}
		}
		return out.String()
	}

	for _, content := range []string{columnsPage(), tablePage()} {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("Font", d("F1", font)),
			"Contents", pdf.Stream([]byte(content)),
		))
		pdf.AddPage(page)
	}

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	return save(pdf, "1.7")
}
