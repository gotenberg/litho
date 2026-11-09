package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("readorder_ua1", readorderUa1) }

// readorderUa1 builds readorder_ua1.pdf: an untagged two-column page set out of order.
//
// ISO 14289-1, 7.2 asks the structure tree to follow the reading order, and
// auto-tagging builds that tree from the page. The content stream here does not
// follow it. A title spans both columns, and the stream draws it last. The
// paragraphs of the two columns alternate, left, right, left, right, and the
// right column carries a smaller heading of its own, drawn before the title. A
// page number at the foot is drawn first.
//
// A reader takes the title, the left column, then the right one. Read in content
// order, the tree put the right column's heading first, tagged it H1 with the
// title under it at the same level, and folded the right column's first
// paragraph and the left column's second, adjacent in the stream and a line apart
// in text space, into one paragraph that crossed the gutter.
//
// The file is untagged on purpose, so remediation to PDF/UA reaches the
// auto-tagger. It declares PDF/UA-1, carries a title, a language and
// DisplayDocTitle, so the rules that fire before remediation are the untagged
// page and the non-embedded font, and the font is the one residual remediation
// leaves.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func readorderUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// para is one text object setting lines down from (x, y) at a 12-point
	// leading.
	para := func(label string, x, y, lines int) string {
		shown := make([]string, 0, lines)
		for i := range lines {
			shown = append(shown, fmt.Sprintf("(%s, line %d.) Tj", label, i+1))
		}
		return fmt.Sprintf("BT /F1 10 Tf 12 TL %d %d Td %s ET\n", x, y, strings.Join(shown, " T* "))
	}

	content := "BT /F1 9 Tf 300 40 Td (1) Tj ET\n" +
		"BT /F1 14 Tf 320 712 Td (Right heading) Tj ET\n" +
		para("Left, paragraph one", 72, 696, 3) +
		para("Right, paragraph one", 320, 696, 5) +
		para("Left, paragraph two", 72, 636, 4) +
		para("Right, paragraph two", 320, 612, 3) +
		"BT /F1 24 Tf 72 740 Td (A title across both columns of the page) Tj ET\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	return save(pdf, "1.7")
}
