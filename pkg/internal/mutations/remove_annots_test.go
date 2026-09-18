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
	return annotPageFixtureWithCatalog(t, annotsArray, annotObjects, "")
}

// annotPageFixtureWithCatalog is [annotPageFixture] with extra catalog entries,
// which is how a fixture states an /AcroForm. One builder rather than two, so
// the raw PDF lives in a single place.
func annotPageFixtureWithCatalog(
	t *testing.T,
	annotsArray, annotObjects, catalogExtra string,
) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	raw := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>\nendobj\n" +
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

func TestRemoveRegionAnnotations_KidWidgetLeavesItsParentsKids(t *testing.T) {
	// A widget carrying /Parent is reached by that field's /Kids, so dropping it
	// from the page alone would leave the field naming an annotation no page
	// carries. Object 9 has exactly one kid, which is the common shape: roughly
	// half the fields carrying /Kids have one. The field stays and loses the
	// entry rather than being removed itself — it still states its name and
	// type, and its value is ScrubFormFieldValues' to clear.
	doc, page := annotPageFixture(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Widget /Rect [100 695 200 720] /Parent 9 0 R >>\nendobj\n"+
			"9 0 obj\n<< /FT /Tx /T (name) /V (SECRET) /Kids [8 0 R] >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if !slices.Contains(report.RemovedAnnotations, 8) {
		t.Errorf("RemovedAnnotations = %v, want object 8", report.RemovedAnnotations)
	}

	field, ok := alloc.Pending()[9].(ast.PdfDict)
	if !ok {
		t.Fatalf("the parent field was not rewritten: %T", alloc.Pending()[9])
	}
	if _, present := field["Kids"]; present {
		t.Errorf("/Kids = %v, want the entry dropped once its last kid went", field["Kids"])
	}
	if _, present := field["T"]; !present {
		t.Errorf("the field lost its name, but only its kid was removed")
	}
}

func TestRemoveRegionAnnotations_TwoKidsUnderOneParentBothGo(t *testing.T) {
	// Both widgets name the same field. Pruning them one at a time against the
	// document would make the second write undo the first, which is the same
	// composition hazard the AcroForm read has: the parent is read alloc-first
	// and the widgets are grouped, so one field is rewritten once.
	doc, page := annotPageFixture(t, "[8 0 R 11 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Widget /Rect [100 695 150 720] /Parent 9 0 R >>\nendobj\n"+
			"11 0 obj\n<< /Type /Annot /Subtype /Widget /Rect [160 695 200 720] /Parent 9 0 R >>\nendobj\n"+
			"9 0 obj\n<< /FT /Tx /T (name) /Kids [8 0 R 11 0 R] >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if !slices.Contains(report.RemovedAnnotations, 8) || !slices.Contains(report.RemovedAnnotations, 11) {
		t.Errorf("RemovedAnnotations = %v, want both 8 and 11", report.RemovedAnnotations)
	}

	field, ok := alloc.Pending()[9].(ast.PdfDict)
	if !ok {
		t.Fatalf("the parent field was not rewritten: %T", alloc.Pending()[9])
	}
	if kids, present := field["Kids"]; present {
		t.Errorf("/Kids = %v, want the entry dropped: both kids went", kids)
	}
}

func TestRemoveRegionAnnotations_DirectParentStillRefuses(t *testing.T) {
	// The /Parent is written into the widget rather than named, so there is no
	// object for the detachment to take. Removing the widget anyway would leave
	// that field holding a value with no widget to show it.
	doc, page := annotPageFixture(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Widget /Rect [100 695 200 720]"+
			" /Parent << /FT /Tx /T (name) /V (SECRET) >> >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if !errors.Is(err, ErrAnnotationIsFormWidget) {
		t.Errorf("err = %v, want ErrAnnotationIsFormWidget for a direct /Parent", err)
	}
}

func TestRemoveRegionAnnotations_RootWidgetIsRemovedFromFieldsToo(t *testing.T) {
	// A parentless widget is the merged field-and-annotation of ISO 32000-1,
	// 12.5.6.19, so it is its own root field. Dropping it from /Annots alone
	// would leave AcroForm /Fields naming an annotation no page carries, still
	// holding /V. Both go together.
	doc, page := annotPageFixtureWithCatalog(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (SECRET)"+
			" /Rect [100 695 200 720] >>\nendobj\n",
		"/AcroForm << /Fields [8 0 R] >>")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if !slices.Contains(report.RemovedAnnotations, 8) {
		t.Errorf("RemovedAnnotations = %v, want object 8", report.RemovedAnnotations)
	}

	catalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("the catalog was not rewritten, so /Fields still names the widget: %T",
			alloc.Pending()[1])
	}
	form, _ := catalog["AcroForm"].(ast.PdfDict)
	fields, _ := form["Fields"].(ast.PdfArray)
	if len(fields) != 0 {
		t.Errorf("/Fields = %v, want empty: the root field went with its widget", fields)
	}
}

