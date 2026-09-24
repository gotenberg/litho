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

// articleThreadPDF builds a four-page document carrying one article whose bead
// ring visits the first three pages in order (ISO 32000-1, 12.4.3).
func articleThreadPDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	page := func(bead int) string {
		body := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R" +
			" /Resources << /Font << /F1 8 0 R >> >>"
		if bead > 0 {
			body += fmt.Sprintf(" /B [%d 0 R]", bead)
		}
		return body + " >>"
	}

	// Page 4 carries no bead, so deleting the three that do empties the article
	// without deleting every page in the document.
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Threads [9 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R 6 0 R] /Count 4 >>",
		page(10),
		page(11),
		page(12),
		page(0),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Thread /F 10 0 R /I << /Title (an article) >> >>",
		"<< /T 9 0 R /P 3 0 R /N 11 0 R /V 12 0 R /R [0 0 100 100] >>",
		"<< /T 9 0 R /P 4 0 R /N 12 0 R /V 10 0 R /R [0 0 100 100] >>",
		"<< /T 9 0 R /P 5 0 R /N 10 0 R /V 11 0 R /R [0 0 100 100] >>",
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

// TestDeletePagesSplicesArticleBeads pins that the bead on a deleted page leaves
// the ring and the rest of the article stays connected.
//
// A bead names its page in /P and chains through /N and /V (ISO 32000-1,
// 12.4.3), so one left in the ring keeps its page in the output. Unlike split,
// delete keeps the catalog and its /Threads, so the article is meant to go on
// working and the ring is spliced rather than dropped.
//
// # Corpus evidence
//
//   - govdocs thread_100_100184.pdf: held 3649 bytes of its deleted page
//     through a bead left in the ring.
func TestDeletePagesSplicesArticleBeads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := articleThreadPDF(t)
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
	if len(pages) != 4 {
		t.Fatalf("fixture has %d pages, want 4", len(pages))
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if err := DeletePages(ctx, alloc, doc, pages, []int{1}); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}

	// Objects 11 and 12 are the beads on the pages that stayed; 10 was on page 1.
	for _, num := range []int{11, 12} {
		bead, ok := alloc.Pending()[num].(ast.PdfDict)
		if !ok {
			t.Fatalf("bead %d was not respliced: %T", num, alloc.Pending()[num])
		}
		next, _ := bead["N"].(ast.PdfIndirectRef)
		prev, _ := bead["V"].(ast.PdfIndirectRef)
		if next.ObjectNum == 10 || prev.ObjectNum == 10 {
			t.Errorf("bead %d still chains to bead 10, which sits on the deleted page", num)
		}
		if next.ObjectNum == 0 || prev.ObjectNum == 0 {
			t.Errorf("bead %d lost a neighbour entirely; the ring must stay closed", num)
		}
	}

	thread, ok := alloc.Pending()[9].(ast.PdfDict)
	if !ok {
		t.Fatalf("the thread was not rewritten: %T", alloc.Pending()[9])
	}
	first, _ := thread["F"].(ast.PdfIndirectRef)
	if first.ObjectNum == 10 {
		t.Error("the thread still names bead 10 as its first, so the article starts on a page that is gone")
	}
	if _, kept := thread["I"]; !kept {
		t.Error("the thread lost its /I information dictionary")
	}
}

// TestDeletePagesDropsAnEmptiedThread pins that an article whose every bead is
// gone leaves /Threads, since an article with no beads has nothing to show.
//
// Page 4 has no bead, so the three bead-bearing pages can go without deleting
// the whole document.
func TestDeletePagesDropsAnEmptiedThread(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := articleThreadPDF(t)
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
	if err := DeletePages(ctx, alloc, doc, pages, []int{1, 2, 3}); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}

	newCatalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("the catalog was not rewritten: %T", alloc.Pending()[1])
	}
	threads, ok := newCatalog["Threads"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Threads is %T, want an array", newCatalog["Threads"])
	}
	if len(threads) != 0 {
		t.Errorf("/Threads holds %d entries, want 0: every bead of the only article is gone", len(threads))
	}
}
