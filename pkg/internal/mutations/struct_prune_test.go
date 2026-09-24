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

// taggedTwoPagePDF builds a tagged two-page document: one structure element per
// page under a shared /Document element, a /ParentTree naming both, and an
// outline whose two items point one at each page.
func taggedTwoPagePDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /Outlines 12 0 R" +
			" /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> /StructParents 0 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> /StructParents 1 >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /StructTreeRoot /K [8 0 R] /ParentTree 11 0 R >>",
		"<< /Type /StructElem /S /Document /P 7 0 R /K [9 0 R 10 0 R] >>",
		// The element on page 1, the page the delete removes.
		"<< /Type /StructElem /S /P /P 8 0 R /Pg 3 0 R /K 0 >>",
		"<< /Type /StructElem /S /P /P 8 0 R /Pg 4 0 R /K 0 >>",
		"<< /Nums [0 [9 0 R] 1 [10 0 R]] >>",
		"<< /Type /Outlines /First 13 0 R /Last 14 0 R /Count 2 >>",
		"<< /Title (to page one) /Parent 12 0 R /Next 14 0 R /Dest [3 0 R /Fit] >>",
		"<< /Title (to page two) /Parent 12 0 R /Prev 13 0 R /A << /S /GoTo /D [4 0 R /Fit] >> >>",
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

// deleteFirstOfTagged runs DeletePages over the tagged fixture.
func deleteFirstOfTagged(t *testing.T) *writer.ObjAllocator {
	t.Helper()

	ctx := context.Background()
	data := taggedTwoPagePDF(t)
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
	return alloc
}

// TestDeletePagesPrunesStructureElements pins that the structure tree lets go of
// the page the delete removed.
//
// A structure element names its page in /Pg (ISO 32000-1, 14.7.2, Table 323).
// Left behind it describes a page the document no longer has, and since the
// rewrite writes whatever the catalog reaches, it keeps that page and its
// content in the output.
//
// # Corpus evidence
//
//   - govdocs thread_400_400912.pdf: held 17204 bytes of the page it had just
//     deleted, by the structure tree alone.
func TestDeletePagesPrunesStructureElements(t *testing.T) {
	t.Parallel()

	alloc := deleteFirstOfTagged(t)

	parent, ok := alloc.Pending()[8].(ast.PdfDict)
	if !ok {
		t.Fatalf("the /Document element was not rewritten: %T", alloc.Pending()[8])
	}
	kids, ok := parent["K"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/K is %T, want an array", parent["K"])
	}
	if len(kids) != 1 {
		t.Fatalf("/K holds %d elements, want 1: the element on the deleted page should go", len(kids))
	}
	if ref, isRef := kids[0].(ast.PdfIndirectRef); !isRef || ref.ObjectNum != 10 {
		t.Errorf("the surviving element is %v, want object 10, the one on the page that stayed", kids[0])
	}
}

// TestDeletePagesPrunesParentTree pins that the number tree lets go too.
//
// /ParentTree maps each /StructParents key back to the elements marked on that
// page (ISO 32000-1, 14.7.4.4), so an entry for a removed page reaches its
// elements and, through their /Pg, the page. Pruning /K alone leaves the page
// exactly where it was.
func TestDeletePagesPrunesParentTree(t *testing.T) {
	t.Parallel()

	alloc := deleteFirstOfTagged(t)

	tree, ok := alloc.Pending()[11].(ast.PdfDict)
	if !ok {
		t.Fatalf("the /ParentTree was not rewritten: %T", alloc.Pending()[11])
	}
	nums, ok := tree["Nums"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Nums is %T, want an array", tree["Nums"])
	}
	if len(nums) != 2 {
		t.Fatalf("/Nums holds %d entries, want 1 key/value pair", len(nums)/2)
	}
	if key, _ := nums[0].(ast.PdfInt); int(key) != 1 {
		t.Errorf("the surviving /Nums key is %v, want 1, the kept page's /StructParents", nums[0])
	}
}

// TestDeletePagesStripsOutlineDestinations pins that a bookmark stops pointing
// at a page that is gone.
//
// The item itself stays: /Dest and /A are both optional (ISO 32000-1, Table
// 153), so a bookmark keeping its title and its place in the tree is valid,
// while unlinking it would mean repairing /First, /Last, /Next, /Prev and /Count
// around the hole.
//
// # Corpus evidence
//
//   - govdocs thread_000_000761.pdf: kept 2196 bytes of its deleted page
//     through an outline destination alone.
func TestDeletePagesStripsOutlineDestinations(t *testing.T) {
	t.Parallel()

	alloc := deleteFirstOfTagged(t)

	stale, ok := alloc.Pending()[13].(ast.PdfDict)
	if !ok {
		t.Fatalf("the outline item naming the deleted page was not rewritten: %T", alloc.Pending()[13])
	}
	if _, kept := stale["Dest"]; kept {
		t.Error("the outline item kept a /Dest naming the deleted page")
	}
	if _, kept := stale["Title"]; !kept {
		t.Error("the outline item lost its /Title; only the destination should go")
	}
	if _, kept := stale["Next"]; !kept {
		t.Error("the outline item lost /Next, breaking the sibling chain")
	}

	// The control: the item naming a page that survived is untouched, so it is
	// never queued for rewrite at all.
	if _, rewritten := alloc.Pending()[14]; rewritten {
		t.Error("the outline item naming a surviving page was rewritten")
	}
}
