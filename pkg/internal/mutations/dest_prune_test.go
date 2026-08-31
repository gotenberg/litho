package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// destinationsPDF builds a two-page document whose destinations reach both
// pages: a /Dests dictionary, a /Names/Dests name tree, and a link annotation
// on page 2 carrying an explicit destination array.
func destinationsPDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Dests << /ToOne [3 0 R /Fit] /ToTwo [4 0 R /Fit] >>" +
			" /Names << /Dests 8 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> /Annots [7 0 R 9 0 R 10 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Annot /Subtype /Link /Rect [10 10 50 50] /Dest [3 0 R /Fit] >>",
		"<< /Names [(NameToOne) [3 0 R /Fit] (NameToTwo) [4 0 R /Fit]] >>",
		"<< /Type /Annot /Subtype /Link /Rect [60 10 90 50] /A << /S /GoTo /D [4 0 R /Fit] >> >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (Jump) /Rect [100 10 140 50]" +
			" /A << /S /GoTo /D [3 0 R /Fit] >> >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// deleteFirstPage runs DeletePages over the fixture and returns the allocator
// holding the rewritten objects.
func deleteFirstPage(t *testing.T) (*writer.ObjAllocator, *document.Document) {
	t.Helper()

	ctx := context.Background()
	data := destinationsPDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("fixture has %d pages, want 2", len(pages))
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if err := DeletePages(ctx, alloc, doc, pages, []int{1}); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	return alloc, doc
}

// TestDeletePagesPrunesDestinationsToRemovedPages pins that no destination
// survives pointing at a page the delete dropped.
//
// A destination that still names a dropped page is broken twice over: ISO
// 32000-1, 12.3.2.2 requires the first element of an explicit destination to be
// a page in the document, and the reference itself keeps the page reachable, so
// the writer carries the removed page and its content stream into the output.
// Deleting page 1 of govdocs thread_100_100852.pdf left the page behind with
// all 1191 bytes of its content.
func TestDeletePagesPrunesDestinationsToRemovedPages(t *testing.T) {
	t.Parallel()

	alloc, _ := deleteFirstPage(t)
	pending := alloc.Pending()

	catalog, ok := pending[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("the catalog was not rewritten: %T", pending[1])
	}

	dests, ok := catalog["Dests"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Dests is %T, want a dict", catalog["Dests"])
	}
	if _, kept := dests["ToOne"]; kept {
		t.Error("/Dests kept ToOne, which names the deleted page")
	}
	if _, kept := dests["ToTwo"]; !kept {
		t.Error("/Dests lost ToTwo, which names a page that survived")
	}

	names, ok := pending[8].(ast.PdfDict)
	if !ok {
		t.Fatalf("the /Names/Dests node was not rewritten: %T", pending[8])
	}
	pairs, ok := names["Names"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Names is %T, want an array", names["Names"])
	}
	if len(pairs) != 2 {
		t.Fatalf("/Names holds %d entries, want 2: only the entry naming the deleted page should go", len(pairs)/2)
	}
	if key, _ := pairs[0].(ast.PdfString); string(key) != "NameToTwo" {
		t.Errorf("the surviving name-tree key is %q, want NameToTwo", key)
	}
}

// TestDeletePagesKeepsLinksToSurvivingPages pins the control: only the link
// whose destination is gone is dropped.
func TestDeletePagesKeepsLinksToSurvivingPages(t *testing.T) {
	t.Parallel()

	alloc, _ := deleteFirstPage(t)

	page, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("the surviving page was not rewritten: %T", alloc.Pending()[4])
	}
	annots, ok := page["Annots"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Annots is %T, want an array", page["Annots"])
	}
	if len(annots) != 2 {
		t.Fatalf("/Annots holds %d entries, want 2: only the link to the deleted page should go", len(annots))
	}
	for i, want := range []int{9, 10} {
		ref, isRef := annots[i].(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum != want {
			t.Errorf("/Annots[%d] is %v, want object %d", i, annots[i], want)
		}
	}
}

