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

// annotFixture builds a two-page document whose pages carry the given /Annots
// arrays, plus any extra objects the test needs. Two pages on purpose: the
// scrub is document-wide, and an annotation on page two quoting text redacted
// on page one is the case a page-scoped pass would miss.
func annotFixture(t *testing.T, annots1, annots2, extra string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>\nendobj\n" +
		"5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"6 0 obj\n<< /Font << /F1 5 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 6 0 R " +
		"/Contents 7 0 R /Annots " + annots1 + " >>\nendobj\n" +
		"4 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 6 0 R " +
		"/Contents 8 0 R /Annots " + annots2 + " >>\nendobj\n" +
		"7 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Length 0 >>\nstream\n\nendstream\nendobj\n" +
		extra +
		"trailer\n<< /Size 40 /Root 1 0 R >>\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) < 2 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return doc, pages[0]
}

// annotObj is a plain indirect annotation carrying text.
func annotObj(num int, key, text string) string {
	return strconv.Itoa(num) + " 0 obj\n<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /" +
		key + " (" + text + ") >>\nendobj\n"
}

// apAnnotObj is an annotation with a normal appearance stream, whose content is
// given so a test can decide whether it paints glyphs.
func apAnnotObj(num, apNum int, text, apContent string) string {
	return strconv.Itoa(num) + " 0 obj\n<< /Type /Annot /Subtype /FreeText /Rect [0 0 10 10] " +
		"/Contents (" + text + ") /AP << /N " + strconv.Itoa(apNum) + " 0 R >> >>\nendobj\n" +
		strconv.Itoa(apNum) + " 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length " +
		strconv.Itoa(len(apContent)) + " >>\nstream\n" + apContent + "\nendstream\nendobj\n"
}

func TestScrubAnnotationText_ClearsAQuotingAnnotation(t *testing.T) {
	// Positive control: without a fixture carrying annotations the whole pass
	// no-ops and every assertion below would hold vacuously.
	doc, _ := annotFixture(t, "[10 0 R]", "[]", annotObj(10, "Contents", "the SECRET note"))
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubAnnotationText(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubAnnotationText: %v", err)
	}
	if !slices.Contains(scrubbed, 10) {
		t.Fatalf("scrubbed = %v, want object 10", scrubbed)
	}

	newAnnot, ok := alloc.Pending()[10].(ast.PdfDict)
	if !ok {
		t.Fatalf("object 10 = %T, want the rewritten annotation", alloc.Pending()[10])
	}
	if _, present := newAnnot["Contents"]; present {
		t.Error("/Contents survived: the annotation still quotes the removed text")
	}
	// The annotation itself stays: dropping it would remove a mark the caller
	// never asked to delete.
	if newAnnot["Subtype"] != ast.PdfName("Text") {
		t.Errorf("annotation lost its /Subtype: %v", newAnnot)
	}
	if _, present := newAnnot["Rect"]; !present {
		t.Error("/Rect was lost")
	}
}

func TestScrubAnnotationText_ReachesAnotherPage(t *testing.T) {
	// The reason this pass is document-wide. The text was redacted on page one
	// and the annotation quoting it sits on page two.
	doc, _ := annotFixture(t, "[]", "[11 0 R]", annotObj(11, "Contents", "quoting SECRET here"))
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubAnnotationText(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubAnnotationText: %v", err)
	}
	if !slices.Contains(scrubbed, 11) {
		t.Errorf("scrubbed = %v, want the page-two annotation 11", scrubbed)
	}
}

func TestScrubAnnotationText_ClearsAQuotingURI(t *testing.T) {
	// A link target routinely spells the heading it points at.
	doc, _ := annotFixture(t, "[12 0 R]", "[]", annotObj(12, "URI", "http://x/SECRET-report"))
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubAnnotationText(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubAnnotationText: %v", err)
	}
	if !slices.Contains(scrubbed, 12) {
		t.Errorf("scrubbed = %v, want object 12", scrubbed)
	}
}

func TestScrubAnnotationText_LeavesUnrelatedAnnotationsAlone(t *testing.T) {
	doc, _ := annotFixture(t, "[13 0 R]", "[]", annotObj(13, "Contents", "an ordinary note"))
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubAnnotationText(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubAnnotationText: %v", err)
	}
	if len(scrubbed) != 0 {
		t.Errorf("scrubbed = %v, want none", scrubbed)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects, want none", n)
	}
}

func TestScrubAnnotationText_RefusesWhenTheAppearancePaintsText(t *testing.T) {
	// The words are drawn by the /AP's own content stream, so clearing
	// /Contents would leave them on screen.
	doc, _ := annotFixture(t, "[14 0 R]", "[]",
		apAnnotObj(14, 15, "the SECRET note", "BT /F1 12 Tf (SECRET) Tj ET"))
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubAnnotationText(context.Background(), alloc, doc, []string{"SECRET"})
	if !errors.Is(err, ErrAnnotationAppearanceHoldsText) {
		t.Errorf("err = %v, want ErrAnnotationAppearanceHoldsText", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestScrubAnnotationText_AppearanceWithoutTextStillScrubs(t *testing.T) {
	// The refusal must not be blanket: an appearance that paints no glyphs
	// cannot be showing the text, so the annotation is cleanable.
	doc, _ := annotFixture(t, "[16 0 R]", "[]",
		apAnnotObj(16, 17, "the SECRET note", "0 0 10 10 re f"))
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubAnnotationText(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubAnnotationText: %v", err)
	}
	if !slices.Contains(scrubbed, 16) {
		t.Errorf("scrubbed = %v, want object 16: its appearance draws no text", scrubbed)
	}
}

func TestScrubAnnotationText_RefusesADirectAnnotation(t *testing.T) {
	// Written straight into /Annots, so there is no object number to replace.
	doc, _ := annotFixture(t,
		"[<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /Contents (the SECRET note) >>]", "[]", "")
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubAnnotationText(context.Background(), alloc, doc, []string{"SECRET"})
	if !errors.Is(err, ErrAnnotationNotAddressable) {
		t.Errorf("err = %v, want ErrAnnotationNotAddressable", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestScrubAnnotationText_RejectsAnEmptyToken(t *testing.T) {
	doc, _ := annotFixture(t, "[18 0 R]", "[]", annotObj(18, "Contents", "note"))
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubAnnotationText(context.Background(), alloc, doc, []string{"SECRET", ""})
	if !errors.Is(err, ErrScanTokenEmpty) {
		t.Errorf("err = %v, want ErrScanTokenEmpty", err)
	}
}

func TestScrubAnnotationText_RefusalAfterScrubbingRegistersNothing(t *testing.T) {
	// Object 20 is scrubbable and comes FIRST in /Annots; object 21 refuses
	// because its appearance paints the text. The order is the test: without a
	// rollback, 20's rewrite stays queued and a caller that handled the error
	// would write a document scrubbed halfway. Every other refusal fixture here
	// holds a single annotation, so none of them reaches this.
	doc, _ := annotFixture(t, "[20 0 R 21 0 R]", "[]",
		annotObj(20, "Contents", "the SECRET note")+
			apAnnotObj(21, 22, "another SECRET", "BT /F1 12 Tf (SECRET) Tj ET"))
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubAnnotationText(context.Background(), alloc, doc, []string{"SECRET"})
	if !errors.Is(err, ErrAnnotationAppearanceHoldsText) {
		t.Fatalf("err = %v, want ErrAnnotationAppearanceHoldsText", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing: the earlier scrub was not rolled back", n)
	}
}