func TestRemoveRegionAnnotations_IndirectAcroFormIsRewrittenInPlace(t *testing.T) {
	// The test above stated /AcroForm inline, which drives the catalog-rewrite
	// branch. An indirect /AcroForm is the ordinary shape, and it takes the
	// other branch: the form object is replaced where it stands and the catalog
	// is left alone. Asserting both halves is what pins WHICH branch ran, rather
	// than only that the field went.
	doc, page := annotPageFixtureWithCatalog(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (SECRET)"+
			" /Rect [100 695 200 720] >>\nendobj\n"+
			"10 0 obj\n<< /Fields [8 0 R] >>\nendobj\n",
		"/AcroForm 10 0 R")
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	form, ok := alloc.Pending()[10].(ast.PdfDict)
	if !ok {
		t.Fatalf("the AcroForm object was not rewritten: %T", alloc.Pending()[10])
	}
	fields, _ := form["Fields"].(ast.PdfArray)
	if len(fields) != 0 {
		t.Errorf("/Fields = %v, want empty", fields)
	}
	if _, rewritten := alloc.Pending()[1]; rewritten {
		t.Errorf("the catalog was rewritten, but an indirect /AcroForm is replaced where it stands")
	}
}

func TestRemoveRegionAnnotations_RootWidgetWithoutAcroFormStillGoes(t *testing.T) {
	// No form to prune. Removing the widget from the page is the whole of it,
	// and inventing an AcroForm to edit would be worse than doing nothing.
	doc, page := annotPageFixture(t, "[8 0 R]",
		"8 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (name)"+
			" /Rect [100 695 200 720] >>\nendobj\n")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, theTextRegion)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if !slices.Contains(report.RemovedAnnotations, 8) {
		t.Errorf("RemovedAnnotations = %v, want object 8", report.RemovedAnnotations)
	}
}

// twoPageWidgetFixture builds a two-page document whose pages each carry one
// parentless widget, both named by a single shared AcroForm.
func twoPageWidgetFixture(t *testing.T) (*document.Document, []*document.Page) {
	t.Helper()
	ctx := context.Background()

	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	n := strconv.Itoa(len(body))
	raw := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /AcroForm 10 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R 13 0 R] /Count 2 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R" +
		" /Contents 6 0 R /Annots [8 0 R] >>\nendobj\n" +
		"6 0 obj\n<< /Length " + n + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (one) /V (SECRET)" +
		" /Rect [100 695 200 720] >>\nendobj\n" +
		"13 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R" +
		" /Contents 14 0 R /Annots [9 0 R] >>\nendobj\n" +
		"14 0 obj\n<< /Length " + n + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"9 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (two) /V (SECRET)" +
		" /Rect [100 695 200 720] >>\nendobj\n" +
		"10 0 obj\n<< /Fields [8 0 R 9 0 R] >>\nendobj\n" +
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
	if err != nil || len(pages) != 2 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return doc, pages
}

func TestRemoveRegionAnnotations_SecondPageKeepsTheFirstPagesPruning(t *testing.T) {
	// A caller redacting page after page shares ONE allocator. The form must
	// therefore be read back as it now stands, not as the document states it:
	// reading it from the document would prune the original array a second time
	// and write that over the first page's result, putting field 8 back into
	// /Fields with no widget left to carry it.
	//
	// Every other test here uses a fresh allocator and a single page, so this is
	// the only one that can see it. Without the alloc-first read /Fields ends as
	// [8 0 R] rather than empty.
	ctx := context.Background()
	doc, pages := twoPageWidgetFixture(t)
	alloc := writer.NewObjAllocator(30)

	for i, page := range pages {
		if _, err := RedactPageRegions(ctx, alloc, doc, page, theTextRegion); err != nil {
			t.Fatalf("RedactPageRegions on page %d: %v", i+1, err)
		}
	}

	form, ok := alloc.Pending()[10].(ast.PdfDict)
	if !ok {
		t.Fatalf("the AcroForm was not rewritten: %T", alloc.Pending()[10])
	}
	fields, _ := form["Fields"].(ast.PdfArray)
	if len(fields) != 0 {
		t.Errorf("/Fields = %v, want empty: the second page overwrote the first page's pruning", fields)
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
