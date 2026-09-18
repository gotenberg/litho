package mutations

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// annotPageFixture builds a one-page document drawing "SECRET" at (100,700)
// whose page carries the given /Annots array and annotation objects.
func annotPageFixture(t *testing.T, annotsArray, annotObjects string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	raw := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R" +
		" /Contents 6 0 R /Annots " + annotsArray + " >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		annotObjects +
		"trailer\n<< /Size 20 /Root 1 0 R >>\n%%EOF\n"

	doc, err := document.NewDocument(ctx, bytes.NewReader([]byte(raw)), int64(len(raw)), nil)
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

// theTextRegion covers the "SECRET" run the fixture draws at (100,700).
var theTextRegion = [][4]float64{{90, 690, 300, 730}}

func TestRemoveRegionAnnotations_DropsALinkInsideTheRegion(t *testing.T) {
	// The leak: clearing the content stream leaves the link whole, and its
	// /A /URI still quotes the redacted word while its /Rect marks where it was.
	doc, page := annotPageFixture(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Link /Rect [100 695 200 720]"+
			" /A << /S /URI /URI (http://example.test/SECRET) >> >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if !slices.Contains(report.RemovedAnnotations, 8) {
		t.Errorf("RemovedAnnotations = %v, want object 8", report.RemovedAnnotations)
	}

	pageDict, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Fatalf("the page was not rewritten: %T", alloc.Pending()[3])
	}
	arr, _ := pageDict["Annots"].(ast.PdfArray)
	if len(arr) != 0 {
		t.Errorf("/Annots = %v, want empty", arr)
	}
}

func TestRemoveRegionAnnotations_KeepsOneClearOfTheRegion(t *testing.T) {
	doc, page := annotPageFixture(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Link /Rect [10 10 50 50] >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedAnnotations) != 0 {
		t.Errorf("RemovedAnnotations = %v, want none: it sits in an empty corner",
			report.RemovedAnnotations)
	}
}

func TestRemoveRegionAnnotations_ReversedRectIsStillFound(t *testing.T) {
	// The discriminating case. Per ISO 32000-1, 7.9.5 a rectangle may state
	// either diagonal, so this names the same area as the test above with its
	// corners the other way round. Comparing raw corners would leave it in
	// place, and an annotation missed is a leak that looks like success.
	doc, page := annotPageFixture(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Link /Rect [200 720 100 695] >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if !slices.Contains(report.RemovedAnnotations, 8) {
		t.Errorf("RemovedAnnotations = %v, want object 8: the corners were reversed",
			report.RemovedAnnotations)
	}
}

func TestRemoveRegionAnnotations_StraddlingRefuses(t *testing.T) {
	// Reaches well past the region. An annotation is atomic, so removing it
	// would take behaviour outside what the caller selected.
	doc, page := annotPageFixture(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Link /Rect [100 695 500 720] >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if !errors.Is(err, ErrAnnotationStraddlesRegion) {
		t.Errorf("err = %v, want ErrAnnotationStraddlesRegion", err)
	}
}

func TestRemoveRegionAnnotations_WidgetRefuses(t *testing.T) {
	// Its field lives in the catalog's AcroForm tree. Dropping the widget alone
	// would leave the field naming an annotation no page carries.
	doc, page := annotPageFixture(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Widget /Rect [100 695 200 720] /V (SECRET) >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if !errors.Is(err, ErrAnnotationIsFormWidget) {
		t.Errorf("err = %v, want ErrAnnotationIsFormWidget", err)
	}
}

func TestRemoveRegionAnnotations_MissingRectRefuses(t *testing.T) {
	// /Rect is required (ISO 32000-1, Table 164). Without it the annotation
	// cannot be placed, so it cannot be ruled outside the region either.
	doc, page := annotPageFixture(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Link >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if !errors.Is(err, ErrAnnotationNotPlaceable) {
		t.Errorf("err = %v, want ErrAnnotationNotPlaceable", err)
	}
}

func TestRemoveRegionAnnotations_RefusalRollsBack(t *testing.T) {
	// A refusal must leave the allocator as it found it. The first annotation is
	// removable and the second straddles, so the pass has already decided to
	// drop one by the time it refuses over the other.
	//
	// This pins the outcome, not the mechanism, and the distinction is worth
	// stating: removeRegionAnnotations gathers its decisions and writes only
	// once the whole array has passed, so nothing is ever queued for a rollback
	// to undo. That is a stronger arrangement than leaning on Restore, and this
	// test is what fails if the pass is ever rewritten to queue as it goes.
	doc, page := annotPageFixture(t, "[8 0 R 9 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Link /Rect [100 695 200 720] >>\nendobj\n"+
			"9 0 obj\n<< /Type /Annot /Subtype /Link /Rect [100 695 500 720] >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion); err == nil {
		t.Fatalf("the straddling annotation was accepted")
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("allocator holds %d objects after a refusal, want none", len(alloc.Pending()))
	}
}
