package mutations

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// structSplitFixture builds a tagged one-page document, page object 3 with
// /StructParents 0, whose content is body in 12-point Helvetica and whose
// structure tree root is object 8. tree holds object 8 and every object it
// names, numbered 8 and up.
func structSplitFixture(t *testing.T, body, tree string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 8 0 R /MarkInfo << /Marked true >> >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R /StructParents 0 >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		tree +
		"trailer\n<< /Size 40 /Root 1 0 R >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return doc, pages[0]
}

// middleParagraph draws "Keep SECRET words" in one operator under MCID 0. In
// 12-point Helvetica "Keep " ends at x 131.36 and "SECRET" at 180.03, so
// coversMiddleWord takes SECRET and leaves the spaces on either side.
const middleParagraph = "/P <</MCID 0>> BDC BT /F1 12 Tf 100 700 Td (Keep SECRET words) Tj ET EMC"

var coversMiddleWord = [][4]float64{{133, 690, 179, 730}}

// twoLines draws "SECRET" at y 700 under MCID 0 and "KEEP" at y 600 under
// MCID 1; coversSecret takes the first line whole.
const twoLines = "/Span <</MCID 0>> BDC BT /F1 12 Tf 100 700 Td (SECRET) Tj ET EMC " +
	"/Span <</MCID 1>> BDC BT /F1 12 Tf 100 600 Td (KEEP) Tj ET EMC"

// redactStructSplit redacts regions from the page and returns the report, the
// error and the allocator.
func redactStructSplit(t *testing.T, body, tree string, regions [][4]float64) (RedactReport, *writer.ObjAllocator, error) {
	t.Helper()
	doc, page := structSplitFixture(t, body, tree)
	alloc := writer.NewObjAllocator(40)
	report, err := RedactPageRegions(context.Background(), alloc, doc, page, regions)
	return report, alloc, err
}

// TestScrubStructText_KeepsAParagraphWhoseMiddleWordWent pins the split of a
// structure element at a cut: the paragraph's marked-content sequence stays in
// the stream around the words that survived, under the MCID its element and
// the /ParentTree name, so the element needs no rewrite and still tags what
// stayed (ISO 32000-1, 14.7.4.2; ISO 14289-1, 7.1). Before, any element whose
// content went in part refused, /ActualText or not.
func TestScrubStructText_KeepsAParagraphWhoseMiddleWordWent(t *testing.T) {
	t.Parallel()
	tree := "8 0 obj\n<< /Type /StructTreeRoot /K 10 0 R /ParentTree << /Nums [0 [10 0 R]] >> /ParentTreeNextKey 1 >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /P /P 8 0 R /Pg 3 0 R /K 0 >>\nendobj\n"
	report, alloc, err := redactStructSplit(t, middleParagraph, tree, coversMiddleWord)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedMCIDs) != 1 || report.RemovedMCIDs[0] != 0 {
		t.Errorf("RemovedMCIDs = %v, want [0]", report.RemovedMCIDs)
	}
	if strings.Join(report.RemovedText, "") != "SECRET" {
		t.Errorf("RemovedText = %q, want SECRET", report.RemovedText)
	}
	if _, queued := alloc.Pending()[10]; queued {
		t.Errorf("the element was rewritten: %v", alloc.Pending()[10])
	}
	got := string(newPageContents(t, alloc, 3))
	if strings.Contains(got, "SECRET") {
		t.Errorf("the content still draws the removed word: %s", got)
	}
	if !strings.Contains(got, "/P <</MCID 0>> BDC") || !strings.Contains(got, "EMC") {
		t.Errorf("the marked-content sequence did not survive the cut: %s", got)
	}

	doc, page := structSplitFixture(t, got, tree)
	kept := splitGlyphs(t, doc, page, []byte(got))
	var text strings.Builder
	for _, el := range kept {
		if el.MCID != 0 {
			t.Errorf("kept glyph %q carries MCID %d, want 0", el.Text, el.MCID)
		}
		text.WriteString(el.Text)
	}
	if text.String() != "Keep  words" {
		t.Errorf("kept text = %q, want the words on either side of the cut", text.String())
	}
}

