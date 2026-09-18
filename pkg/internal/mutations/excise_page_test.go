package mutations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// excisePageFixture builds a one-page document whose /Contents is a single
// stream holding body, and returns the document and that page.
func excisePageFixture(t *testing.T, body string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n" +
		fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(body), body) +
		"trailer\n<< /Size 5 /Root 1 0 R >>\n%%EOF\n")

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

// newPageContents returns the bytes the page's rewritten /Contents stream holds.
func newPageContents(t *testing.T, alloc *writer.ObjAllocator, objNum int) []byte {
	t.Helper()
	ctx := context.Background()

	dict, ok := alloc.Pending()[objNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("page %d was not written back: %T", objNum, alloc.Pending()[objNum])
	}
	ref, ok := dict["Contents"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("/Contents = %T, want a single stream reference", dict["Contents"])
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("/Contents target = %T, want a stream", alloc.Pending()[ref.ObjectNum])
	}
	got, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("stream.Bytes: %v", err)
	}
	if length, ok := stream.Dict["Length"].(ast.PdfInt); !ok || int(length) != len(got) {
		t.Errorf("/Length = %v, want %d to match the body", stream.Dict["Length"], len(got))
	}
	return got
}

func TestExcisePageSpans_ReplacesContentsWithTheSplicedStream(t *testing.T) {
	doc, page := excisePageFixture(t, "BT /F1 12 Tf (secret) Tj ET")
	alloc := writer.NewObjAllocator(20)

	// [12,24) covers " (secret) Tj": the show-text operator with the leading
	// whitespace a parsed range carries.
	err := ExcisePageSpans(context.Background(), alloc, doc, page, []content.ByteRange{{Start: 12, End: 24}})
	if err != nil {
		t.Fatalf("ExcisePageSpans: %v", err)
	}

	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	if string(got) != "BT /F1 12 Tf ET" {
		t.Errorf("page content = %q, want %q", got, "BT /F1 12 Tf ET")
	}
	if bytes.Contains(got, []byte("secret")) {
		t.Error("the excised text is still in the page content")
	}
}

func TestExcisePageSpans_NoSpansLeavesThePageAlone(t *testing.T) {
	doc, page := excisePageFixture(t, "BT /F1 12 Tf (keep) Tj ET")
	alloc := writer.NewObjAllocator(20)

	if err := ExcisePageSpans(context.Background(), alloc, doc, page, nil); err != nil {
		t.Fatalf("ExcisePageSpans: %v", err)
	}

	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects, want none: removing nothing must not rewrite the page", n)
	}
}

func TestExcisePageSpans_RefusesAnUnaddressablePage(t *testing.T) {
	doc, _ := excisePageFixture(t, "BT /F1 12 Tf (secret) Tj ET")
	alloc := writer.NewObjAllocator(20)

	// A page inlined in a /Kids array resolves with object number 0, which has
	// no slot to write back to. Other mutations skip such a page; removal has
	// to refuse, or the content survives in a document the caller believes
	// clean.
	inlined := &document.Page{Ref: ast.PdfIndirectRef{ObjectNum: 0}, Dict: ast.PdfDict{}}

	err := ExcisePageSpans(context.Background(), alloc, doc, inlined, []content.ByteRange{{Start: 0, End: 1}})
	if !errors.Is(err, ErrPageNotAddressable) {
		t.Errorf("err = %v, want ErrPageNotAddressable", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing; a stream allocated for a page that cannot be written back is an orphan", n)
	}
}

func TestExcisePageSpans_RejectsASpanOutsideTheContent(t *testing.T) {
	doc, page := excisePageFixture(t, "BT /F1 12 Tf (secret) Tj ET")
	alloc := writer.NewObjAllocator(20)

	err := ExcisePageSpans(context.Background(), alloc, doc, page, []content.ByteRange{{Start: 4, End: 9999}})
	if !errors.Is(err, ErrSpanOutOfRange) {
		t.Errorf("err = %v, want ErrSpanOutOfRange", err)
	}
	if _, written := alloc.Pending()[page.Ref.ObjectNum]; written {
		t.Error("the page was rewritten despite the refusal")
	}
}

func TestExcisePageSpans_PreservesAQueuedPageEdit(t *testing.T) {
	doc, page := excisePageFixture(t, "BT /F1 12 Tf (secret) Tj ET")
	alloc := writer.NewObjAllocator(20)

	// An earlier stage queued its own edit at this object number; reading the
	// page alloc-first has to carry it, not overwrite it.
	queued := ast.PdfDict{
		"Type":     ast.PdfName("Page"),
		"Marker":   ast.PdfName("kept"),
		"Contents": ast.PdfIndirectRef{ObjectNum: 4},
	}
	if !alloc.AllocAt(page.Ref.ObjectNum, queued) {
		t.Fatalf("AllocAt(%d) refused the queued edit", page.Ref.ObjectNum)
	}

	err := ExcisePageSpans(context.Background(), alloc, doc, page, []content.ByteRange{{Start: 12, End: 24}})
	if err != nil {
		t.Fatalf("ExcisePageSpans: %v", err)
	}

	dict, ok := alloc.Pending()[page.Ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("page = %T, want a dict", alloc.Pending()[page.Ref.ObjectNum])
	}
	if name, ok := dict["Marker"].(ast.PdfName); !ok || name != "kept" {
		t.Errorf("/Marker = %v, want the queued edit preserved", dict["Marker"])
	}
	if got := newPageContents(t, alloc, page.Ref.ObjectNum); string(got) != "BT /F1 12 Tf ET" {
		t.Errorf("page content = %q, want the spliced content", got)
	}
}
