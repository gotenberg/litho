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
