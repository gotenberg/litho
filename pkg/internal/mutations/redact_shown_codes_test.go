package mutations

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// shownCodesFixture builds a one-page document whose content is body, with
// Helvetica as /F1 and, as /F2, an Identity-H Type0 font whose /ToUnicode maps
// code 0041 alone.
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
		"5 0 obj\n<< /Font << /F1 4 0 R /F2 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /Font /Subtype /Type0 /BaseFont /Litho /Encoding /Identity-H /DescendantFonts [8 0 R] /ToUnicode 9 0 R >>\nendobj\n" +
		"8 0 obj\n<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Litho /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /DW 1000 >>\nendobj\n" +
		"9 0 obj\n<< /Length " + strconv.Itoa(len(cmap)) + " >>\nstream\n" + cmap + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 10 /Root 1 0 R >>\n%%EOF\n")
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
// operator showing code 0000, which the font's /ToUnicode leaves unmapped:
// extraction reads no glyph and no advance for it, so the glyph a viewer
// draws cannot be ruled out of the region, as litho corpus cmaps_2b,
// type0nodesc_2b and usecmap_2u show.
func TestRedactPageRegions_RefusesAPageShowingUnplaceableCodes(t *testing.T) {
	doc, page := shownCodesFixture(t, "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F2 12 Tf 100 600 Td <00000041> Tj ET")
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
