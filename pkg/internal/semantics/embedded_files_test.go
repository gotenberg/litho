package semantics

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// buildPDF assembles objects into a document with a correct cross-reference
// table, so the reader takes the ordinary path rather than the rebuild path.
func buildPDF(objs []string) []byte {
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

// annotAttachmentPDF builds a document whose only embedded file hangs off a
// FileAttachment annotation, with no /Names -> /EmbeddedFiles tree at all.
func annotAttachmentPDF() []byte {
	const payload = "hello attachment"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] /FS 5 0 R >>",
		"<< /Type /Filespec /F (secret.txt) /UF (secret.txt) /EF << /F 6 0 R >> >>",
		fmt.Sprintf("<< /Length %d /Subtype /text#2Fplain >>\nstream\n%s\nendstream",
			len(payload), payload),
	})
}

// sharedAttachmentPDF builds a document where one embedded file stream is
// reachable both from the name tree and from a FileAttachment annotation, which
// is what a producer writes when it attaches a file and also drops a marker on
// the page.
func sharedAttachmentPDF() []byte {
	const payload = "hello attachment"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 7 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] /FS 5 0 R >>",
		"<< /Type /Filespec /F (secret.txt) /UF (secret.txt) /EF << /F 6 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(payload), payload),
		"<< /Names [(secret.txt) 5 0 R] >>",
	})
}

func openBytes(t *testing.T, data []byte) *document.Document {
	t.Helper()

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestExtractEmbeddedFilesFindsAnnotationAttachment pins that a file carried by
// a FileAttachment annotation is listed.
//
// ISO 32000-1, 12.5.6.15 puts the file specification in the annotation's /FS,
// so it never reaches the /Names -> /EmbeddedFiles tree. Listing only that tree
// reported "(no attachments)" for a document litho's own compliance engine
// flags under PDF/A-1 clause 6.1.11 for containing an embedded file.
func TestExtractEmbeddedFilesFindsAnnotationAttachment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	files, err := ExtractEmbeddedFiles(ctx, openBytes(t, annotAttachmentPDF()))
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("listed %d files, want 1: the FileAttachment annotation carries one", len(files))
	}
	if files[0].Name != "secret.txt" {
		t.Errorf("name = %q, want %q", files[0].Name, "secret.txt")
	}
	if files[0].Open == nil {
		t.Fatal("the listed file has no Open accessor")
	}

	rc, err := files[0].Open(ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	var got bytes.Buffer
	if _, err := got.ReadFrom(rc); err != nil {
		t.Fatalf("reading attachment: %v", err)
	}
	if got.String() != "hello attachment" {
		t.Errorf("content = %q, want %q", got.String(), "hello attachment")
	}
}

// TestExtractPortfolioItemsIgnoresAnnotationAttachment is the control: the
// portfolio view is the name tree, and an annotation attachment is not a
// portfolio item.
func TestExtractPortfolioItemsIgnoresAnnotationAttachment(t *testing.T) {
	t.Parallel()

	files, err := ExtractPortfolioItems(context.Background(), openBytes(t, annotAttachmentPDF()))
	if err != nil {
		t.Fatalf("ExtractPortfolioItems: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("listed %d portfolio items, want 0", len(files))
	}
}

// TestExtractEmbeddedFilesReportsSharedStreamOnce pins that a stream reachable
// from both the name tree and an annotation is not listed twice.
func TestExtractEmbeddedFilesReportsSharedStreamOnce(t *testing.T) {
	t.Parallel()

	files, err := ExtractEmbeddedFiles(context.Background(), openBytes(t, sharedAttachmentPDF()))
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}
	if len(files) != 1 {
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = f.Name
		}
		t.Fatalf("listed %d files, want 1: the annotation points at the tree's stream (%v)", len(files), names)
	}
}

// TestExtractEmbeddedFilesKeepsNameTreeFiles is the control for the ordinary
// case: a document-level attachment with no annotation is still listed.
func TestExtractEmbeddedFilesKeepsNameTreeFiles(t *testing.T) {
	t.Parallel()

	const payload = "hello attachment"
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 6 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Filespec /F (doc.txt) /UF (doc.txt) /EF << /F 5 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(payload), payload),
		"<< /Names [(doc.txt) 4 0 R] >>",
	})

	files, err := ExtractEmbeddedFiles(context.Background(), openBytes(t, data))
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}
	if len(files) != 1 || files[0].Name != "doc.txt" {
		t.Fatalf("got %d files %+v, want the one name tree entry", len(files), files)
	}
}

// afAttachmentPDF builds a document whose only embedded file is associated
// through the catalog's /AF array, with no name tree and no annotation.
func afAttachmentPDF() []byte {
	const payload = "hello associated"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AF [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Filespec /F (data.xml) /UF (data.xml) /AFRelationship /Data /EF << /F 5 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(payload), payload),
	})
}

// TestExtractEmbeddedFilesFindsAssociatedFile pins that a file attached only
// through /AF is listed.
//
// ISO 32000-2, 14.13 associates a file with the document through /AF, and a
// producer may use it as the only route: pdfjs bug1937438_mml_from_latex.pdf
// attaches two stylesheets that way with no name tree, and litho's own
// compliance engine flags them under PDF/A-1 clause 6.1.11 while
// `litho attachments` reported none.
func TestExtractEmbeddedFilesFindsAssociatedFile(t *testing.T) {
	t.Parallel()

	files, err := ExtractEmbeddedFiles(context.Background(), openBytes(t, afAttachmentPDF()))
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("listed %d files, want 1: the /AF entry carries one", len(files))
	}
	if files[0].Name != "data.xml" {
		t.Errorf("name = %q, want %q", files[0].Name, "data.xml")
	}
}

// TestAssociatedFileAlsoInTheTreeReportedOnce pins that a file the producer put
// in both places is not listed twice.
func TestAssociatedFileAlsoInTheTreeReportedOnce(t *testing.T) {
	t.Parallel()

	const payload = "hello associated"
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AF [4 0 R] /Names << /EmbeddedFiles 6 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Filespec /F (data.xml) /UF (data.xml) /EF << /F 5 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(payload), payload),
		"<< /Names [(data.xml) 4 0 R] >>",
	})

	files, err := ExtractEmbeddedFiles(context.Background(), openBytes(t, data))
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("listed %d files, want 1: /AF names the stream the tree already supplied", len(files))
	}
}

// TestPageAssociatedFileFound pins that /AF on a page is walked too, since
// ISO 32000-2, 14.13 allows associating a file with a part of the document.
func TestPageAssociatedFileFound(t *testing.T) {
	t.Parallel()

	const payload = "page associated"
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /AF [4 0 R] >>",
		"<< /Type /Filespec /F (page.txt) /UF (page.txt) /EF << /F 5 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(payload), payload),
	})

	files, err := ExtractEmbeddedFiles(context.Background(), openBytes(t, data))
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}
	if len(files) != 1 || files[0].Name != "page.txt" {
		t.Errorf("got %d files %+v, want the page's associated file", len(files), files)
	}
}
