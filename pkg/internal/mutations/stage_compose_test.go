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

// threePagePDF builds a three-page document whose /Resources and /MediaBox live
// on the /Pages node, so a page rewritten without materialising them loses its
// font (ISO 32000-1, 7.7.3.4).
func threePagePDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3" +
			" /MediaBox [0 0 612 792] /Resources << /Font << /F1 7 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// composeFixture opens the three-page fixture and returns everything the staged
// mutations need.
func composeFixture(t *testing.T) (context.Context, *document.Document, []*document.Page, *writer.ObjAllocator) {
	t.Helper()

	ctx := context.Background()
	data := threePagePDF(t)
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
	if len(pages) != 3 {
		t.Fatalf("fixture has %d pages, want 3", len(pages))
	}
	return ctx, doc, pages, writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
}

// TestDeleteKeepsAnEarlierStagesPageEdits pins that a mutation running after
// another in the same allocator builds on it instead of replacing it.
//
// Both rewrite whole page dictionaries at the page's own object number. Copying
// the source dictionary throws away whatever the earlier stage wrote, so
// `litho pipe f rotate -angle 90 delete -pages 1` produced a file with no
// rotation at all, while running the two commands separately rotated it.
func TestDeleteKeepsAnEarlierStagesPageEdits(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := composeFixture(t)

	if err := RotatePages(ctx, alloc, pages, 90, nil); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	if err := DeletePages(ctx, alloc, doc, pages, []int{1}); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}

	for _, objNum := range []int{4, 5} {
		page, ok := alloc.Pending()[objNum].(ast.PdfDict)
		if !ok {
			t.Fatalf("page %d was not rewritten: %T", objNum, alloc.Pending()[objNum])
		}
		if rotate, _ := page["Rotate"].(ast.PdfInt); rotate != 90 {
			t.Errorf("page %d has /Rotate %v, want 90: the delete discarded the rotation", objNum, page["Rotate"])
		}
	}
}

// TestInsertKeepsAnEarlierStagesPageEdits pins the same for insert, which
// rewrites every target page to re-parent it.
func TestInsertKeepsAnEarlierStagesPageEdits(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := composeFixture(t)

	srcData := threePagePDF(t)
	src, err := document.NewDocument(ctx, bytes.NewReader(srcData), int64(len(srcData)), nil)
	if err != nil {
		t.Fatalf("NewDocument (source): %v", err)
	}
	srcCatalog, err := document.ResolveCatalog(ctx, src)
	if err != nil {
		t.Fatalf("ResolveCatalog (source): %v", err)
	}
	srcPages, err := document.ResolvePages(ctx, src, srcCatalog)
	if err != nil {
		t.Fatalf("ResolvePages (source): %v", err)
	}

	if err := RotatePages(ctx, alloc, pages, 180, nil); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	if err := InsertPages(ctx, alloc, doc, pages, 0, src, srcPages, []int{1}); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}

	for _, objNum := range []int{3, 4, 5} {
		page, ok := alloc.Pending()[objNum].(ast.PdfDict)
		if !ok {
			t.Fatalf("page %d was not rewritten: %T", objNum, alloc.Pending()[objNum])
		}
		if rotate, _ := page["Rotate"].(ast.PdfInt); rotate != 180 {
			t.Errorf("page %d has /Rotate %v, want 180: the insert discarded the rotation", objNum, page["Rotate"])
		}
	}
}

// TestRotateKeepsAnEarlierStagesPageEdits pins the reverse order: rotating
// after a delete must not undo the flat-tree /Parent and the attributes the
// delete materialised, or an inheriting page loses its font.
func TestRotateKeepsAnEarlierStagesPageEdits(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := composeFixture(t)

	if err := DeletePages(ctx, alloc, doc, pages, []int{1}); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	if err := RotatePages(ctx, alloc, pages, 270, nil); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	page, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("the page was not rewritten: %T", alloc.Pending()[4])
	}
	if rotate, _ := page["Rotate"].(ast.PdfInt); rotate != 270 {
		t.Errorf("/Rotate = %v, want 270", page["Rotate"])
	}
	if _, kept := page["Resources"]; !kept {
		t.Error("the page lost the /Resources the delete materialised, so its font is gone")
	}
	if _, kept := page["MediaBox"]; !kept {
		t.Error("the page lost the /MediaBox the delete materialised")
	}
}
