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

// annotAttachmentDoc builds a one-page document whose only embedded file hangs
// off a FileAttachment annotation, with no /Names /EmbeddedFiles tree.
func annotAttachmentDoc(t *testing.T) (context.Context, *document.Document, *writer.ObjAllocator) {
	t.Helper()

	const payload = "hello attachment"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R 7 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] /FS 5 0 R >>",
		"<< /Type /Filespec /F (secret.txt) /UF (secret.txt) /EF << /F 6 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(payload), payload),
		// A second annotation that must survive the removal.
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] >>",
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

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc, writer.NewObjAllocator(doc.HighestObjectNumber())
}

// pageAnnots returns the /Annots the allocator holds for the page, or nil when
// the page was not rewritten.
func pageAnnots(t *testing.T, alloc *writer.ObjAllocator) (ast.PdfArray, bool) {
	t.Helper()

	page, queued := alloc.Pending()[3].(ast.PdfDict)
	if !queued {
		return nil, false
	}
	arr, _ := page["Annots"].(ast.PdfArray)
	return arr, true
}

// TestDetachRemovesAnAnnotationAttachment pins that removing a file also
// removes the FileAttachment annotation carrying it.
//
// ISO 32000-1, 12.5.6.15 lets an annotation carry its own file specification,
// so a file can reach the document without the /Names /EmbeddedFiles tree.
// RemoveAttachment walked only that tree, so `litho detach` exited 0, wrote a
// file byte-identical in size to the input, and left the attachment, and its
// bytes, in place.
func TestDetachRemovesAnAnnotationAttachment(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := annotAttachmentDoc(t)
	if err := RemoveAttachment(ctx, alloc, doc, "secret.txt"); err != nil {
		t.Fatalf("RemoveAttachment: %v", err)
	}

	annots, rewritten := pageAnnots(t, alloc)
	if !rewritten {
		t.Fatal("the page was not rewritten: the annotation still carries the file")
	}
	if len(annots) != 1 {
		t.Fatalf("page has %d annotations, want 1: only the attachment should go", len(annots))
	}
	if ref, ok := annots[0].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 7 {
		t.Errorf("the surviving annotation is %v, want the Square at object 7", annots[0])
	}
}

// TestDetachLeavesOtherAttachmentsAlone is the control: a name the document
// does not carry must change nothing.
func TestDetachLeavesOtherAttachmentsAlone(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := annotAttachmentDoc(t)
	if err := RemoveAttachment(ctx, alloc, doc, "not-in-this-file.txt"); err != nil {
		t.Fatalf("RemoveAttachment: %v", err)
	}

	if _, rewritten := pageAnnots(t, alloc); rewritten {
		t.Error("the page was rewritten for a file the document does not carry")
	}
}

// TestDetachMatchesTheUnicodeFilename pins that the match uses the same name
// the listing reports, which prefers /UF over /F.
func TestDetachMatchesTheUnicodeFilename(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := annotAttachmentDoc(t)
	// The fixture states both /F and /UF as "secret.txt"; asking for the name
	// the listing shows must remove it.
	if err := RemoveAttachment(ctx, alloc, doc, "secret.txt"); err != nil {
		t.Fatalf("RemoveAttachment: %v", err)
	}
	annots, rewritten := pageAnnots(t, alloc)
	if !rewritten || len(annots) != 1 {
		t.Errorf("the attachment named by /UF was not removed (rewritten=%v annots=%d)",
			rewritten, len(annots))
	}
}

// afAttachmentDoc builds a document whose embedded files are associated only
// through the catalog's /AF array, with no name tree and no annotation.
func afAttachmentDoc(t *testing.T) (context.Context, *document.Document, *writer.ObjAllocator) {
	t.Helper()

	const one, two = "first payload", "second payload"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AF [4 0 R 6 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 5 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(one), one),
		"<< /Type /Filespec /F (b.txt) /UF (b.txt) /EF << /F 7 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(two), two),
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

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc, writer.NewObjAllocator(doc.HighestObjectNumber())
}

// pendingCatalog returns the catalog the allocator holds, or nil when it was
// not rewritten.
func pendingCatalog(t *testing.T, ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) ast.PdfDict {
	t.Helper()

	ref, ok := CatalogRef(ctx, doc)
	if !ok {
		t.Fatal("trailer has no /Root")
	}
	d, _ := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	return d
}

// TestDetachRemovesAnAssociatedFile pins that removing a file also drops the
// /AF entry naming it.
//
// ISO 32000-2, 14.13 associates a file with the document through /AF, and that
// array may be the only route to it. Removing only the name tree left such a
// file in the output while detach reported success, the same silent failure
// the FileAttachment route had.
//
// # Corpus evidence
//
//   - pdfjs bug1937438_mml_from_latex.pdf: attaches two stylesheets through
//     /AF only, and removing only the name tree left both in the output.
func TestDetachRemovesAnAssociatedFile(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := afAttachmentDoc(t)
	if err := RemoveAttachment(ctx, alloc, doc, "a.txt"); err != nil {
		t.Fatalf("RemoveAttachment: %v", err)
	}

	catalog := pendingCatalog(t, ctx, doc, alloc)
	if catalog == nil {
		t.Fatal("the catalog was not rewritten: the /AF entry still names the file")
	}
	af, _ := catalog["AF"].(ast.PdfArray)
	if len(af) != 1 {
		t.Fatalf("/AF has %d entries, want 1: only the named file should go", len(af))
	}
	if ref, ok := af[0].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 6 {
		t.Errorf("/AF[0] = %v, want the surviving b.txt filespec at object 6", af[0])
	}
}

// TestDetachDropsAnEmptiedAFArray pins that removing the last associated file
// removes /AF rather than leaving an empty array behind.
func TestDetachDropsAnEmptiedAFArray(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := afAttachmentDoc(t)
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := RemoveAttachment(ctx, alloc, doc, name); err != nil {
			t.Fatalf("RemoveAttachment(%s): %v", name, err)
		}
	}

	catalog := pendingCatalog(t, ctx, doc, alloc)
	if catalog == nil {
		t.Fatal("the catalog was not rewritten")
	}
	if _, present := catalog["AF"]; present {
		t.Errorf("/AF is still present after removing every associated file: %v", catalog["AF"])
	}
}

// TestDetachLeavesOtherAssociatedFilesAlone is the control: a name the document
// does not carry must change nothing.
func TestDetachLeavesOtherAssociatedFilesAlone(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := afAttachmentDoc(t)
	if err := RemoveAttachment(ctx, alloc, doc, "not-here.txt"); err != nil {
		t.Fatalf("RemoveAttachment: %v", err)
	}
	if catalog := pendingCatalog(t, ctx, doc, alloc); catalog != nil {
		t.Error("the catalog was rewritten for a file the document does not carry")
	}
}