// TestScrubStructText_RefusesActualTextSpanningTheCut pins the refusal that
// stays: /ActualText describes the element's whole content (ISO 32000-1,
// 14.9.4), so clearing it takes the description of the words that stayed and
// keeping it keeps the removed one.
func TestScrubStructText_RefusesActualTextSpanningTheCut(t *testing.T) {
	t.Parallel()
	tree := "8 0 obj\n<< /Type /StructTreeRoot /K 10 0 R /ParentTree << /Nums [0 [10 0 R]] >> >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /P /P 8 0 R /Pg 3 0 R /K 0 /ActualText (Keep SECRET words) >>\nendobj\n"
	_, alloc, err := redactStructSplit(t, middleParagraph, tree, coversMiddleWord)
	if !errors.Is(err, ErrStructElementPartlyRemoved) {
		t.Fatalf("err = %v, want ErrStructElementPartlyRemoved", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

// TestScrubStructText_KeepsAnEmptyAltAcrossTheCut pins that an empty /Alt,
// which spells nothing, does not refuse the cut: it holds no copy of the
// removed word, and the element keeps it for the words that stayed.
//
// # Corpus evidence
//
//   - govdocs thread_300_300784.pdf, whole page 1: an /InlineShape with
//     /Alt () over a glyph MCID and a drawing MCID refused once the drawing
//     counted as content kept.
func TestScrubStructText_KeepsAnEmptyAltAcrossTheCut(t *testing.T) {
	t.Parallel()
	tree := "8 0 obj\n<< /Type /StructTreeRoot /K 10 0 R /ParentTree << /Nums [0 [10 0 R]] >> >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /P /P 8 0 R /Pg 3 0 R /K 0 /Alt () /ActualText <FEFF> >>\nendobj\n"
	_, alloc, err := redactStructSplit(t, middleParagraph, tree, coversMiddleWord)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if _, queued := alloc.Pending()[10]; queued {
		t.Errorf("the element was rewritten: %v", alloc.Pending()[10])
	}
}

// TestScrubStructText_SplitsATableCell pins a cell whose text went in part:
// the cell and the table above it hold no text of their own, so both stay as
// they are. A table whose /Alt describes the kept cell as well as the cut one
// refuses, as the element holding it would.
func TestScrubStructText_SplitsATableCell(t *testing.T) {
	t.Parallel()
	body := "/TD <</MCID 0>> BDC BT /F1 12 Tf 100 700 Td (Keep SECRET words) Tj ET EMC " +
		"/TD <</MCID 1>> BDC BT /F1 12 Tf 300 700 Td (Value) Tj ET EMC"
	tree := func(table string) string {
		return "8 0 obj\n<< /Type /StructTreeRoot /K 11 0 R /ParentTree << /Nums [0 [13 0 R 14 0 R]] >> >>\nendobj\n" +
			"11 0 obj\n<< /Type /StructElem /S /Table /P 8 0 R /Pg 3 0 R /K 12 0 R " + table + " >>\nendobj\n" +
			"12 0 obj\n<< /Type /StructElem /S /TR /P 11 0 R /K [13 0 R 14 0 R] >>\nendobj\n" +
			"13 0 obj\n<< /Type /StructElem /S /TD /P 12 0 R /K 0 >>\nendobj\n" +
			"14 0 obj\n<< /Type /StructElem /S /TD /P 12 0 R /K 1 >>\nendobj\n"
	}

	report, alloc, err := redactStructSplit(t, body, tree(""), coversMiddleWord)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedMCIDs) != 1 || report.RemovedMCIDs[0] != 0 {
		t.Errorf("RemovedMCIDs = %v, want [0]", report.RemovedMCIDs)
	}
	for _, num := range []int{11, 12, 13, 14} {
		if _, queued := alloc.Pending()[num]; queued {
			t.Errorf("element %d was rewritten: %v", num, alloc.Pending()[num])
		}
	}

	_, alloc, err = redactStructSplit(t, body, tree("/Alt (Keep SECRET words, Value)"), coversMiddleWord)
	if !errors.Is(err, ErrStructElementPartlyRemoved) {
		t.Fatalf("table /Alt: err = %v, want ErrStructElementPartlyRemoved", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("table /Alt: registered %d objects while refusing", n)
	}
}

// TestScrubStructText_ReadsTheElementsAboveTheRemovedContent pins that the text
// an ancestor holds is read with its whole subtree: a division whose
// /ActualText covers a removed span and a kept one refuses, and one whose
// subtree went whole loses it. The scrub read only the element an MCID named,
// so the division kept a readable copy of the removed span in the second case.
func TestScrubStructText_ReadsTheElementsAboveTheRemovedContent(t *testing.T) {
	t.Parallel()
	spans := "12 0 obj\n<< /Type /StructElem /S /Span /P 11 0 R /K 0 >>\nendobj\n" +
		"13 0 obj\n<< /Type /StructElem /S /Span /P 11 0 R /K 1 >>\nendobj\n"

	mixed := "8 0 obj\n<< /Type /StructTreeRoot /K 10 0 R /ParentTree << /Nums [0 [12 0 R 13 0 R]] >> >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /Div /P 8 0 R /Pg 3 0 R /K 11 0 R /ActualText (SECRET KEEP) >>\nendobj\n" +
		"11 0 obj\n<< /Type /StructElem /S /P /P 10 0 R /K [12 0 R 13 0 R] >>\nendobj\n" + spans
	_, alloc, err := redactStructSplit(t, twoLines, mixed, coversSecret)
	if !errors.Is(err, ErrStructElementPartlyRemoved) {
		t.Fatalf("mixed division: err = %v, want ErrStructElementPartlyRemoved", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("mixed division: registered %d objects while refusing", n)
	}

	whole := "8 0 obj\n<< /Type /StructTreeRoot /K [10 0 R 13 0 R] /ParentTree << /Nums [0 [12 0 R 13 0 R]] >> >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /Div /P 8 0 R /Pg 3 0 R /K 11 0 R /ActualText (SECRET) >>\nendobj\n" +
		"11 0 obj\n<< /Type /StructElem /S /P /P 10 0 R /K 12 0 R >>\nendobj\n" + spans
	_, alloc, err = redactStructSplit(t, twoLines, whole, coversSecret)
	if err != nil {
		t.Fatalf("removed division: RedactPageRegions: %v", err)
	}
	if !clearedActualText(alloc, 10) {
		t.Errorf("the division above the removed span kept /ActualText: %v", alloc.Pending()[10])
	}
	if _, queued := alloc.Pending()[13]; queued {
		t.Errorf("the kept span was rewritten: %v", alloc.Pending()[13])
	}
}

// TestScrubStructText_RefusesTextDescribingAnotherPage pins that content on
// another page counts as kept: an element whose /ActualText also covers the
// rest of a paragraph continued on page 7 refuses, where the scrub used to
// clear it for the page that lost its part.
func TestScrubStructText_RefusesTextDescribingAnotherPage(t *testing.T) {
	t.Parallel()
	tree := "8 0 obj\n<< /Type /StructTreeRoot /K 10 0 R /ParentTree << /Nums [0 [10 0 R 11 0 R]] >> >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /P /P 8 0 R /Pg 3 0 R /K [0 << /Type /MCR /Pg 7 0 R /MCID 4 >>] /ActualText (SECRET continued) >>\nendobj\n" +
		"11 0 obj\n<< /Type /StructElem /S /P /P 8 0 R /Pg 3 0 R /K 1 >>\nendobj\n"
	_, _, err := redactStructSplit(t, twoLines, tree, coversSecret)
	if !errors.Is(err, ErrStructElementPartlyRemoved) {
		t.Fatalf("err = %v, want ErrStructElementPartlyRemoved", err)
	}
}

// TestScrubStructText_ATreeCycleReadsAsKept pins that a /K cycle ends the walk
// and fails closed: the element the cycle comes back to cannot be shown to
// describe removed content alone.
func TestScrubStructText_ATreeCycleReadsAsKept(t *testing.T) {
	t.Parallel()
	tree := "8 0 obj\n<< /Type /StructTreeRoot /K [10 0 R 12 0 R] /ParentTree << /Nums [0 [11 0 R 12 0 R]] >> >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /Div /P 8 0 R /Pg 3 0 R /K 11 0 R /ActualText (SECRET) >>\nendobj\n" +
		"11 0 obj\n<< /Type /StructElem /S /P /P 10 0 R /K [10 0 R 0] >>\nendobj\n" +
		"12 0 obj\n<< /Type /StructElem /S /P /P 8 0 R /K 1 >>\nendobj\n"
	_, _, err := redactStructSplit(t, twoLines, tree, coversSecret)
	if !errors.Is(err, ErrStructElementPartlyRemoved) {
		t.Fatalf("err = %v, want ErrStructElementPartlyRemoved", err)
	}
}

// TestScrubStructText_RefusesADirectElementInTheTree pins that an element
// written into its parent's /K that holds text for removed content refuses: it
// has no object number for the scrubbed copy to take.
func TestScrubStructText_RefusesADirectElementInTheTree(t *testing.T) {
	t.Parallel()
	tree := "8 0 obj\n<< /Type /StructTreeRoot /K [10 0 R 11 0 R] /ParentTree << /Nums [0 [12 0 R 11 0 R]] >> >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /Div /P 8 0 R /Pg 3 0 R /K << /Type /StructElem /S /P /K 12 0 R /ActualText (SECRET) >> >>\nendobj\n" +
		"11 0 obj\n<< /Type /StructElem /S /P /P 8 0 R /K 1 >>\nendobj\n" +
		"12 0 obj\n<< /Type /StructElem /S /Span /K 0 >>\nendobj\n"
	_, _, err := redactStructSplit(t, twoLines, tree, coversSecret)
	if !errors.Is(err, ErrStructElementNotAddressable) {
		t.Fatalf("err = %v, want ErrStructElementNotAddressable", err)
	}
}
