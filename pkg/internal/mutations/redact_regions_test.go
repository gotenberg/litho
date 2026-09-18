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

// redactFixture builds a one-page document with a Helvetica font and two text
// runs: "SECRET" at y=700 and "KEEP" at y=600.
func redactFixture(t *testing.T) (*document.Document, *document.Page) {
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
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")

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

func TestRedactPageRegions_RemovesTheCoveredRunAndKeepsTheRest(t *testing.T) {
	doc, page := redactFixture(t)
	alloc := writer.NewObjAllocator(20)

	// Covers the "SECRET" run at y=700 and nothing at y=600.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	if bytes.Contains(got, []byte("SECRET")) {
		t.Error("the covered text is still in the page content")
	}
	if !bytes.Contains(got, []byte("KEEP")) {
		t.Errorf("text outside the region was removed too: %q", got)
	}
}

func TestRedactPageRegions_NoRegionsLeavesThePageAlone(t *testing.T) {
	doc, page := redactFixture(t)
	alloc := writer.NewObjAllocator(20)

	if err := RedactPageRegions(context.Background(), alloc, doc, page, nil); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects, want none", n)
	}
}

func TestRedactPageRegions_RegionCoveringNothingLeavesThePageAlone(t *testing.T) {
	doc, page := redactFixture(t)
	alloc := writer.NewObjAllocator(20)

	// Empty corner of the page: no glyph's box reaches it.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{10, 10, 40, 40}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects, want none: nothing was selected", n)
	}
}

