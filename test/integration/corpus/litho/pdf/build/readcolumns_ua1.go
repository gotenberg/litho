package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("readcolumns_ua1", readcolumnsUa1) }

// readcolumnsUa1 builds readcolumns_ua1.pdf: untagged columns set line by line, a running head
// set with the title.
//
// ISO 14289-1, 7.2 asks the structure tree to follow the reading order, and
// auto-tagging builds that tree from the page. On each of the two pages the
// content stream sets two columns line by line across both, each line a text
// object of its own, so no two lines of a column are adjacent in the content. A
// reader takes the left column, then the right one, each one paragraph. ISO
// 32000-1, 14.7.4.1 lets a structure element hold several marked-content
// sequences, so each column is one P whose lines are its sequences. Tagged
// before, each line was a paragraph of its own.
//
// The running head is set in the text object that sets the page's title under
// it, so the object reaches out of the top margin and read as body text: the
// head was tagged as part of the title. ISO 14289-1, 7.8 asks it to be an
// artifact, and it is now cut off the text object as one, its sequence kept
// between the object's BT and ET (ISO 32000-1, 14.6). The page number at the foot
// is a text object of its own.
//
// The right column's baselines sit half a line below the left's, so no row of
// the two reads as a table row.
//
// The file is untagged on purpose, so remediation to PDF/UA reaches the
// auto-tagger. It declares PDF/UA-1, carries a title, a language and
// DisplayDocTitle, so the rules that fire before remediation are the untagged
// pages and the non-embedded font, and the font is the one residual remediation
// leaves.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func readcolumnsUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// pageContent is the content of one page: head and title, the columns, the number.
	pageContent := func(number int) string {
		var out strings.Builder
		fmt.Fprintf(&out, "BT /F1 9 Tf 72 760 Td (Litho corpus: columns set line by line) Tj\n"+
			"/F1 16 Tf 0 -70 Td (Page %d: two columns, one paragraph each) Tj ET\n", number)
		for line := range 6 {
			fmt.Fprintf(&out, "BT /F1 10 Tf 72 %d Td (Left column, page %d, line %d of six.) Tj ET\n",
				650-12*line, number, line+1)
			fmt.Fprintf(&out, "BT /F1 10 Tf 320 %d Td (Right column, page %d, line %d of six.) Tj ET\n",
				644-12*line, number, line+1)
		}
		fmt.Fprintf(&out, "BT /F1 9 Tf 300 40 Td (%d) Tj ET\n", number)
		return out.String()
	}

	for _, number := range []int{1, 2} {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("Font", d("F1", font)),
			"Contents", pdf.Stream([]byte(pageContent(number))),
		))
		pdf.AddPage(page)
	}

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	return save(pdf, "1.7")
}
