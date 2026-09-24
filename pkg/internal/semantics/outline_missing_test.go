package semantics

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// loadDoc opens raw PDF bytes as a document, failing the test if it cannot.
func loadDoc(t *testing.T, data []byte) *document.Document {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// buildDanglingOutlinePDF assembles a document whose /Outlines points at an
// object the xref table declares but the file never writes. The entry's offset
// lands on the `xref` keyword, so parsing it fails rather than returning null.
//
// # Corpus evidence
//
//   - pdfjs issue2618: has exactly this shape.
func buildDanglingOutlinePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+2)
	for _, o := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", o)
	}
	// Object 4 is in use per the table, but its body was never written and the
	// offset points at the table itself.
	fmt.Fprintf(&buf, "%010d 00000 n \r\n", xref)
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+2, xref)
	return buf.Bytes()
}

// TestResolveOutlineDanglingReference pins that an /Outlines reference litho
// cannot read reports no outline rather than an error.
//
// Per ISO 32000-1, 7.3.9 a reference to an undefined object is a reference to
// null, and 7.3.7 makes a null entry equivalent to an absent one. An object
// whose declared offset holds no object is indistinguishable from an undefined
// one. Returning an error here made Bookmarks the only reader call that failed
// on such a file, and `litho bookmarks` exited non-zero.
//
// # Corpus evidence
//
//   - pdfjs issue2618: info, pages, metadata, annotations, images and forms all
//     read it, poppler and pikepdf both report it as having no outline, and
//     Bookmarks alone failed.
func TestResolveOutlineDanglingReference(t *testing.T) {
	t.Parallel()

	doc := loadDoc(t, buildDanglingOutlinePDF())
	bm, err := ResolveOutline(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveOutline on a dangling /Outlines: %v", err)
	}
	if len(bm) != 0 {
		t.Errorf("got %d bookmarks from an unreadable outline, want 0", len(bm))
	}
}

// TestResolveOutlineWrongType pins the other half: an /Outlines entry that
// resolves to something that is not a dictionary is a type error the caller
// should hear about, not silence. Only unreadable references are treated as
// absent.
func TestResolveOutlineWrongType(t *testing.T) {
	t.Parallel()

	doc := loadDoc(t, buildCyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"[(not) (an) (outline)]",
	))
	if _, err := ResolveOutline(context.Background(), doc); err == nil {
		t.Error("a non-dictionary /Outlines resolved without error")
	}
}
