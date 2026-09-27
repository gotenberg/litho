package mutations

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// taggedFixture builds a tagged one-page document whose text sits inside real
// marked content, so the extractor populates each element's MCID:
//
//	MCID 0 → "SECRET" at y=700
//	MCID 1 → "KEEP"   at y=600
//
// nums is the /ParentTree /Nums body, which decides what each MCID maps to.
// Object 10 describes MCID 0 and object 11 describes MCID 1.
func taggedFixture(t *testing.T, nums string) (*document.Document, *document.Page) {
	t.Helper()
	body := "/P <</MCID 0>> BDC BT /F1 12 Tf 100 700 Td (SECRET) Tj ET EMC " +
		"/P <</MCID 1>> BDC BT /F1 12 Tf 100 600 Td (KEEP) Tj ET EMC"
	return taggedFixtureWith(t, nums, body, "")
}

// taggedFixtureWith builds [taggedFixture]'s document with body as the page's
// content and properties as the entries of its /Properties resource.
func taggedFixtureWith(t *testing.T, nums, body, properties string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 8 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /Properties << " + properties + " >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R /StructParents 0 >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /StructTreeRoot /ParentTree 9 0 R >>\nendobj\n" +
		"9 0 obj\n<< /Nums " + nums + " >>\nendobj\n" +
		"10 0 obj\n<< /Type /StructElem /S /P /ActualText (SECRET) /Alt (the secret) >>\nendobj\n" +
		"11 0 obj\n<< /Type /StructElem /S /P /ActualText (KEEP) >>\nendobj\n" +
		"trailer\n<< /Size 12 /Root 1 0 R >>\n%%EOF\n")

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

// distinctElements maps MCID 0 to object 10 and MCID 1 to object 11.
const distinctElements = "[0 [10 0 R 11 0 R]]"

// coversSecret is the region over the "SECRET" run at y=700, clear of "KEEP".
var coversSecret = [][4]float64{{90, 690, 300, 730}}

func TestScrubStructText_ClearsTheElementForRemovedContent(t *testing.T) {
	// Positive control for the whole vein. If this fails with the element
	// untouched, the page was cleared while the structure tree kept a readable
	// copy of the same words, which is the failure redaction exists to prevent.
	doc, page := taggedFixture(t, distinctElements)
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedMCIDs) != 1 || report.RemovedMCIDs[0] != 0 {
		t.Fatalf("RemovedMCIDs = %v, want [0]", report.RemovedMCIDs)
	}

	scrubbed, ok := alloc.Pending()[10].(ast.PdfDict)
	if !ok {
		t.Fatalf("object 10 = %T, want the rewritten structure element", alloc.Pending()[10])
	}
	if _, present := scrubbed["ActualText"]; present {
		t.Errorf("/ActualText survived: the structure tree still holds the removed text")
	}
	if _, present := scrubbed["Alt"]; present {
		t.Errorf("/Alt survived: it describes the removed content too")
	}
	// The element itself stays: dropping it would detach the page's structure.
	if scrubbed["S"] != ast.PdfName("P") {
		t.Errorf("element lost its /S: %v", scrubbed)
	}
}

// TestScrubStructText_ClearsTheElementOfANamedPropertyList pins that text whose
// marked content names its property list from the page's /Properties (ISO
// 32000-1, 14.6.2) is tied to its structure element as text under an inline
// list is. The extractor read an inline list alone, so the removed text carried
// no MCID and the element kept a readable copy of it in /ActualText.
func TestScrubStructText_ClearsTheElementOfANamedPropertyList(t *testing.T) {
	body := "/P /S0 BDC BT /F1 12 Tf 100 700 Td (SECRET) Tj ET EMC " +
		"/P /S1 BDC BT /F1 12 Tf 100 600 Td (KEEP) Tj ET EMC"
	doc, page := taggedFixtureWith(t, distinctElements, body, "/S0 << /MCID 0 >> /S1 << /MCID 1 >>")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedMCIDs) != 1 || report.RemovedMCIDs[0] != 0 {
		t.Fatalf("RemovedMCIDs = %v, want [0]", report.RemovedMCIDs)
	}
	scrubbed, ok := alloc.Pending()[10].(ast.PdfDict)
	if !ok {
		t.Fatalf("object 10 = %T, want the rewritten structure element", alloc.Pending()[10])
	}
	if _, present := scrubbed["ActualText"]; present {
		t.Errorf("/ActualText survived: the structure tree still holds the removed text")
	}
	if _, queued := alloc.Pending()[11]; queued {
		t.Errorf("the surviving element was rewritten: %v", alloc.Pending()[11])
	}
}

func TestScrubStructText_LeavesTheSurvivingElementAlone(t *testing.T) {
	doc, page := taggedFixture(t, distinctElements)
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	// Object 11 describes "KEEP", which was never selected.
	if _, queued := alloc.Pending()[11]; queued {
		t.Errorf("the surviving element was rewritten: %v", alloc.Pending()[11])
	}
}

func TestScrubStructText_RefusesAnElementSharedWithSurvivingContent(t *testing.T) {
	// Both MCIDs name object 10, so its /ActualText describes the run that went
	// and the run that stayed. Clearing it would take the description of
	// content the caller never selected; keeping it leaves the removed words.
	doc, page := taggedFixture(t, "[0 [10 0 R 10 0 R]]")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret)
	if !errors.Is(err, ErrStructElementPartlyRemoved) {
		t.Errorf("err = %v, want ErrStructElementPartlyRemoved", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestScrubStructText_RefusesADirectElement(t *testing.T) {
	// The entry is a dictionary written straight into the array, so there is no
	// object number for the replacement to take.
	doc, page := taggedFixture(t,
		"[0 [<< /Type /StructElem /S /P /ActualText (SECRET) >> 11 0 R]]")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret)
	if !errors.Is(err, ErrStructElementNotAddressable) {
		t.Errorf("err = %v, want ErrStructElementNotAddressable", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestScrubStructText_UntaggedPageIsUntouched(t *testing.T) {
	// No /StructParents and no /StructTreeRoot: there is no second copy to
	// clear, and the scrub must not refuse over its absence.
	doc, page := redactFixture(t)
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, coversSecret)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedText) == 0 {
		t.Errorf("the page text was not removed: %+v", report)
	}
	// Nothing in this document carries an MCID, so none is reported.
	if len(report.RemovedMCIDs) != 0 {
		t.Errorf("RemovedMCIDs = %v, want none on an untagged page", report.RemovedMCIDs)
	}
}