func TestRedactPageRegions_RefusesPartialOperatorCoverage(t *testing.T) {
	doc, page := redactFixture(t)
	alloc := writer.NewObjAllocator(20)

	// Clips the left edge of "SECRET": some of its glyphs are inside the
	// region and some are not. Removing the operator would take the rest of
	// the word with it, so the mutation refuses.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 690, 110, 730}})
	if !errors.Is(err, ErrPartialOperatorCoverage) {
		t.Errorf("err = %v, want ErrPartialOperatorCoverage", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestRedactPageRegions_RefusesAnUnaddressablePage(t *testing.T) {
	doc, _ := redactFixture(t)
	alloc := writer.NewObjAllocator(20)

	inlined := &document.Page{Ref: ast.PdfIndirectRef{ObjectNum: 0}, Dict: ast.PdfDict{}}
	err := RedactPageRegions(context.Background(), alloc, doc, inlined,
		[][4]float64{{0, 0, 100, 100}})
	if !errors.Is(err, ErrPageNotAddressable) {
		t.Errorf("err = %v, want ErrPageNotAddressable", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

// redactPathFixture builds a one-page document with a Helvetica font whose
// content stream is body, so a test can mix painted paths and text freely.
func redactPathFixture(t *testing.T, body string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")

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

func TestRedactPageRegions_RemovesAFilledPathInsideTheRegion(t *testing.T) {
	doc, page := redactPathFixture(t,
		"100 100 50 50 re f BT /F1 12 Tf 100 600 Td (KEEP) Tj ET")
	alloc := writer.NewObjAllocator(20)

	// Covers the rectangle at (100,100)-(150,150) and nothing at y=600.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 90, 160, 160}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	// The construction operator has to go with the paint operator: leaving the
	// re behind would hand the rectangle to whatever painted next.
	if bytes.Contains(got, []byte("re")) {
		t.Errorf("the path's construction operator survived: %q", got)
	}
	if bytes.Contains(got, []byte(" f")) {
		t.Errorf("the path's paint operator survived: %q", got)
	}
	if !bytes.Contains(got, []byte("KEEP")) {
		t.Errorf("text outside the region was removed too: %q", got)
	}
}

func TestRedactPageRegions_RefusesAPathStraddlingTheRegion(t *testing.T) {
	doc, page := redactPathFixture(t, "100 100 100 100 re f")
	alloc := writer.NewObjAllocator(20)

	// The rectangle runs to (200,200) and the region stops at 160, so removing
	// the path would take ink the caller never selected.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 90, 160, 160}})
	if !errors.Is(err, ErrPathStraddlesRegion) {
		t.Errorf("err = %v, want ErrPathStraddlesRegion", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestRedactPageRegions_RefusesAStrokedPath(t *testing.T) {
	doc, page := redactPathFixture(t, "100 100 50 50 re S")
	alloc := writer.NewObjAllocator(20)

	// A stroke spreads half a line width past the geometry the machine
	// records, so the bounds cannot prove the ink stays inside the region.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 90, 160, 160}})
	if !errors.Is(err, ErrRegionCoversAStrokedPath) {
		t.Errorf("err = %v, want ErrRegionCoversAStrokedPath", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestRedactPageRegions_RefusesAPathThatAlsoClips(t *testing.T) {
	doc, page := redactPathFixture(t, "100 100 50 50 re W f")
	alloc := writer.NewObjAllocator(20)

	// The path paints and narrows the clip, so removing it would widen the
	// clip for everything drawn after it.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 90, 160, 160}})
	if !errors.Is(err, ErrRegionCoversAClippedPath) {
		t.Errorf("err = %v, want ErrRegionCoversAClippedPath", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestRedactPageRegions_ClipViaNDoesNotTaintTheNextPath(t *testing.T) {
	// The W n sets a clip and paints nothing, so it is not a path to remove.
	// The filled rectangle after it is an ordinary removable one. If the n let
	// the clip flag outlive it, this would refuse instead of removing.
	doc, page := redactPathFixture(t, "0 0 50 50 re W n 100 100 50 50 re f")
	alloc := writer.NewObjAllocator(20)

	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 90, 160, 160}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	if bytes.Contains(got, []byte("100 100 50 50 re")) {
		t.Errorf("the filled path inside the region survived: %q", got)
	}
	if !bytes.Contains(got, []byte("0 0 50 50 re W n")) {
		t.Errorf("the clipping path outside the region was removed: %q", got)
	}
}

func TestRedactPageRegions_PathAwayFromTheRegionIsLeftAlone(t *testing.T) {
	doc, page := redactPathFixture(t, "400 400 50 50 re f")
	alloc := writer.NewObjAllocator(20)

	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 90, 160, 160}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects, want none: the path is outside the region", n)
	}
}

// redactImageFixture builds a one-page document whose content places an image
// at (50,600)-(250,700) and draws "KEEP" far below it, so a region can meet
// either one without the other.
func redactImageFixture(t *testing.T, body string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Im1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 1 /Height 1 " +
		"/ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")

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

// imagePageContent places Im1 at (50,600) sized 200x100, then draws "KEEP" at
// (100,300), well clear of it.
const imagePageContent = "q 200 0 0 100 50 600 cm /Im1 Do Q BT /F1 12 Tf 100 300 Td (KEEP) Tj ET"

func TestRedactPageRegions_RefusesARegionCoveringAnImage(t *testing.T) {
	doc, page := redactImageFixture(t, imagePageContent)
	alloc := writer.NewObjAllocator(20)

	// Squarely inside the image's box. Removal deletes whole operators and
	// cannot blank pixels, so clearing this region is not something it can do.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{60, 610, 240, 690}})
	if !errors.Is(err, ErrRegionCoversAnImage) {
		t.Errorf("err = %v, want ErrRegionCoversAnImage", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestRedactPageRegions_ImageAwayFromTheRegionDoesNotRefuse(t *testing.T) {
	doc, page := redactImageFixture(t, imagePageContent)
	alloc := writer.NewObjAllocator(20)

	// An image on the page must not block redaction by existing; only a region
	// that actually meets it is refused. This one covers the text instead.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 290, 300, 330}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	if bytes.Contains(got, []byte("KEEP")) {
		t.Errorf("the covered text is still in the page content: %q", got)
	}
	if !bytes.Contains(got, []byte("/Im1 Do")) {
		t.Errorf("the image invocation was removed too: %q", got)
	}
}

// inlineImagePageContent paints an inline image over the same box
// imagePageContent uses for Im1, then draws "KEEP" well clear of it. The
// fixture's /XObject resource goes unreferenced, so the inline image is the
// only one on the page and a refusal can only have come from it.
//
// ID is followed by exactly one whitespace byte, and EI must be preceded by
// whitespace, which is what the parser's inline-image scan expects.
const inlineImagePageContent = "q 200 0 0 100 50 600 cm BI /W 1 /H 1 /CS /G /BPC 8 ID \x00 EI Q " +
	"BT /F1 12 Tf 100 300 Td (KEEP) Tj ET"

func TestRedactPageRegions_RefusesARegionCoveringAnInlineImage(t *testing.T) {
	doc, page := redactImageFixture(t, inlineImagePageContent)
	alloc := writer.NewObjAllocator(20)

	// An inline image carries its pixels in the content stream and no Do names
	// it, so it is a second way a page paints over a region. Removal still
	// cannot blank those pixels.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{60, 610, 240, 690}})
	if !errors.Is(err, ErrRegionCoversAnImage) {
		t.Errorf("err = %v, want ErrRegionCoversAnImage", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestRedactPageRegions_InlineImageAwayFromTheRegionDoesNotRefuse(t *testing.T) {
	doc, page := redactImageFixture(t, inlineImagePageContent)
	alloc := writer.NewObjAllocator(20)

	// An inline image elsewhere on the page must not block redaction, or a page
	// carrying one anywhere could never be cleared.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{90, 290, 300, 330}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	if bytes.Contains(got, []byte("KEEP")) {
		t.Errorf("the covered text is still in the page content: %q", got)
	}
	if !bytes.Contains(got, []byte("BI ")) {
		t.Errorf("the inline image was removed too: %q", got)
	}
}

func TestRedactPageRegions_RefusesAnUnresolvableXObject(t *testing.T) {
	doc, page := redactImageFixture(t, "q 1 0 0 1 0 0 cm /Missing Do Q")
	alloc := writer.NewObjAllocator(20)

	// The name is not in /XObject, so the machine cannot place what this Do
	// draws and cannot rule it out of the region either.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{0, 0, 10, 10}})
	if !errors.Is(err, ErrXObjectUnresolved) {
		t.Errorf("err = %v, want ErrXObjectUnresolved", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

// redactFormFixture builds a one-page document whose only content invokes a
// form XObject that paints text from the form's own stream.
func redactFormFixture(t *testing.T) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	form := "BT /F1 12 Tf (ZZ) Tj ET"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Fm1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length 7 >>\nstream\n/Fm1 Do\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 200 200] " +
		"/Resources << /Font << /F1 4 0 R >> >> /Length " + strconv.Itoa(len(form)) +
		" >>\nstream\n" + form + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")

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

func TestRedactPageRegions_RefusesContentDrawnByAFormXObject(t *testing.T) {
	doc, page := redactFormFixture(t)
	alloc := writer.NewObjAllocator(20)

	// The glyphs belong to the form's own content stream, so no byte range in
	// the page's stream addresses them. Removing the Do that invoked the form
	// would erase everything else the form draws, so the region is refused
	// rather than answered with the only span it could name.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{-10, -10, 300, 300}})
	if !errors.Is(err, ErrRegionInFormXObject) {
		t.Errorf("err = %v, want ErrRegionInFormXObject", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestRedactPageRegions_FormContentOutsideTheRegionDoesNotRefuse(t *testing.T) {
	doc, page := redactFormFixture(t)
	alloc := writer.NewObjAllocator(20)

	// A form on the page must not block redaction by existing. Only a region
	// that actually selects what the form draws is refused; this one is far
	// from it, so the page is simply left alone.
	err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{500, 700, 560, 760}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects, want none: nothing was selected", n)
	}
}
