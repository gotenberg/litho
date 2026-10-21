package mutations

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// shownCodesFixture builds a one-page document whose content is body, with
// Helvetica as /F1, as /F2 an Identity-H Type0 font whose /ToUnicode maps code
// 0041 alone, and as /F3 the same font without /DescendantFonts.
func shownCodesFixture(t *testing.T, body string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()
	cmap := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange " +
		"1 beginbfchar <0041> <0041> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R /F2 7 0 R /F3 10 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /Font /Subtype /Type0 /BaseFont /Litho /Encoding /Identity-H /DescendantFonts [8 0 R] /ToUnicode 9 0 R >>\nendobj\n" +
		"8 0 obj\n<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Litho /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /DW 1000 >>\nendobj\n" +
		"9 0 obj\n<< /Length " + strconv.Itoa(len(cmap)) + " >>\nstream\n" + cmap + "\nendstream\nendobj\n" +
		"10 0 obj\n<< /Type /Font /Subtype /Type0 /BaseFont /Litho /Encoding /Identity-H /ToUnicode 9 0 R >>\nendobj\n" +
		"trailer\n<< /Size 11 /Root 1 0 R >>\n%%EOF\n")
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

// TestRedactPageRegions_RefusesRemovingClippingText covers text in rendering
// mode 7 masking a fill drawn after it, as pdfjs text_clip_cff_cid draws its
// glyphs. Removing the text whole widened the clip, and the fill painted as a
// solid rectangle outside the region.
func TestRedactPageRegions_RefusesRemovingClippingText(t *testing.T) {
	doc, page := shownCodesFixture(t, "BT 7 Tr /F1 12 Tf 100 700 Td (SECRET) Tj ET 0 0 1 rg 90 600 300 200 re f")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{0, 690, 612, 730}})
	if !errors.Is(err, ErrRegionCoversClippingText) {
		t.Fatalf("got %v, want ErrRegionCoversClippingText", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("a refusal registered %d objects", n)
	}
}

// TestRedactPageRegions_RefusesAnAmbiguousTextPosition covers litho corpus
// qshyphen_2b's fourth line: a Td inside q...Q in a text object, which
// poppler keeps after Q and litho undoes, so the word after it sits on two
// lines. A region over poppler's line kept the word poppler draws there.
func TestRedactPageRegions_RefusesAnAmbiguousTextPosition(t *testing.T) {
	doc, page := shownCodesFixture(t, "BT /F1 12 Tf 100 700 Td (soft) Tj q 0 -30 Td Q (ware) Tj ET")
	_, err := RedactPageRegions(context.Background(), writer.NewObjAllocator(20), doc, page, [][4]float64{{0, 665, 612, 680}})
	if !errors.Is(err, ErrTextPositionAmbiguous) {
		t.Fatalf("got %v, want ErrTextPositionAmbiguous", err)
	}
	for _, body := range []string{
		"BT /F1 12 Tf 100 700 Td (soft) Tj q 1 0 0 rg Q (ware) Tj ET",
		"BT /F1 12 Tf 100 700 Td q 0 -30 Td Q 1 0 0 1 100 700 Tm (ware) Tj ET",
		"q BT /F1 12 Tf 100 700 Td (soft) Tj ET Q BT /F1 12 Tf 100 600 Td (ware) Tj ET",
	} {
		doc, page := shownCodesFixture(t, body)
		if _, err := RedactPageRegions(context.Background(), writer.NewObjAllocator(20), doc, page, [][4]float64{{0, 665, 612, 680}}); err != nil {
			t.Errorf("%q: %v", body, err)
		}
	}
}

// TestRedactPageRegions_RefusesAPageShowingUnplaceableCodes covers a kept
// operator showing code 0000, which the font's /ToUnicode leaves unmapped,
// through a Type 0 font naming no CIDFont: ISO 32000-1, 9.7.4.3 then states no
// width for it, so the glyph a viewer draws, and every code after it, cannot
// be ruled out of the region, as litho corpus cmaps_2b and type0nodesc_2b
// show.
func TestRedactPageRegions_RefusesAPageShowingUnplaceableCodes(t *testing.T) {
	doc, page := shownCodesFixture(t, "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F3 12 Tf 100 600 Td <00000041> Tj ET")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{0, 690, 612, 730}})
	if !errors.Is(err, ErrTextNotPlaceable) {
		t.Fatalf("got %v, want ErrTextNotPlaceable", err)
	}

	// Removed whole with the rest of the page, the code goes too.
	alloc = writer.NewObjAllocator(20)
	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{0, 0, 612, 792}}); err != nil {
		t.Fatalf("whole page: %v", err)
	}
	if got := newPageContents(t, alloc, page.Ref.ObjectNum); bytes.Contains(got, []byte("<00000041>")) {
		t.Errorf("the operator is still in the page content: %q", got)
	}
}

// TestRedactPageRegions_PlacesUnmappedCodes covers code 0000 of an Identity-H
// font over a CIDFont, which the /ToUnicode leaves unmapped, as litho corpus
// usecmap_2u shows: it draws a glyph /DW wide (ISO 32000-1, 9.7.4.3), so a
// region elsewhere keeps its operator, and a region over the glyph after it,
// placed one /DW advance on, splits it there.
func TestRedactPageRegions_PlacesUnmappedCodes(t *testing.T) {
	doc, page := shownCodesFixture(t, "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F2 12 Tf 100 600 Td <00000041> Tj ET")
	if _, err := RedactPageRegions(context.Background(), writer.NewObjAllocator(20), doc, page, [][4]float64{{0, 690, 612, 730}}); err != nil {
		t.Fatalf("region over SECRET: %v", err)
	}

	// Code 0000 spans x 100 to 112 at 12 Tf; 0041 spans 112 to 124.
	alloc := writer.NewObjAllocator(20)
	report, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{113, 590, 123, 620}})
	if err != nil {
		t.Fatalf("region over 0041: %v", err)
	}
	if !slices.Contains(report.RemovedText, "A") {
		t.Errorf("RemovedText = %q, want the A shown after code 0000", report.RemovedText)
	}
	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	if bytes.Contains(got, []byte("0041")) {
		t.Errorf("code 0041 is still in the page content: %q", got)
	}
	if !bytes.Contains(got, []byte("<0000>")) {
		t.Errorf("code 0000, outside the region, is gone: %q", got)
	}
}

// TestRedactPageRegions_ReportsEveryRemovedGlyphBox pins that the report holds
// the box of every glyph removed, the space among them, which extraction reads
// as no text and a caller checking the page would otherwise not see go.
func TestRedactPageRegions_ReportsEveryRemovedGlyphBox(t *testing.T) {
	doc, page := shownCodesFixture(t, "BT /F1 12 Tf 100 700 Td (A B) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET")
	report, err := RedactPageRegions(context.Background(), writer.NewObjAllocator(20), doc, page, [][4]float64{{0, 690, 612, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedGlyphBoxes) != 3 {
		t.Fatalf("RemovedGlyphBoxes = %v, want the boxes of A, the space and B", report.RemovedGlyphBoxes)
	}
	for i, b := range report.RemovedGlyphBoxes {
		if b[0] < 100 || b[2] <= b[0] || b[1] > 700 || b[3] <= 700 {
			t.Errorf("box %d = %v, want one on the line at 700 from x 100", i, b)
		}
	}
	if s := report.RemovedGlyphBoxes[1]; s[0] < report.RemovedGlyphBoxes[0][2]-0.01 {
		t.Errorf("the space's box %v starts before A's ends at %v", s, report.RemovedGlyphBoxes[0][2])
	}
}
