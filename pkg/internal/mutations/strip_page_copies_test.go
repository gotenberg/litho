package mutations

import (
	"bytes"
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// thumbFixture builds a page carrying both a /Thumb and a page-level
// /PieceInfo, plus an /Im1 the body may or may not draw. No existing fixture
// has either entry, so without this the strip would never run and the suite
// would pass with the code untouched.
func thumbFixture(t *testing.T, body string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 1 /Height 1 " +
		"/ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Im1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R " +
		"/Contents 6 0 R /Thumb 8 0 R /PieceInfo << /SomeApp << /LastModified (D:20260918) >> >> >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /XObject /Subtype /Image /Width 1 /Height 1 " +
		"/ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")

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

const thumbTextBody = "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"

func TestStripPageCopies_DropsThumbAndPieceInfoAfterARemoval(t *testing.T) {
	// A /Thumb is a producer-baked raster of the page as it was, so it still
	// shows the words the content stream no longer draws.
	doc, page := thumbFixture(t, thumbTextBody)
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	if !slices.Contains(report.DroppedPageEntries, "Thumb") {
		t.Errorf("DroppedPageEntries = %q, want /Thumb", report.DroppedPageEntries)
	}
	if !slices.Contains(report.DroppedPageEntries, "PieceInfo") {
		t.Errorf("DroppedPageEntries = %q, want /PieceInfo", report.DroppedPageEntries)
	}

	pageDict, ok := alloc.Pending()[page.Ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("page object = %T, want the rewritten page dictionary", alloc.Pending()[page.Ref.ObjectNum])
	}
	if _, present := pageDict["Thumb"]; present {
		t.Error("/Thumb survived: the pre-redaction raster is still in the file")
	}
	if _, present := pageDict["PieceInfo"]; present {
		t.Error("/PieceInfo survived")
	}
	// The strip must not take the page apart: the excision's /Contents and the
	// page's own geometry have to remain.
	if _, present := pageDict["Contents"]; !present {
		t.Error("/Contents was lost, so the excision was undone")
	}
	if _, present := pageDict["MediaBox"]; !present {
		t.Error("/MediaBox was lost")
	}
}

func TestStripPageCopies_KeepsThemWhenNothingWasRemoved(t *testing.T) {
	// An empty corner of the page. Nothing was cleared, so there is no reason
	// to throw away the page's thumbnail.
	doc, page := thumbFixture(t, thumbTextBody)
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{10, 10, 40, 40}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	if len(report.DroppedPageEntries) != 0 {
		t.Errorf("DroppedPageEntries = %q, want none", report.DroppedPageEntries)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects, want none: nothing was removed", n)
	}
}

func TestStripPageCopies_DropsThemAfterAnImageOnlyRemoval(t *testing.T) {
	// Blanking an image removes no operator, so this takes the path where no
	// span is excised. A thumbnail left behind there would still show the
	// picture whose samples were just cleared.
	doc, page := thumbFixture(t,
		"q 200 0 0 100 50 600 cm /Im1 Do Q BT /F1 12 Tf 100 300 Td (KEEP) Tj ET")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{60, 610, 240, 690}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	if len(report.BlankedImages) == 0 {
		t.Fatalf("no image was blanked, so this is not exercising the image-only path: %+v", report)
	}
	if len(report.RemovedText) != 0 {
		t.Fatalf("text was removed, so this is not the image-only path: %+v", report)
	}
	if !slices.Contains(report.DroppedPageEntries, "Thumb") {
		t.Errorf("DroppedPageEntries = %q, want /Thumb after an image-only removal", report.DroppedPageEntries)
	}
}

func TestStripPageCopies_PageWithoutThemIsNotRewritten(t *testing.T) {
	// redactFixture carries neither entry. The strip must report nothing and
	// must not queue a pointless rewrite of the page dictionary.
	doc, page := redactFixture(t)
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.DroppedPageEntries) != 0 {
		t.Errorf("DroppedPageEntries = %q, want none", report.DroppedPageEntries)
	}
	// The excision still rewrote the page, so the text removal itself stands.
	if len(report.RemovedText) == 0 {
		t.Errorf("the page text was not removed: %+v", report)
	}
}