// TestDeletePagesKeepsWidgetsAndClearsTheirDestination pins that a form control
// survives with only its stale destination removed.
//
// A widget is the field's presence on a page it still paints (ISO 32000-1,
// 12.5.6.19). Dropping it because its /A named a deleted page would take a form
// field off a page the caller asked to keep, which is a worse outcome than the
// dangling action it repairs.
func TestDeletePagesKeepsWidgetsAndClearsTheirDestination(t *testing.T) {
	t.Parallel()

	alloc, _ := deleteFirstPage(t)

	widget, ok := alloc.Pending()[10].(ast.PdfDict)
	if !ok {
		t.Fatalf("the widget was not rewritten: %T", alloc.Pending()[10])
	}
	if _, kept := widget["A"]; kept {
		t.Error("the widget kept the /GoTo naming the deleted page")
	}
	if subtype, _ := widget["Subtype"].(ast.PdfName); subtype != "Widget" {
		t.Errorf("/Subtype = %v, want Widget: the annotation itself must survive", widget["Subtype"])
	}
	if _, kept := widget["T"]; !kept {
		t.Error("the widget lost /T, so the form field it carries is gone")
	}
}

// TestDeletePagesLeavesCleanDocumentsAlone pins that a document with no
// destination into a removed page is not rewritten for nothing.
//
// Pruning rewrites the catalog and the name tree, so a fixer that fires
// unconditionally would churn every file that deletes a page.
func TestDeletePagesLeavesCleanDocumentsAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := destinationsPDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}

	// Deleting page 2 leaves every destination in the fixture valid except the
	// ones naming page 2, so the name tree still changes; deleting nothing must
	// change nothing at all.
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if err := DeletePages(ctx, alloc, doc, pages, nil); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("deleting no pages queued %d rewrites, want none", n)
	}
}

// TestRemovedPageSetKeepsRepeatedPages pins that a page kept at one index and
// dropped at another is not treated as removed.
//
// A split may repeat a page, and the object still belongs to the output, so
// pruning its destinations would break links that are perfectly good.
func TestRemovedPageSetKeepsRepeatedPages(t *testing.T) {
	t.Parallel()

	pages := []*document.Page{
		{Ref: ast.PdfIndirectRef{ObjectNum: 3}},
		{Ref: ast.PdfIndirectRef{ObjectNum: 4}},
		{Ref: ast.PdfIndirectRef{ObjectNum: 3}},
	}
	removed := removedPageSet(pages, []int{0})
	if removed[3] {
		t.Error("object 3 counted as removed, but index 0 keeps it")
	}
	if !removed[4] {
		t.Error("object 4 is dropped and should count as removed")
	}
}

// TestDeletePagesDropsWidgetsOfRemovedPages pins that a deleted page's widget
// leaves the interactive form with it.
//
// A widget names its page in /P (ISO 32000-1, 12.5.6.19) and its field lists it
// in /Kids, so a widget left behind is a live reference that carries the deleted
// page into the output. Deleting page 1 of govdocs thread_100_100852.pdf kept
// the page and all 1191 bytes of its content this way, reachable as
// /Pages/Kids[0]/Annots[0]/Parent/Kids[4]/P.
func TestDeletePagesDropsWidgetsOfRemovedPages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := widgetAcrossPagesPDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if err := DeletePages(ctx, alloc, doc, pages, []int{1}); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}

	field, ok := alloc.Pending()[7].(ast.PdfDict)
	if !ok {
		t.Fatalf("the field was not rewritten: %T", alloc.Pending()[7])
	}
	kids, ok := field["Kids"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Kids is %T, want an array", field["Kids"])
	}
	if len(kids) != 1 {
		t.Fatalf("/Kids holds %d widgets, want 1: the widget on the deleted page keeps that page alive", len(kids))
	}
	if ref, isRef := kids[0].(ast.PdfIndirectRef); !isRef || ref.ObjectNum != 9 {
		t.Errorf("the surviving widget is %v, want object 9, the one on the page that stayed", kids[0])
	}
	if _, kept := field["T"]; !kept {
		t.Error("the field itself was dropped; only its dead widget should go")
	}
}

// widgetAcrossPagesPDF builds a two-page document with one text field whose
// widgets sit on both pages.
func widgetAcrossPagesPDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [7 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> /Annots [8 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> /Annots [9 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /FT /Tx /T (shared) /Kids [8 0 R 9 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 7 0 R /P 3 0 R /Rect [10 10 50 50] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 7 0 R /P 4 0 R /Rect [10 10 50 50] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}
