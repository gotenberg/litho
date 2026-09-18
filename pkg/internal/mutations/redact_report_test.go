package mutations

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// redactScanFixture draws "SECRET" on the page and puts the same word in the
// document's /Info /Title. Clearing the page leaves the /Info copy untouched,
// which is the whole reason a page redaction is not a document redaction.
func redactScanFixture(t *testing.T) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Title (SECRET) >>\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R /Info 7 0 R >>\n%%EOF\n")

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

func TestRedactReport_ReportsTheWholeRemovedRun(t *testing.T) {
	// One models.TextElement is one GLYPH on this path, so the report has to
	// join an operator's glyphs back into the run it drew. Without that this
	// reports ["S" "E" "C" "R" "E" "T"], and handing single letters to a
	// document-wide scan matches nearly every object in a real file.
	doc, page := redactFixture(t)
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	if len(report.RemovedText) != 1 {
		t.Fatalf("RemovedText = %q, want one run", report.RemovedText)
	}
	if report.RemovedText[0] != "SECRET" {
		t.Errorf("RemovedText[0] = %q, want %q", report.RemovedText[0], "SECRET")
	}
}

func TestRedactReport_ReportsNothingWhenNothingWasRemoved(t *testing.T) {
	doc, page := redactFixture(t)
	alloc := writer.NewObjAllocator(20)

	// An empty corner of the page: no glyph's box reaches it.
	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{10, 10, 40, 40}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	if len(report.RemovedText) != 0 || len(report.BlankedImages) != 0 || report.RemovedPaths != 0 {
		t.Errorf("report = %+v, want empty", report)
	}
}

func TestRedactReport_NamesABlankedImage(t *testing.T) {
	doc, page := redactImageFixture(t, imagePageContent)
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{60, 610, 240, 690}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	if len(report.BlankedImages) != 1 || report.BlankedImages[0] != "Im1" {
		t.Errorf("BlankedImages = %q, want [Im1]", report.BlankedImages)
	}
	// The text sits at y=300, well clear of the region.
	if len(report.RemovedText) != 0 {
		t.Errorf("RemovedText = %q, want none", report.RemovedText)
	}
}

func TestRedactReport_CountsARemovedPath(t *testing.T) {
	doc, page := redactPathFixture(t,
		"100 100 50 50 re f BT /F1 12 Tf 100 600 Td (KEEP) Tj ET")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 90, 160, 160}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	if report.RemovedPaths != 1 {
		t.Errorf("RemovedPaths = %d, want 1", report.RemovedPaths)
	}
	// A path carries no text, so there is nothing for a scan to look for.
	if len(report.RemovedText) != 0 {
		t.Errorf("RemovedText = %q, want none: a path draws no text", report.RemovedText)
	}
}

func TestRedactReport_RemovedTextFindsTheCopyThePageEditMissed(t *testing.T) {
	// The point of the whole vein: clearing a page is not clearing a document.
	// The same word sits in /Info, which no page edit touches, and the scan
	// finds it from what the redaction reported having removed.
	doc, page := redactScanFixture(t)
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedText) == 0 {
		t.Fatalf("nothing reported as removed, so there is nothing to scan for")
	}

	res, err := ScanForTokens(context.Background(), doc, report.RemovedText)
	if err != nil {
		t.Fatalf("ScanForTokens: %v", err)
	}

	// Object 7 is the fixture's /Info dictionary. Asserting on that object
	// rather than on "some hit" is what makes this meaningful: the scan reads
	// the parsed document, whose page stream still holds the original text
	// because the rewrite is queued in the allocator, so a bare "found
	// something" would pass even if /Info had been clean.
	found := false
	for _, hit := range res.Hits {
		if hit.ObjectNum == 7 && hit.Token == "SECRET" && hit.Where == "string" {
			found = true
		}
	}
	if !found {
		t.Errorf("the /Info copy was not reported: %+v", res.Hits)
	}
}
