package semantics

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// outlinePDF assembles a one-page document whose catalog carries the given
// /Outlines entry verbatim. Object 2 is deliberately never defined, so a
// reference to it exercises the undefined-object path.
func outlinePDF(outlinesEntry string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 3 0 R " + outlinesEntry + " >>",
		"<< /Type /Placeholder >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] >>",
	}

	var buf []byte
	buf = append(buf, "%PDF-1.7\n"...)
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = len(buf)
		buf = append(buf, fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, body)...)
	}
	start := len(buf)
	buf = append(buf, fmt.Sprintf("xref\n0 %d\n", len(objs)+1)...)
	buf = append(buf, "0000000000 65535 f \n"...)
	for i := 1; i <= len(objs); i++ {
		// Object 2 is marked free: a reference to it is a reference to null.
		if i == 2 {
			buf = append(buf, "0000000000 65535 f \n"...)
			continue
		}
		buf = append(buf, fmt.Sprintf("%010d 00000 n \n", offsets[i])...)
	}
	buf = append(buf, fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, start)...)
	return buf
}

func outlineDoc(t *testing.T, entry string) *document.Document {
	t.Helper()
	data := outlinePDF(entry)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestOutlinePointingAtUndefinedObjectIsNoOutline pins that /Outlines resolving
// to null means the document has no outline.
//
// Per ISO 32000-1, 7.3.9 a reference to an undefined object is a reference to
// null, and per 7.3.7 an entry whose value is null is equivalent to the entry
// being absent. pdfjs colors.pdf points /Outlines at a free object; treating
// that as a broken outline made `litho bookmarks` fail on a file both poppler
// and pikepdf read as simply having none.
func TestOutlinePointingAtUndefinedObjectIsNoOutline(t *testing.T) {
	t.Parallel()

	marks, err := ResolveOutline(context.Background(), outlineDoc(t, "/Outlines 2 0 R"))
	if err != nil {
		t.Fatalf("an /Outlines pointing at a free object must not error, got: %v", err)
	}
	if len(marks) != 0 {
		t.Errorf("bookmarks = %v, want none", marks)
	}
}

// TestOutlineAbsentIsNoOutline keeps the neighbouring case covered.
func TestOutlineAbsentIsNoOutline(t *testing.T) {
	t.Parallel()

	marks, err := ResolveOutline(context.Background(), outlineDoc(t, ""))
	if err != nil {
		t.Fatalf("a catalog without /Outlines must not error, got: %v", err)
	}
	if len(marks) != 0 {
		t.Errorf("bookmarks = %v, want none", marks)
	}
}

// TestOutlineWithWrongTypeStillErrors checks the leniency is bounded: an
// /Outlines holding a name is malformed and must still surface.
func TestOutlineWithWrongTypeStillErrors(t *testing.T) {
	t.Parallel()

	_, err := ResolveOutline(context.Background(), outlineDoc(t, "/Outlines /NotADict"))
	if err == nil {
		t.Fatal("a non-dictionary /Outlines must report an error")
	}
	if !strings.Contains(err.Error(), "outlines") {
		t.Errorf("error = %v, want it to name the outlines entry", err)
	}
}
