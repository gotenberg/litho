package mutations

import (
	"bytes"
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// twoFontFixture builds a page whose resources bind /F1 to object 4 and /F2 to
// object 8, so a test can redact one font's text and leave the other's. No
// other fixture here uses two fonts, and with one font every outcome looks the
// same.
func twoFontFixture(t *testing.T, body string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"8 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R /F2 8 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
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

func TestRedactFonts_ReportsOnlyTheFontWhollyRemoved(t *testing.T) {
	// /F1 (object 4) draws at y=700 and is redacted whole; /F2 (object 8)
	// draws at y=600 and survives. Reporting both would mean the check is
	// "any font the region touched" rather than "every glyph of this font".
	doc, page := twoFontFixture(t,
		"BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F2 12 Tf 100 600 Td (KEEP) Tj ET")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	if !slices.Contains(report.FontsFullyRemovedOnPage, 4) {
		t.Errorf("FontsFullyRemovedOnPage = %v, want object 4: all of its glyphs went",
			report.FontsFullyRemovedOnPage)
	}
	if slices.Contains(report.FontsFullyRemovedOnPage, 8) {
		t.Errorf("FontsFullyRemovedOnPage = %v, must not name object 8: it still draws KEEP",
			report.FontsFullyRemovedOnPage)
	}
}

func TestRedactFonts_PartlyRemovedFontIsNotReported(t *testing.T) {
	// Both runs use /F1. One is redacted and one is not, so the font still
	// draws on this page and its outlines are not describing only removed text.
	doc, page := twoFontFixture(t,
		"BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	if len(report.RemovedText) == 0 {
		t.Fatalf("nothing was removed, so this proves nothing: %+v", report)
	}
	if len(report.FontsFullyRemovedOnPage) != 0 {
		t.Errorf("FontsFullyRemovedOnPage = %v, want none: /F1 still draws KEEP",
			report.FontsFullyRemovedOnPage)
	}
}

func TestRedactFonts_NothingRemovedReportsNoFonts(t *testing.T) {
	doc, page := twoFontFixture(t,
		"BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F2 12 Tf 100 600 Td (KEEP) Tj ET")
	alloc := writer.NewObjAllocator(20)

	// An empty corner: no glyph's box reaches it.
	report, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{10, 10, 40, 40}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.FontsFullyRemovedOnPage) != 0 {
		t.Errorf("FontsFullyRemovedOnPage = %v, want none", report.FontsFullyRemovedOnPage)
	}
}

func TestRedactFonts_UnattributableFontNeverAppears(t *testing.T) {
	// The resources hold the font dictionary directly, so it has no object
	// number. The sentinel must never be reported as if it were one, and -1 or
	// 0 appearing here would be exactly that.
	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")

	ctx := context.Background()
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
		t.Fatalf("ResolvePages: %v", err)
	}
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(ctx, alloc, doc, pages[0],
		[][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedText) == 0 {
		t.Fatalf("nothing was removed, so this proves nothing: %+v", report)
	}
	for _, ref := range report.FontsFullyRemovedOnPage {
		if ref <= 0 {
			t.Errorf("FontsFullyRemovedOnPage = %v: %d is not an object number",
				report.FontsFullyRemovedOnPage, ref)
		}
	}
}
