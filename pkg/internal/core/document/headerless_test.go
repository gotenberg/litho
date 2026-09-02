package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

// headerlessPDF builds a complete one-page document and omits the "%PDF-"
// line, the shape of pdfjs bug1606566.pdf. Everything else, including the
// xref table, trailer and startxref, is well formed.
func headerlessPDF(withHeader bool) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}

	var buf bytes.Buffer
	if withHeader {
		buf.WriteString("%PDF-1.7\n")
	}
	// The binary comment a producer writes after the header stays either way,
	// so the headerless file still begins with a comment byte.
	buf.WriteString("%\xe2\xe3\xcf\xd3\n")

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

// TestDocumentWithoutHeaderIsRead pins that a file carrying no "%PDF-" line is
// still read when the rest of it is a PDF.
//
// ISO 32000-1, 7.5.2 requires the header, so the file is invalid and litho
// rejected it outright. Every other reader accepts it: pdfjs bug1606566.pdf has
// a complete xref, trailer and startxref and merely lacks the header line, and
// pdf.js reports one page with a null PDFFormatVersion, poppler warns "May not
// be a PDF file (continuing anyway)", and pikepdf opens it without complaint.
func TestDocumentWithoutHeaderIsRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := headerlessPDF(false)
	if bytes.Contains(data, []byte("%PDF-")) {
		t.Fatal("fixture is wrong: it still carries a header")
	}

	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Version != "" {
		t.Errorf("version = %q, want empty: the file states none", doc.Version)
	}

	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) != 1 {
		t.Errorf("resolved %d pages, want 1", len(pages))
	}
}

// TestDocumentWithHeaderStillReportsItsVersion is the control for the ordinary
// case.
func TestDocumentWithHeaderStillReportsItsVersion(t *testing.T) {
	t.Parallel()

	data := headerlessPDF(true)
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Version != "1.7" {
		t.Errorf("version = %q, want 1.7", doc.Version)
	}
	if doc.HeaderOffset != 0 {
		t.Errorf("header offset = %d, want 0", doc.HeaderOffset)
	}
}

// TestNonPDFInputIsStillRejected pins that dropping the header requirement does
// not turn every file into a PDF. Input with no header and no startxref must
// fail immediately rather than be dragged through a rebuild scan.
func TestNonPDFInputIsStillRejected(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"png", append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 4096)...)},
		{"empty", nil},
		{"plain text", bytes.Repeat([]byte("hello world, not a pdf\n"), 200)},
		{"startxref word only, no offset", []byte("startxref\n")},
	} {
		_, err := NewDocument(context.Background(), bytes.NewReader(tc.data), int64(len(tc.data)), nil)
		if err == nil {
			t.Errorf("%s: opened as a PDF", tc.name)
			continue
		}
		if !errors.Is(err, ErrMissingHeader) {
			t.Errorf("%s: got %v, want %v", tc.name, err, ErrMissingHeader)
		}
	}
}
