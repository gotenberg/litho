package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPUAActualTextFanoutBomb pins that the /ActualText search over a structure
// tree whose every element lists the same /K child twice cannot force
// exponential work.
//
// [TestStructTreeFanoutBomb] does not cover this walk. The search runs only
// from the PUA rule, which fires for a font whose /ToUnicode maps into a
// private use area, and no other fan-out fixture carries such a font. The
// search caps recursion at maxStructAttrSearchDepth, but a depth bound counts
// levels, not paths: this chain stays inside the bound while a walk without a
// visited set follows 2^27 of them. No element carries /ActualText, so the
// whole graph is explored before the answer comes back false.
func TestPUAActualTextFanoutBomb(t *testing.T) {
	t.Parallel()

	// <0001> maps to U+E12D, inside the Private Use Area, which is what makes
	// the PUA rule run at all.
	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <0000> <ffff> endcodespacerange\n" +
		"1 beginbfchar <0001> <E12D> endbfchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	const body = "/Span << /MCID 0 >> BDC BT /F1 12 Tf 10 50 Td <0001> Tj ET EMC"

	// Deep enough that an unguarded search cannot finish, shallow enough that
	// maxStructAttrSearchDepth never fires: only the visited set can end it.
	const levels = 28
	const firstElem = 10
	lastElem := firstElem + levels - 1

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /StructParents 0 " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		// A predefined CMap name litho cannot load makes type0CodeSplitter
		// return nil, which is what routes the check into its conservative
		// branch and so into the /ActualText search over the structure tree.
		// Identity-H splits fine and never reaches it.
		"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /NoSuchCMap " +
			"/DescendantFonts [8 0 R] /ToUnicode 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
		fmt.Sprintf("<< /Type /StructTreeRoot /K [%d 0 R %d 0 R] /ParentTree << /Nums [0 [%d 0 R]] >> >>",
			firstElem, firstElem, lastElem),
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /DW 1000 " +
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 9 0 R >>",
		"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 /Ascent 700 " +
			"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
	}
	for i := range levels {
		objNum := firstElem + i
		if i == levels-1 {
			objs = append(objs, "<< /Type /StructElem /S /Span /P 7 0 R /Pg 3 0 R /K 0 >>")
			continue
		}
		child := objNum + 1
		objs = append(objs, fmt.Sprintf(
			"<< /Type /StructElem /S /Div /P 7 0 R /K [%d 0 R %d 0 R] >>", child, child))
	}

	data := cyclePDF(objs...)
	runWithinDeadline(t, data, func(doc *document.Document) {
		_, _ = ValidatePDFA(context.Background(), doc, models.PDFA2a)
	})
}
