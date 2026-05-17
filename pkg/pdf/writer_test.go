package pdf

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/models"
)

// writerTestPDF builds a minimal valid PDF with a Catalog, one page, and an
// Info dictionary.
func writerTestPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"4 0 obj\n<< /Title (Original Title) /Author (Jane) >>\nendobj\n" +
			"xref\n0 5\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"0000000198 00000 n \r\n" +
			"trailer\n<< /Size 5 /Root 1 0 R /Info 4 0 R >>\n" +
			"startxref\n256\n%%EOF\n")
}

// threePagePDF builds a valid PDF with 3 pages, each containing text.
func threePagePDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			// Catalog
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			// Pages
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 6 0 R 8 0 R] /Count 3 >>\nendobj\n" +
			// Font
			"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			// Resources
			"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
			// Page 1
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 10 0 R >>\nendobj\n" +
			// Page 1 content
			"10 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Page1) Tj ET\nendstream\nendobj\n" +
			// Page 2
			"6 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 11 0 R >>\nendobj\n" +
			// Page 2 content
			"11 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Page2) Tj ET\nendstream\nendobj\n" +
			// Page 3
			"8 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 792 612] /Resources 5 0 R /Contents 12 0 R >>\nendobj\n" +
			// Page 3 content (landscape)
			"12 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 500 Td (Page3) Tj ET\nendstream\nendobj\n" +
			// Info
			"9 0 obj\n<< /Title (Three Pages) >>\nendobj\n" +
			// xref
			"xref\n0 13\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000248 00000 n \r\n" +
			"0000000131 00000 n \r\n" +
			"0000000204 00000 n \r\n" +
			"0000000413 00000 n \r\n" +
			"0000000000 00000 f \r\n" +
			"0000000578 00000 n \r\n" +
			"0000000744 00000 n \r\n" +
			"0000000380 00000 n \r\n" +
			"0000000545 00000 n \r\n" +
			"0000000710 00000 n \r\n" +
			"trailer\n<< /Size 13 /Root 1 0 R /Info 9 0 R >>\n" +
			"startxref\n790\n%%EOF\n")
}

// formPDF builds a minimal PDF with an AcroForm containing two text fields.
func formPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			// Catalog with AcroForm
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>\nendobj\n" +
			// Pages
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			// Page
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R 6 0 R] >>\nendobj\n" +
			// Font
			"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			// Field 1: name
			"5 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (John) /Rect [100 700 300 720] >>\nendobj\n" +
			// Field 2: email
			"6 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (email) /V (john@example.com) /Rect [100 660 300 680] >>\nendobj\n" +
			// AcroForm
			"7 0 obj\n<< /Fields [5 0 R 6 0 R] >>\nendobj\n" +
			// xref
			"xref\n0 8\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000076 00000 n \r\n" +
			"0000000133 00000 n \r\n" +
			"0000000252 00000 n \r\n" +
			"0000000325 00000 n \r\n" +
			"0000000440 00000 n \r\n" +
			"0000000564 00000 n \r\n" +
			"trailer\n<< /Size 8 /Root 1 0 R >>\n" +
			"startxref\n608\n%%EOF\n")
}

// mockSigner is a test Signer that produces a deterministic fake signature.
type mockSigner struct {
	signedDigest []byte
}

func (m *mockSigner) Sign(digest []byte) ([]byte, error) {
	m.signedDigest = digest
	// Return a fake PKCS#7 blob (not valid, but exercises the flow).
	return bytes.Repeat([]byte{0xDE, 0xAD}, 128), nil
}

func (m *mockSigner) Certificate() *x509.Certificate {
	return &x509.Certificate{} // empty, sufficient for tests
}

func (m *mockSigner) EstimatedSize() int {
	return 8192
}

func TestWriter_SetMetadata(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetMetadata(Metadata{
		Title:  "New Title",
		Author: "John",
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open the written PDF and verify metadata.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	meta, err := r2.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}

	if meta["Title"] != "New Title" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "New Title")
	}
	if meta["Author"] != "John" {
		t.Errorf("Author: got %q, want %q", meta["Author"], "John")
	}
}

func TestWriter_RotatePages(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	if err := w.RotatePages(90, 1); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify rotation.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	page, err := r2.Page(ctx, 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}

	if page.Rotate != 90 {
		t.Errorf("Rotate: got %d, want 90", page.Rotate)
	}
	if !page.IsLandscape {
		t.Error("expected landscape after 90° rotation")
	}
}

func TestWriter_RotatePages_InvalidAngle(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	if err := w.RotatePages(45, 1); err == nil {
		t.Error("expected error for non-90° rotation")
	}
}

func TestWriter_SetBookmarks(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetBookmarks([]models.Bookmark{
		{Title: "Chapter 1", PageNumber: 1},
		{Title: "Chapter 2", PageNumber: 1, Children: []models.Bookmark{
			{Title: "Section 2.1", PageNumber: 1},
		}},
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify bookmarks.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	bms, err := r2.Bookmarks(ctx)
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}

	if len(bms) != 2 {
		t.Fatalf("bookmark count: got %d, want 2", len(bms))
	}
	if bms[0].Title != "Chapter 1" {
		t.Errorf("bookmark 0 title: got %q, want %q", bms[0].Title, "Chapter 1")
	}
	if bms[1].Title != "Chapter 2" {
		t.Errorf("bookmark 1 title: got %q, want %q", bms[1].Title, "Chapter 2")
	}
	if len(bms[1].Children) != 1 {
		t.Fatalf("bookmark 1 children: got %d, want 1", len(bms[1].Children))
	}
	if bms[1].Children[0].Title != "Section 2.1" {
		t.Errorf("child title: got %q, want %q", bms[1].Children[0].Title, "Section 2.1")
	}
}

func TestWriter_NoMutations(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Output should be identical to input (just a copy).
	if !bytes.Equal(out.Bytes(), data) {
		t.Error("expected output to be identical to input when no mutations applied")
	}
}

func TestWriter_MultipleMutations(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetMetadata(Metadata{Title: "Combined"})
	if err := w.RotatePages(180, 1); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify both mutations.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	meta, err := r2.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "Combined" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Combined")
	}

	page, err := r2.Page(ctx, 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Rotate != 180 {
		t.Errorf("Rotate: got %d, want 180", page.Rotate)
	}
}

// headingPDF builds a 2-page PDF with distinct font sizes for heading detection.
// Uses 24pt for main headings, 18pt for subheadings, and 10pt for body text.
func headingPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>\nendobj\n" +
			"5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			"6 0 obj\n<< /Font << /F1 5 0 R >> >>\nendobj\n" +
			// Page 1: "Introduction" at 24pt, body at 10pt, "Overview" at 18pt
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 6 0 R /Contents 7 0 R >>\nendobj\n" +
			"7 0 obj\n<< /Length 276 >>\nstream\n" +
			"BT /F1 24 Tf 100 750 Td (Introduction) Tj ET\n" +
			"BT /F1 10 Tf 100 720 Td (This body text appears many times across the document) Tj ET\n" +
			"BT /F1 10 Tf 100 700 Td (More body content here to establish the common size) Tj ET\n" +
			"BT /F1 18 Tf 100 660 Td (Overview) Tj ET\n" +
			"endstream\nendobj\n" +
			// Page 2: "Details" at 24pt, body at 10pt
			"4 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 6 0 R /Contents 8 0 R >>\nendobj\n" +
			"8 0 obj\n<< /Length 200 >>\nstream\n" +
			"BT /F1 24 Tf 100 750 Td (Details) Tj ET\n" +
			"BT /F1 10 Tf 100 720 Td (Detailed content here and here and here) Tj ET\n" +
			"BT /F1 10 Tf 100 700 Td (Even more detailed body content text) Tj ET\n" +
			"endstream\nendobj\n" +
			"xref\n0 9\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000179 00000 n \r\n" +
			"0000000435 00000 n \r\n" +
			"0000000111 00000 n \r\n" +
			"0000000172 00000 n \r\n" +
			"0000000262 00000 n \r\n" +
			"0000000520 00000 n \r\n" +
			"trailer\n<< /Size 9 /Root 1 0 R >>\n" +
			"startxref\n690\n%%EOF\n")
}

func TestReader_GenerateBookmarks(t *testing.T) {
	data := headingPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	bms, err := r.GenerateBookmarks(ctx, BookmarkGenerationOptions{})
	if err != nil {
		t.Fatalf("GenerateBookmarks: %v", err)
	}

	if len(bms) == 0 {
		t.Fatal("expected at least one bookmark")
	}

	// "Introduction" (24pt) should be level 1.
	if bms[0].Title != "Introduction" {
		t.Errorf("first bookmark: got %q, want %q", bms[0].Title, "Introduction")
	}
	if bms[0].PageNumber != 1 {
		t.Errorf("first bookmark page: got %d, want 1", bms[0].PageNumber)
	}
}

func TestWriter_GenerateBookmarks(t *testing.T) {
	data := headingPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.GenerateBookmarks(BookmarkGenerationOptions{}, BookmarkReplace)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	bms, err := r2.Bookmarks(ctx)
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}

	if len(bms) == 0 {
		t.Fatal("expected at least one generated bookmark")
	}

	// Verify the first generated bookmark is the largest heading.
	if bms[0].Title != "Introduction" {
		t.Errorf("first bookmark: got %q, want %q", bms[0].Title, "Introduction")
	}
}

func TestWriter_AddAttachment(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.AddAttachment("test.txt", []byte("Hello from attachment"), "text/plain")

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify the attachment exists.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	files, err := r2.EmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("attachment count: got %d, want 1", len(files))
	}
	if files[0].Name != "test.txt" {
		t.Errorf("name: got %q, want %q", files[0].Name, "test.txt")
	}
}

func TestWriter_AddMultipleAttachments(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.AddAttachment("a.txt", []byte("File A"), "text/plain")
	w.AddAttachment("b.csv", []byte("col1,col2"), "text/csv")

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	files, err := r2.EmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("ExtractEmbeddedFiles: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("attachment count: got %d, want 2", len(files))
	}
}

func TestWriter_Encrypt_AES256(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.Encrypt(EncryptConfig{
		UserPassword:  "user123",
		OwnerPassword: "owner456",
		Algorithm:     EncryptAES256,
		Permissions:   -4, // allow printing
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open without password should fail.
	_, err = Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err == nil {
		t.Error("expected error opening encrypted PDF without password")
	}

	// Re-open with correct user password.
	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("user123"))
	if err != nil {
		t.Fatalf("Open with user password: %v", err)
	}

	meta, err := r3.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "Original Title" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Original Title")
	}

	// Re-open with correct owner password.
	r4, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("owner456"))
	if err != nil {
		t.Fatalf("Open with owner password: %v", err)
	}

	meta2, err := r4.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta2["Title"] != "Original Title" {
		t.Errorf("Title: got %q, want %q", meta2["Title"], "Original Title")
	}
}

func TestWriter_Encrypt_AES128(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.Encrypt(EncryptConfig{
		UserPassword:  "test",
		OwnerPassword: "test",
		Algorithm:     EncryptAES128,
		Permissions:   -4,
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open with password.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("test"))
	if err != nil {
		t.Fatalf("Open encrypted: %v", err)
	}

	meta, err := r2.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "Original Title" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Original Title")
	}
}

func TestWriter_Decrypt(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	// First, encrypt the document.
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	w.Encrypt(EncryptConfig{
		UserPassword: "secret",
		Algorithm:    EncryptAES256,
		Permissions:  -4,
	})
	var encrypted bytes.Buffer
	if err := w.WriteTo(ctx, &encrypted); err != nil {
		t.Fatalf("encrypt WriteTo: %v", err)
	}

	// Now decrypt it.
	r2, err := Open(ctx, bytes.NewReader(encrypted.Bytes()), int64(encrypted.Len()), WithPassword("secret"))
	if err != nil {
		t.Fatalf("Open encrypted: %v", err)
	}

	w2 := NewWriter(r2)
	w2.RemoveEncryption()

	var decrypted bytes.Buffer
	if err := w2.WriteTo(ctx, &decrypted); err != nil {
		t.Fatalf("decrypt WriteTo: %v", err)
	}

	// Re-open without password. Should work now.
	r3, err := Open(ctx, bytes.NewReader(decrypted.Bytes()), int64(decrypted.Len()))
	if err != nil {
		t.Fatalf("Open decrypted: %v", err)
	}

	if r3.doc.IsEncrypted {
		t.Error("expected document to not be encrypted after decrypt")
	}

	meta, err := r3.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "Original Title" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Original Title")
	}
}

func TestWriter_EncryptAndModify(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	w.SetMetadata(Metadata{Title: "Encrypted Doc"})
	w.Encrypt(EncryptConfig{
		UserPassword: "pw",
		Algorithm:    EncryptAES256,
		Permissions:  -4,
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("pw"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	meta, err := r2.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "Encrypted Doc" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Encrypted Doc")
	}
}

func TestWriter_SetFormField(t *testing.T) {
	data := formPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetFormField("name", "Jane Doe")
	w.SetFormField("email", "jane@example.com")

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open modified: %v", err)
	}

	fieldMap, err := r2.FormData(ctx)
	if err != nil {
		t.Fatalf("FormData: %v", err)
	}

	if fieldMap["name"] != "Jane Doe" {
		t.Errorf("name: got %q, want %q", fieldMap["name"], "Jane Doe")
	}
	if fieldMap["email"] != "jane@example.com" {
		t.Errorf("email: got %q, want %q", fieldMap["email"], "jane@example.com")
	}
}

func TestWriter_SetFormField_NotFound(t *testing.T) {
	data := formPDF()
	ctx := context.Background()

	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	w.SetFormField("nonexistent", "value")

	var out bytes.Buffer
	err := w.WriteTo(ctx, &out)
	if err == nil {
		t.Error("expected error for nonexistent field")
	}
}

func TestWriter_FlattenForm(t *testing.T) {
	data := formPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.FlattenForm()

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify AcroForm is removed.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open flattened: %v", err)
	}

	if r2.HasAcroForm(ctx) {
		t.Error("expected AcroForm to be removed after flatten")
	}
}

// buildFormPDFWithAppearances builds a minimal PDF containing two widget
// annotations, each with a /AP/N Form XObject appearance stream. After
// flattening, the resulting page dict must contain both XObject entries
// (FlatAP<N1> and FlatAP<N2>) AND the corresponding `/FlatAPN Do`
// invocations in /Contents. This is the regression fixture for the
// write-overlap bug where addXObjectToPage + appendToPageContents +
// /Annots update each clobbered the other's mutations.
func buildFormPDFWithAppearances(t *testing.T) []byte {
	t.Helper()
	// Build objects as strings; record each object's byte offset as we
	// append, so xref entries don't have to be hand-counted.
	var (
		body    bytes.Buffer
		offsets = []int64{0} // object 0 is the free head
	)
	add := func(body2 *bytes.Buffer, obj string) {
		offsets = append(offsets, int64(body2.Len()))
		body2.WriteString(obj)
	}

	body.WriteString("%PDF-1.4\n")

	// 1 Catalog
	add(&body, "1 0 obj\n<< /Type /Catalog /Pages 2 0 R /AcroForm 6 0 R >>\nendobj\n")
	// 2 Pages
	add(&body, "2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	// 3 Page
	add(&body, "3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R] >>\nendobj\n")
	// 4 Widget 1 (with /AP/N → 7 0 R)
	add(&body, "4 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (John) /Rect [100 700 200 720] /AP << /N 7 0 R >> >>\nendobj\n")
	// 5 Widget 2 (with /AP/N → 8 0 R)
	add(&body, "5 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (email) /V (j@x) /Rect [100 660 200 680] /AP << /N 8 0 R >> >>\nendobj\n")
	// 6 AcroForm
	add(&body, "6 0 obj\n<< /Fields [4 0 R 5 0 R] >>\nendobj\n")
	// 7 Appearance stream for widget 1 (Form XObject)
	add(&body, "7 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 100 20] /Length 8 >>\nstream\nq 1 0 Q\nendstream\nendobj\n")
	// 8 Appearance stream for widget 2
	add(&body, "8 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 100 20] /Length 8 >>\nstream\nq 2 0 Q\nendstream\nendobj\n")

	xrefStart := body.Len()
	body.WriteString("xref\n0 9\n")
	body.WriteString("0000000000 65535 f \r\n")
	for i := 1; i <= 8; i++ {
		body.WriteString(fmt.Sprintf("%010d 00000 n \r\n", offsets[i]))
	}
	body.WriteString("trailer\n<< /Size 9 /Root 1 0 R >>\n")
	body.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefStart))

	return body.Bytes()
}

func TestWriter_FlattenForm_XObjectAndContentsSurvive(t *testing.T) {
	// Regression guard for the flattenPageAnnotations write-overlap bug.
	// Before the fix, addXObjectToPage, appendToPageContents, and the
	// /Annots update each wrote a fresh page dict derived from the
	// original page.Dict, so each AllocAt clobbered the previous
	// mutations. After the fix, all three mutations go through a single
	// AllocAt and must coexist.
	data := buildFormPDFWithAppearances(t)
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.FlattenForm()

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	outStr := out.String()

	// AcroForm must be removed from the catalog.
	if r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len())); err != nil {
		t.Fatalf("Open flattened: %v", err)
	} else if r2.HasAcroForm(ctx) {
		t.Error("expected AcroForm to be removed after flatten")
	}

	// The output must reference two FlatAP Form XObjects by name. The
	//    XObject allocator picks names like "FlatAP7" and "FlatAP8" based
	//    on object numbers; match the prefix and count occurrences.
	flatAPCount := strings.Count(outStr, "/FlatAP")
	// Expected references: 1× in /Resources/XObject, 1× in /Contents (Do
	// invocation) per widget. With two widgets that's 4 references.
	if flatAPCount < 4 {
		t.Errorf("expected at least 4 /FlatAP references (2 resources + 2 Do ops), got %d", flatAPCount)
	}

	// The content stream must contain two Do invocations.
	doCount := strings.Count(outStr, " Do Q")
	if doCount < 2 {
		t.Errorf("expected at least 2 `Do Q` invocations in flattened content, got %d", doCount)
	}
}

func TestMerge_TwoDocuments(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r1, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r1: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open r2: %v", err)
	}

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r1, r2}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Re-open merged PDF.
	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open merged: %v", err)
	}

	n, err := r3.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 6 {
		t.Errorf("page count: got %d, want 6", n)
	}

	// Verify text from original pages.
	for i, want := range []string{"Page1", "Page2", "Page3", "Page1", "Page2", "Page3"} {
		text, err := r3.ExtractText(ctx, i+1)
		if err != nil {
			t.Fatalf("ExtractText page %d: %v", i+1, err)
		}
		if text != want {
			t.Errorf("page %d text: got %q, want %q", i+1, text, want)
		}
	}
}

func TestMerge_PreservesMetadata(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r1, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	r2, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r1, r2}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open merged: %v", err)
	}

	meta, err := r3.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "Three Pages" {
		t.Errorf("title: got %q, want %q", meta["Title"], "Three Pages")
	}
}

func TestMerge_SingleDocument(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r}, &out); err != nil {
		t.Fatalf("Merge single: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	n, err := r2.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 3 {
		t.Errorf("page count: got %d, want 3", n)
	}
}

func TestMerge_Empty(t *testing.T) {
	ctx := context.Background()
	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{}, &out); err == nil {
		t.Error("expected error for empty sources")
	}
}

func TestWriter_RemediatePDFA_BasicFixes(t *testing.T) {
	// This PDF is non-compliant: no XMP metadata, no OutputIntent, no MarkInfo,
	// no StructTreeRoot, no ID, no ViewerPreferences.
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Validate before remediation to confirm violations exist.
	violationsBefore, err := r.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA before: %v", err)
	}
	if len(violationsBefore) == 0 {
		t.Skip("document already compliant, nothing to test")
	}
	t.Logf("violations before remediation: %d", len(violationsBefore))

	// Remediate.
	w := NewWriter(r)
	report, err := w.RemediatePDFA(ctx, models.PDFA2b, WithLanguage("en"))
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}

	if len(report.Fixed) == 0 {
		t.Error("expected at least one fix")
	}
	for _, fix := range report.Fixed {
		t.Logf("fixed: %s", fix)
	}
	t.Logf("residual violations: %d", len(report.Residual))

	// Write the remediated document.
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and validate.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open remediated: %v", err)
	}

	violationsAfter, err := r2.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA after: %v", err)
	}

	t.Logf("violations after remediation: %d", len(violationsAfter))
	if len(violationsAfter) >= len(violationsBefore) {
		t.Errorf("remediation did not reduce violations: before=%d after=%d", len(violationsBefore), len(violationsAfter))
	}
}

func TestWriter_RemediatePDFA_ReportContents(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	report, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}

	// Should have fixed at least XMP metadata and trailer ID.
	fixMap := make(map[string]bool)
	for _, fix := range report.Fixed {
		fixMap[fix] = true
	}

	if !fixMap["generated XMP metadata stream"] {
		t.Errorf("expected XMP fix in report: %v", report.Fixed)
	}
	if !fixMap["added /ID to trailer"] {
		t.Errorf("expected trailer ID fix in report: %v", report.Fixed)
	}
}

func TestWriter_RemediatePDFA_Idempotent(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	// First pass: remediate.
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	_, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("first RemediatePDFA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("first WriteTo: %v", err)
	}

	// Second pass: remediate the already-remediated document.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open remediated: %v", err)
	}
	w2 := NewWriter(r2)
	report2, err := w2.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("second RemediatePDFA: %v", err)
	}

	// The second pass should not find new things to fix (or very few).
	t.Logf("second pass fixes: %d, residual: %d", len(report2.Fixed), len(report2.Residual))
}

func TestWriter_RemediatePDFUA_BasicFixes(t *testing.T) {
	// writerTestPDF is missing the basic accessibility metadata needed for
	// PDF/UA conformance: no MarkInfo, no XMP metadata, no document Lang.
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	violationsBefore, err := r.ValidatePDFUA(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA before: %v", err)
	}
	if len(violationsBefore) == 0 {
		t.Skip("document already PDF/UA-1 compliant, nothing to test")
	}
	t.Logf("violations before remediation: %d", len(violationsBefore))

	w := NewWriter(r)
	report, err := w.RemediatePDFUA(ctx, models.PDFUA1, WithLanguage("en"))
	if err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	for _, fix := range report.Fixed {
		t.Logf("fixed: %s", fix)
	}
	t.Logf("residual violations: %d", len(report.Residual))

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open remediated: %v", err)
	}
	violationsAfter, err := r2.ValidatePDFUA(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA after: %v", err)
	}
	t.Logf("violations after remediation: %d", len(violationsAfter))
	if len(violationsAfter) > len(violationsBefore) {
		t.Errorf("remediation increased violations: before=%d after=%d",
			len(violationsBefore), len(violationsAfter))
	}
}

func TestPipeline_RemediatePDFUA(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var report *RemediationReport
	var out bytes.Buffer
	_, err = NewPipeline().
		Open(r).
		Apply(func(applyCtx context.Context, w *Writer) error {
			var remErr error
			report, remErr = w.RemediatePDFUA(applyCtx, models.PDFUA1, WithLanguage("en"))
			return remErr
		}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("Pipeline.WriteTo: %v", err)
	}
	if report == nil {
		t.Fatal("expected remediation report, got nil")
	}
	if out.Len() == 0 {
		t.Error("pipeline produced no output")
	}
}

func TestWriter_Sign(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	signer := &mockSigner{}
	w := NewWriter(r)
	w.Sign(signer)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Verify the signer was called with a non-empty digest.
	if len(signer.signedDigest) == 0 {
		t.Error("signer was not called")
	}
	if len(signer.signedDigest) != 32 {
		t.Errorf("digest length: got %d, want 32 (SHA-256)", len(signer.signedDigest))
	}

	// Re-open and verify the document has a signature field.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open signed: %v", err)
	}

	sigs, err := r2.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}

	if len(sigs) == 0 {
		t.Error("expected at least one signature")
	}
}

func TestSigner_ExtractedSignatureFieldsArePopulated(t *testing.T) {
	// Regression guard that pins down the end-to-end sign → extract
	// round trip: after signing a minimal PDF with the mockSigner, the
	// extracted signature must have a non-zero byte range, non-empty
	// Contents, a non-empty document hash, and a recognisable field
	// name. A bug in the writer's ByteRange calculation or the
	// reader's signature extractor would light this up.
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	w.Sign(&mockSigner{})
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open signed: %v", err)
	}
	sigs, err := r2.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	if len(sigs) != 1 {
		t.Fatalf("got %d signatures, want 1", len(sigs))
	}
	sig := sigs[0]

	if sig.FieldName == "" {
		t.Error("signature field name should be populated after signing")
	}
	// ByteRange has 4 int64 entries: [start1, len1, start2, len2]. Only
	// len2 varies with the PDF size; start1 is always 0. The second
	// range must start after the /Contents hex block, so start2 must
	// be strictly greater than len1.
	if sig.ByteRange[0] != 0 {
		t.Errorf("ByteRange[0]: got %d, want 0", sig.ByteRange[0])
	}
	if sig.ByteRange[1] <= 0 {
		t.Errorf("ByteRange[1] (length of first range): got %d, want > 0", sig.ByteRange[1])
	}
	if sig.ByteRange[2] <= sig.ByteRange[1] {
		t.Errorf("ByteRange[2] (start of second range): got %d, want > %d",
			sig.ByteRange[2], sig.ByteRange[1])
	}
	if sig.ByteRange[3] <= 0 {
		t.Errorf("ByteRange[3] (length of second range): got %d, want > 0", sig.ByteRange[3])
	}
	if sig.Contents == "" {
		t.Error("signature Contents should be non-empty (contains the signer's PKCS7 blob)")
	}
	if sig.DocumentHash == "" {
		t.Error("signature DocumentHash should be populated")
	}
}

func TestVerifySignatures_CallableAfterRoundTrip(t *testing.T) {
	// Signing with mockSigner produces a fake PKCS7 blob, so actual
	// cryptographic validation will fail, but VerifySignatures itself
	// must return a non-nil result slice without panicking, and the
	// per-signature result must record the failure reason rather than
	// disappearing. This locks in that the verify pipeline is
	// wired end-to-end.
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	w.Sign(&mockSigner{})
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open signed: %v", err)
	}
	// Pass nil root pool. The verifier should not panic on a missing
	// trust store; it reports cert-chain-invalid instead.
	results, err := r2.VerifySignatures(ctx, nil)
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d validation results, want 1", len(results))
	}
	if results[0].FieldName == "" {
		t.Error("validation result should carry the signature field name")
	}
}

func TestWriter_SignAndModify(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetMetadata(Metadata{Title: "Signed Document"})
	w.Sign(&mockSigner{})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	meta, err := r2.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "Signed Document" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Signed Document")
	}

	sigs, err := r2.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	if len(sigs) == 0 {
		t.Error("expected signature")
	}
}

func TestWriter_Sign_ByteRangeValid(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.Sign(&mockSigner{})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Extract ByteRange from the signature and verify it covers the whole file.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open signed: %v", err)
	}

	sigs, err := r2.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	if len(sigs) == 0 {
		t.Fatal("no signatures found")
	}

	br := sigs[0].ByteRange
	// ByteRange should span the entire file: [0, start, end, total-end].
	// br[0]+br[1] should be the start of /Contents, br[2]+br[3] should be total length.
	totalLen := int64(out.Len())
	coveredBytes := br[1] + br[3]
	signatureGap := br[2] - br[1] // the /Contents hex string
	if coveredBytes+signatureGap != totalLen {
		t.Errorf("ByteRange doesn't cover full file: covered=%d gap=%d total=%d", coveredBytes, signatureGap, totalLen)
	}
}

func TestSplit_SinglePage(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var out bytes.Buffer
	if err := Split(ctx, r, []int{2}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}

	// Re-open the split PDF.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open split: %v", err)
	}

	n, err := r2.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 1 {
		t.Errorf("page count: got %d, want 1", n)
	}

	text, err := r2.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if text != "Page2" {
		t.Errorf("text: got %q, want %q", text, "Page2")
	}
}

func TestSplit_MultiplePages(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var out bytes.Buffer
	if err := Split(ctx, r, []int{3, 1}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open split: %v", err)
	}

	n, err := r2.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 2 {
		t.Errorf("page count: got %d, want 2", n)
	}

	// Page 1 of split should be original page 3 (landscape).
	page, err := r2.Page(ctx, 1)
	if err != nil {
		t.Fatalf("GetPage 1: %v", err)
	}
	if !page.IsLandscape {
		t.Error("page 1: expected landscape (from original page 3)")
	}

	// Page 2 of split should be original page 1.
	text, err := r2.ExtractText(ctx, 2)
	if err != nil {
		t.Fatalf("ExtractText page 2: %v", err)
	}
	if text != "Page1" {
		t.Errorf("page 2 text: got %q, want %q", text, "Page1")
	}
}

func TestSplit_PreservesMetadata(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var out bytes.Buffer
	if err := Split(ctx, r, []int{1}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open split: %v", err)
	}

	meta, err := r2.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "Three Pages" {
		t.Errorf("title: got %q, want %q", meta["Title"], "Three Pages")
	}
}

func TestSplit_InvalidPage(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	if err := Split(ctx, r, []int{5}, &out); err == nil {
		t.Error("expected error for out-of-bounds page")
	}
}

func TestSplit_EmptyPages(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	if err := Split(ctx, r, []int{}, &out); err == nil {
		t.Error("expected error for empty pages list")
	}
}

// pdfWithBookmarks builds a 3-page PDF then uses litho to add an outline.
// Returns the bytes of the bookmarked PDF.
func pdfWithBookmarks(t *testing.T, marks []models.Bookmark) []byte {
	t.Helper()
	ctx := context.Background()

	src := threePagePDF()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open base: %v", err)
	}

	w := NewWriter(r)
	w.SetBookmarks(marks)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

func TestSplit_BookmarkUpdate_DropOrphans(t *testing.T) {
	ctx := context.Background()
	// Bookmarks: A→p1, B→p2, C→p3
	data := pdfWithBookmarks(t, []models.Bookmark{
		{Title: "A", PageNumber: 1},
		{Title: "B", PageNumber: 2},
		{Title: "C", PageNumber: 3},
	})

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Keep only pages 1 and 3 → bookmarks A (new p1) and C (new p2).
	var out bytes.Buffer
	if err := Split(ctx, r, []int{1, 3}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open split: %v", err)
	}
	bms, err := r2.Bookmarks(ctx)
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	if len(bms) != 2 {
		t.Fatalf("got %d bookmarks, want 2 (A, C)", len(bms))
	}
	if bms[0].Title != "A" || bms[0].PageNumber != 1 {
		t.Errorf("first: %+v", bms[0])
	}
	if bms[1].Title != "C" || bms[1].PageNumber != 2 {
		t.Errorf("second: %+v", bms[1])
	}
}

func TestSplit_BookmarkNone(t *testing.T) {
	ctx := context.Background()
	data := pdfWithBookmarks(t, []models.Bookmark{
		{Title: "A", PageNumber: 1},
		{Title: "B", PageNumber: 2},
	})
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	if err := Split(ctx, r, []int{1, 2}, &out, WithBookmarks(BookmarkOptions{Handling: BookmarkNone})); err != nil {
		t.Fatalf("Split: %v", err)
	}

	r2, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	bms, _ := r2.Bookmarks(ctx)
	if len(bms) != 0 {
		t.Errorf("expected no bookmarks, got %d", len(bms))
	}
}

func TestSplit_BookmarkOrphanRemap(t *testing.T) {
	ctx := context.Background()
	data := pdfWithBookmarks(t, []models.Bookmark{
		{Title: "Skipped", PageNumber: 2}, // dropped
		{Title: "Kept", PageNumber: 3},
	})
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))

	var out bytes.Buffer
	err := Split(ctx, r, []int{1, 3}, &out, WithBookmarks(BookmarkOptions{
		Handling:     BookmarkUpdate,
		OrphanPolicy: OrphanRemap,
	}))
	if err != nil {
		t.Fatalf("Split: %v", err)
	}

	r2, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	bms, _ := r2.Bookmarks(ctx)
	if len(bms) != 2 {
		t.Fatalf("got %d, want 2", len(bms))
	}
	// "Skipped" remapped to nearest kept (page 1 or 3 → either is acceptable),
	// "Kept" should be at new page 2.
	for _, bm := range bms {
		if bm.PageNumber < 1 || bm.PageNumber > 2 {
			t.Errorf("page out of range: %+v", bm)
		}
	}
}

func TestMerge_BookmarkUpdate_OffsetPages(t *testing.T) {
	ctx := context.Background()
	src1 := pdfWithBookmarks(t, []models.Bookmark{
		{Title: "Doc1 A", PageNumber: 1},
		{Title: "Doc1 B", PageNumber: 2},
	})
	src2 := pdfWithBookmarks(t, []models.Bookmark{
		{Title: "Doc2 A", PageNumber: 1},
	})

	r1, _ := Open(ctx, bytes.NewReader(src1), int64(len(src1)))
	r2, _ := Open(ctx, bytes.NewReader(src2), int64(len(src2)))

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{r1, r2}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	r3, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	bms, _ := r3.Bookmarks(ctx)
	if len(bms) != 3 {
		t.Fatalf("got %d, want 3 bookmarks", len(bms))
	}
	// Doc2's "A" should now point to page 4 (offset by 3 pages from src1).
	last := bms[2]
	if last.Title != "Doc2 A" || last.PageNumber != 4 {
		t.Errorf("last bookmark: got %+v, want {Doc2 A, page 4}", last)
	}
}

func TestMerge_BookmarkWrapEachSource(t *testing.T) {
	ctx := context.Background()
	src1 := pdfWithBookmarks(t, []models.Bookmark{
		{Title: "Section", PageNumber: 1},
	})
	src2 := pdfWithBookmarks(t, []models.Bookmark{
		{Title: "Other", PageNumber: 1},
	})

	r1, _ := Open(ctx, bytes.NewReader(src1), int64(len(src1)))
	r2, _ := Open(ctx, bytes.NewReader(src2), int64(len(src2)))

	var out bytes.Buffer
	err := Merge(ctx, []*Reader{r1, r2}, &out, WithBookmarks(BookmarkOptions{
		WrapEachSource: true,
		SourceLabels:   []string{"User Guide", "Reference"},
	}))
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	r3, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	bms, _ := r3.Bookmarks(ctx)
	if len(bms) != 2 {
		t.Fatalf("expected 2 root wrappers, got %d", len(bms))
	}
	if bms[0].Title != "User Guide" || bms[1].Title != "Reference" {
		t.Errorf("wrapper labels wrong: %+v %+v", bms[0], bms[1])
	}
	if len(bms[0].Children) != 1 || bms[0].Children[0].Title != "Section" {
		t.Errorf("first wrapper children: %+v", bms[0].Children)
	}
}

func TestMerge_BookmarkNone(t *testing.T) {
	ctx := context.Background()
	src := pdfWithBookmarks(t, []models.Bookmark{{Title: "X", PageNumber: 1}})
	r1, _ := Open(ctx, bytes.NewReader(src), int64(len(src)))
	r2, _ := Open(ctx, bytes.NewReader(src), int64(len(src)))

	var out bytes.Buffer
	err := Merge(ctx, []*Reader{r1, r2}, &out, WithBookmarks(BookmarkOptions{Handling: BookmarkNone}))
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	r3, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	bms, _ := r3.Bookmarks(ctx)
	if len(bms) != 0 {
		t.Errorf("expected no bookmarks, got %d", len(bms))
	}
}

func TestWriter_StampText(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	if err := w.StampText(TextStamp{
		Text:     "CONFIDENTIAL",
		X:        200,
		Y:        400,
		FontSize: 36,
	}, 1); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open and verify the stamp text is extractable.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open stamped: %v", err)
	}

	text, err := r2.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}

	if !strings.Contains(text, "CONFIDENTIAL") {
		t.Errorf("expected stamp text in output, got: %q", text)
	}
}

func TestWriter_StampText_AllPages(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	if err := w.StampText(TextStamp{
		Text:     "DRAFT",
		X:        100,
		Y:        100,
		FontSize: 24,
	}); err != nil { // no pages = all pages
		t.Fatalf("StampText: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open stamped: %v", err)
	}

	for i := 1; i <= 3; i++ {
		text, err := r2.ExtractText(ctx, i)
		if err != nil {
			t.Fatalf("ExtractText page %d: %v", i, err)
		}
		if !strings.Contains(text, "DRAFT") {
			t.Errorf("page %d: expected DRAFT in text, got: %q", i, text)
		}
	}
}

func TestWriter_StampText_StandardFont(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	if err := w.StampText(TextStamp{
		Text:     "DRAFT",
		X:        100,
		Y:        500,
		FontSize: 48,
		Font:     FontTimesBold,
	}, 1); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/BaseFont /Times-Bold")) {
		t.Error("expected /BaseFont /Times-Bold in output")
	}
}

func TestWriter_StampText_Opacity_EmitsExtGState(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	if err := w.StampText(TextStamp{
		Text:     "CONFIDENTIAL",
		X:        100,
		Y:        400,
		FontSize: 24,
		Opacity:  0.3,
	}, 1); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	// ExtGState dict with /ca and /CA entries must be present in the output.
	if !bytes.Contains(out.Bytes(), []byte("/ExtGState")) {
		t.Error("expected /ExtGState in output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/ca 0.3")) && !bytes.Contains(out.Bytes(), []byte("/ca 0.30")) {
		t.Error("expected /ca 0.3 in ExtGState")
	}
}

func TestWriter_StampText_Rotation(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	if err := w.StampText(TextStamp{
		Text:     "ROTATED",
		X:        300,
		Y:        400,
		FontSize: 24,
		Rotation: 45,
	}, 1); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	// The content stream should contain a "cm" transform operator.
	if !bytes.Contains(out.Bytes(), []byte("cm")) {
		t.Error("expected cm operator in rotated stamp content stream")
	}
}

func TestNewTTFFont_RejectsShort(t *testing.T) {
	if _, err := NewTTFFont([]byte{0, 1, 2}); err == nil {
		t.Error("expected error for short TTF data")
	}
}

func TestWriter_StampText_CustomTTF(t *testing.T) {
	ttf := minimalTTF(t)
	font, err := NewTTFFont(ttf)
	if err != nil {
		t.Fatalf("NewTTFFont: %v", err)
	}

	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	if err := w.StampText(TextStamp{
		Text:     "ABC",
		X:        100,
		Y:        500,
		FontSize: 24,
		Font:     font,
	}, 1); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	// Output should reference a Type0 composite font and embed the TTF.
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /Type0")) {
		t.Error("expected /Subtype /Type0 in output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /CIDFontType2")) {
		t.Error("expected /Subtype /CIDFontType2 in output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/FontFile2")) {
		t.Error("expected /FontFile2 in output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/ToUnicode")) {
		t.Error("expected /ToUnicode in output")
	}
}

func TestWriter_StampImage_JPEG(t *testing.T) {
	jpeg := minimalJPEG(t)

	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	err = w.StampImage(ImageStamp{
		Data:     jpeg,
		MimeType: "image/jpeg",
		X:        100,
		Y:        500,
		Width:    64,
		Height:   64,
	}, 1)
	if err != nil {
		t.Fatalf("StampImage: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/DCTDecode")) {
		t.Error("expected /DCTDecode in JPEG stamp output")
	}
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /Image")) {
		t.Error("expected /Subtype /Image in output")
	}
}

func TestWriter_StampImage_PNGNoAlpha(t *testing.T) {
	pngData := buildPNG(t, false)

	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	err = w.StampImage(ImageStamp{
		Data:     pngData,
		MimeType: "image/png",
		X:        50,
		Y:        50,
	}, 1)
	if err != nil {
		t.Fatalf("StampImage: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/FlateDecode")) {
		t.Error("expected /FlateDecode in PNG stamp output")
	}
	if bytes.Contains(out.Bytes(), []byte("/SMask")) {
		t.Error("opaque PNG should not emit /SMask")
	}
}

func TestWriter_StampImage_PNGWithAlpha(t *testing.T) {
	pngData := buildPNG(t, true)

	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	err = w.StampImage(ImageStamp{
		Data:     pngData,
		MimeType: "image/png",
		X:        50,
		Y:        50,
	}, 1)
	if err != nil {
		t.Fatalf("StampImage: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/SMask")) {
		t.Error("expected /SMask for PNG with alpha")
	}
}

func TestWriter_StampImage_UnsupportedMime(t *testing.T) {
	data := threePagePDF()
	ctx := context.Background()
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	defer r.Close()
	w := NewWriter(r)
	err := w.StampImage(ImageStamp{Data: []byte("x"), MimeType: "image/gif"}, 1)
	if err == nil {
		t.Error("expected error for unsupported mime type")
	}
}

func TestWriter_StampPDF(t *testing.T) {
	// Use a copy of threePagePDF as both the target and the stamp source.
	targetData := threePagePDF()
	stampData := threePagePDF()

	ctx := context.Background()
	target, err := Open(ctx, bytes.NewReader(targetData), int64(len(targetData)))
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	defer target.Close()

	stamp, err := Open(ctx, bytes.NewReader(stampData), int64(len(stampData)))
	if err != nil {
		t.Fatalf("Open stamp: %v", err)
	}
	defer stamp.Close()

	w := NewWriter(target)
	err = w.StampPDF(PDFStamp{
		Source: stamp,
		Page:   2,
		X:      0,
		Y:      0,
		Scale:  0.5,
	}, 1)
	if err != nil {
		t.Fatalf("StampPDF: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("/Subtype /Form")) {
		t.Error("expected /Subtype /Form for PDF stamp")
	}
}

func TestWriter_StampPDF_InvalidPage(t *testing.T) {
	targetData := threePagePDF()
	stampData := threePagePDF()
	ctx := context.Background()
	target, _ := Open(ctx, bytes.NewReader(targetData), int64(len(targetData)))
	defer target.Close()
	stamp, _ := Open(ctx, bytes.NewReader(stampData), int64(len(stampData)))
	defer stamp.Close()

	w := NewWriter(target)
	if err := w.StampPDF(PDFStamp{Source: stamp, Page: 99}); err != nil {
		// Accept if the error surfaces at queue time.
		if strings.Contains(err.Error(), "out of range") {
			return
		}
	}
	// Otherwise the error must surface at WriteTo time.
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err == nil {
		t.Error("expected error for out-of-range PDF stamp page")
	}
}

// minimalJPEG returns a very small valid JPEG for stamp tests. Generated
// by encoding a 2x2 solid-red image.
func minimalJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	// Use PNG then convert to a synthetic minimal JPEG? Simpler: write a
	// hand-crafted minimal JPEG that only exercises the SOF marker path.
	return minimalSyntheticJPEG()
}

// minimalSyntheticJPEG returns the raw bytes of a tiny 1x1 black JPEG.
// The goal is to exercise the SOF marker parsing, not render a real image.
// This is an 8x8 grayscale JFIF.
func minimalSyntheticJPEG() []byte {
	return []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, // APP0
		0xFF, 0xDB, 0x00, 0x43, 0x00, // DQT (quantization table, 64 bytes)
		0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09, 0x09, 0x08, 0x0A, 0x0C, 0x14,
		0x0D, 0x0C, 0x0B, 0x0B, 0x0C, 0x19, 0x12, 0x13, 0x0F, 0x14, 0x1D, 0x1A, 0x1F, 0x1E, 0x1D, 0x1A,
		0x1C, 0x1C, 0x20, 0x24, 0x2E, 0x27, 0x20, 0x22, 0x2C, 0x23, 0x1C, 0x1C, 0x28, 0x37, 0x29, 0x2C,
		0x30, 0x31, 0x34, 0x34, 0x34, 0x1F, 0x27, 0x39, 0x3D, 0x38, 0x32, 0x3C, 0x2E, 0x33, 0x34, 0x32,
		0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x08, 0x00, 0x08, 0x01, 0x01, 0x11, 0x00, // SOF0: 8x8 grayscale
		0xFF, 0xC4, 0x00, 0x1F, 0x00, // DHT (DC luma)
		0x00, 0x01, 0x05, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B,
		0xFF, 0xC4, 0x00, 0xB5, 0x10, // DHT (AC luma)
		0x00, 0x02, 0x01, 0x03, 0x03, 0x02, 0x04, 0x03, 0x05, 0x05, 0x04, 0x04, 0x00, 0x00, 0x01, 0x7D,
		0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12, 0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
		0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xA1, 0x08, 0x23, 0x42, 0xB1, 0xC1, 0x15, 0x52, 0xD1, 0xF0,
		0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0A, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x25, 0x26, 0x27, 0x28,
		0x29, 0x2A, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3A, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
		0x4A, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
		0x6A, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
		0x8A, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9A, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7,
		0xA8, 0xA9, 0xAA, 0xB2, 0xB3, 0xB4, 0xB5, 0xB6, 0xB7, 0xB8, 0xB9, 0xBA, 0xC2, 0xC3, 0xC4, 0xC5,
		0xC6, 0xC7, 0xC8, 0xC9, 0xCA, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0xDA, 0xE1, 0xE2,
		0xE3, 0xE4, 0xE5, 0xE6, 0xE7, 0xE8, 0xE9, 0xEA, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8,
		0xF9, 0xFA,
		0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, // SOS
		0xFC, 0xFF, 0xD9, // EOI (minimal compressed data)
	}
}

// buildPNG creates a small 4x4 PNG, optionally with per-pixel alpha.
func buildPNG(t *testing.T, withAlpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			a := uint8(255)
			if withAlpha {
				a = uint8((x + y) * 32)
			}
			img.Set(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

// minimalTTF builds a minimal valid TrueType font with the tables required
// by the stamp font embedder (head, hhea, maxp, hmtx, cmap, name, post).
// It defines two glyphs: notdef and the letter "A" (GID 1, U+0041).
func minimalTTF(t *testing.T) []byte {
	t.Helper()

	// head table: 54 bytes, must set unitsPerEm at offset 18 and FontBBox
	// at offsets 36-44. indexToLocFormat at offset 50.
	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000) // version
	binary.BigEndian.PutUint16(head[18:], 1000)      // unitsPerEm
	binary.BigEndian.PutUint16(head[36:], 0)         // xMin
	binary.BigEndian.PutUint16(head[38:], 0)         // yMin
	binary.BigEndian.PutUint16(head[40:], 1000)      // xMax
	binary.BigEndian.PutUint16(head[42:], 1000)      // yMax
	binary.BigEndian.PutUint16(head[50:], 0)         // indexToLocFormat (short)

	// hhea table: ascent at offset 4, descent at offset 6,
	// numberOfHMetrics at offset 34. Minimum 36 bytes.
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[4:], 800) // ascent
	binary.BigEndian.PutUint16(hhea[6:], 200) // descent (as uint16; int16 logically negative)
	binary.BigEndian.PutUint16(hhea[34:], 2)  // numberOfHMetrics

	// maxp: 6 bytes header, numGlyphs at offset 4.
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:], 2)

	// hmtx: 2 glyphs × 4 bytes each.
	hmtx := make([]byte, 8)
	binary.BigEndian.PutUint16(hmtx[0:], 500) // gid 0 advance
	binary.BigEndian.PutUint16(hmtx[4:], 600) // gid 1 advance

	// cmap: format 4 subtable mapping U+0041 → GID 1.
	cmapSub := buildCmapFormat4TestOnly(map[uint16]uint16{0x0041: 1})
	cmap := buildCmapTestOnly(3, 1, cmapSub)

	// post: format 3 (no glyph names), minimum 32 bytes.
	post := make([]byte, 32)
	binary.BigEndian.PutUint32(post[0:], 0x00030000) // format 3.0

	// name: minimal table with no records.
	name := make([]byte, 6)
	binary.BigEndian.PutUint16(name[0:], 0) // format
	binary.BigEndian.PutUint16(name[2:], 0) // count
	binary.BigEndian.PutUint16(name[4:], 6) // stringOffset

	// loca: two entries (gid 0 and gid 1), both pointing to offset 0.
	loca := make([]byte, 6)

	// glyf: empty.
	glyf := []byte{}

	tables := map[string][]byte{
		"head": head,
		"hhea": hhea,
		"maxp": maxp,
		"hmtx": hmtx,
		"cmap": cmap,
		"post": post,
		"name": name,
		"loca": loca,
		"glyf": glyf,
	}
	return buildTableDirectoryTestOnly(tables)
}

func buildTableDirectoryTestOnly(tables map[string][]byte) []byte {
	numTables := len(tables)
	headerSize := 12 + numTables*16
	totalSize := headerSize
	for _, data := range tables {
		totalSize += len(data)
	}
	buf := make([]byte, totalSize)
	binary.BigEndian.PutUint32(buf[0:4], 0x00010000)
	binary.BigEndian.PutUint16(buf[4:6], uint16(numTables))

	offset := uint32(headerSize)
	i := 0
	for tag, data := range tables {
		recOff := 12 + i*16
		copy(buf[recOff:recOff+4], tag)
		binary.BigEndian.PutUint32(buf[recOff+8:recOff+12], offset)
		binary.BigEndian.PutUint32(buf[recOff+12:recOff+16], uint32(len(data)))
		copy(buf[offset:], data)
		offset += uint32(len(data))
		i++
	}
	return buf
}

func buildCmapFormat4TestOnly(mappings map[uint16]uint16) []byte {
	type segment struct {
		start, end uint16
		delta      int16
	}
	var segs []segment
	for charCode, gid := range mappings {
		segs = append(segs, segment{charCode, charCode, int16(gid) - int16(charCode)})
	}
	segs = append(segs, segment{0xFFFF, 0xFFFF, 1})

	segCount := len(segs)
	length := 14 + segCount*8 + 2
	data := make([]byte, length)
	binary.BigEndian.PutUint16(data[0:], 4)
	binary.BigEndian.PutUint16(data[2:], uint16(length))
	binary.BigEndian.PutUint16(data[6:], uint16(segCount*2))

	endOff := 14
	startOff := endOff + segCount*2 + 2
	deltaOff := startOff + segCount*2
	rangeOff := deltaOff + segCount*2
	for i, seg := range segs {
		binary.BigEndian.PutUint16(data[endOff+i*2:], seg.end)
		binary.BigEndian.PutUint16(data[startOff+i*2:], seg.start)
		binary.BigEndian.PutUint16(data[deltaOff+i*2:], uint16(seg.delta))
		binary.BigEndian.PutUint16(data[rangeOff+i*2:], 0)
	}
	return data
}

func buildCmapTestOnly(platformID, encodingID uint16, subtable []byte) []byte {
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:], 0)
	binary.BigEndian.PutUint16(header[2:], 1)
	binary.BigEndian.PutUint16(header[4:], platformID)
	binary.BigEndian.PutUint16(header[6:], encodingID)
	binary.BigEndian.PutUint32(header[8:], uint32(len(header)))
	return append(header, subtable...)
}

// pages12Fixture loads the 12-page corpus fixture. Skips the test when
// the file is not present (e.g. sparse checkout without test corpora).
func pages12Fixture(t *testing.T) []byte {
	t.Helper()
	abs, err := filepath.Abs("../../test/integration/testdata/litho/pages_12.pdf")
	if err != nil {
		t.Skipf("resolve fixture path: %v", err)
	}
	data, err := os.ReadFile(abs) //nolint:gosec // fixed test corpus path.
	if err != nil {
		t.Skipf("read fixture %s: %v", abs, err)
	}
	return data
}

// stripRandomID redacts the hex /ID arrays emitted by the trailer-id
// fixer. Every successful remediation rolls two fresh 16-byte random
// values; byte-level comparisons must excise them to test everything
// else for determinism.
func stripRandomID(pdf []byte) []byte {
	out := make([]byte, 0, len(pdf))
	for i := 0; i < len(pdf); {
		if pdf[i] == '<' && i+33 < len(pdf) && pdf[i+33] == '>' {
			hex := true
			for j := i + 1; j < i+33; j++ {
				c := pdf[j]
				if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')) {
					hex = false
					break
				}
			}
			if hex {
				out = append(out, []byte("<REDACTED_ID>")...)
				i += 34
				continue
			}
		}
		out = append(out, pdf[i])
		i++
	}
	return out
}

func TestEvaluateRegression(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		baseline integritySnap
		post     integritySnap
		tol      RegressionTolerance
		wantOK   bool
	}{
		{
			name:     "identical",
			baseline: integritySnap{pageCount: 3, textLen: 100, imageCount: 2},
			post:     integritySnap{pageCount: 3, textLen: 100, imageCount: 2},
			tol:      DefaultRegressionTolerance,
			wantOK:   true,
		},
		{
			name:     "page loss forbidden by default",
			baseline: integritySnap{pageCount: 3, textLen: 100},
			post:     integritySnap{pageCount: 2, textLen: 100},
			tol:      DefaultRegressionTolerance,
			wantOK:   false,
		},
		{
			name:     "page loss allowed when opted in",
			baseline: integritySnap{pageCount: 3, textLen: 100},
			post:     integritySnap{pageCount: 2, textLen: 100},
			tol:      RegressionTolerance{MaxTextDropRatio: 0.1, AllowPageLoss: true},
			wantOK:   true,
		},
		{
			name:     "text gone entirely",
			baseline: integritySnap{pageCount: 1, textLen: 100},
			post:     integritySnap{pageCount: 1, textLen: 0},
			tol:      DefaultRegressionTolerance,
			wantOK:   false,
		},
		{
			name:     "text drop within ratio",
			baseline: integritySnap{pageCount: 1, textLen: 1000},
			post:     integritySnap{pageCount: 1, textLen: 950},
			tol:      DefaultRegressionTolerance,
			wantOK:   true,
		},
		{
			name:     "text drop exceeds ratio",
			baseline: integritySnap{pageCount: 1, textLen: 1000},
			post:     integritySnap{pageCount: 1, textLen: 800},
			tol:      DefaultRegressionTolerance,
			wantOK:   false,
		},
		{
			name:     "zero baseline text accepts empty post",
			baseline: integritySnap{pageCount: 1, textLen: 0, imageCount: 0},
			post:     integritySnap{pageCount: 1, textLen: 0, imageCount: 0},
			tol:      RegressionTolerance{MaxTextDropRatio: 0.0, MaxImageDropRatio: 0.0},
			wantOK:   true,
		},
		{
			name:     "image drop within tolerance",
			baseline: integritySnap{pageCount: 1, textLen: 0, imageCount: 4},
			post:     integritySnap{pageCount: 1, textLen: 0, imageCount: 2},
			tol:      DefaultRegressionTolerance,
			wantOK:   true,
		},
		{
			name:     "image drop exceeds tolerance",
			baseline: integritySnap{pageCount: 1, textLen: 0, imageCount: 4},
			post:     integritySnap{pageCount: 1, textLen: 0, imageCount: 0},
			tol:      DefaultRegressionTolerance,
			wantOK:   false,
		},
		{
			name:     "strict tolerance catches any text drop",
			baseline: integritySnap{pageCount: 1, textLen: 100},
			post:     integritySnap{pageCount: 1, textLen: 99},
			tol:      RegressionTolerance{MaxTextDropRatio: 0.0, MaxImageDropRatio: 1.0},
			wantOK:   false,
		},
		{
			// Unreadable source: baseline is zero across the board. Every
			// check should skip so post content (legitimately non-zero)
			// does not register as a regression.
			name:     "zero baseline skips all checks",
			baseline: integritySnap{},
			post:     integritySnap{pageCount: 12, textLen: 4000, imageCount: 3},
			tol:      DefaultRegressionTolerance,
			wantOK:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := evaluateRegression(tc.baseline, tc.post, tc.tol)
			gotOK := reason == ""
			if gotOK != tc.wantOK {
				t.Errorf("ok=%v, reason=%q; want ok=%v", gotOK, reason, tc.wantOK)
			}
		})
	}
}

// TestWriter_RemediatePDFA_Determinism runs the same remediation many
// times on the same input and asserts every output parses cleanly with
// a stable page count. The predecessor of this test flaked roughly 1 in
// 10 runs because remediation's override map was applied twice during
// full rewrite (once through the copier, once through a queued mutation
// that stomped the copier's dst-namespace allocations).
func TestWriter_RemediatePDFA_Determinism(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := writerTestPDF()

	const runs = 50
	for i := range runs {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("run %d Open: %v", i, err)
		}
		w := NewWriter(r)
		_, err = w.RemediatePDFA(ctx, models.PDFA2b)
		if err != nil {
			r.Close()
			t.Fatalf("run %d RemediatePDFA: %v", i, err)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			r.Close()
			t.Fatalf("run %d WriteTo: %v", i, err)
		}
		r.Close()

		r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("run %d reopen: %v", i, err)
		}
		n, err := r2.PageCount(ctx)
		r2.Close()
		if err != nil {
			t.Fatalf("run %d PageCount: %v", i, err)
		}
		if n != 1 {
			t.Fatalf("run %d PageCount: got %d, want 1", i, n)
		}
	}
}

// TestWriter_RemediatePDFA_ByteDeterminism runs the full-rewrite path
// twice and asserts the two outputs are byte-identical apart from the
// random trailer /ID. A previous bug left copyDict traversing its input
// in map iteration order, which produced differently numbered dst
// objects on every run.
func TestWriter_RemediatePDFA_ByteDeterminism(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := writerTestPDF()

	render := func() []byte {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		w := NewWriter(r)
		if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}

	a := stripRandomID(render())
	b := stripRandomID(render())
	if !bytes.Equal(a, b) {
		t.Fatalf("full rewrite is non-deterministic (len a=%d b=%d)", len(a), len(b))
	}
}

// TestWriter_RemediatePDFA_IntegrityProbe_HappyPath confirms the new
// baseline probe and post probe do not introduce regressions on
// documents that never trigger content-tier fixers.
func TestWriter_RemediatePDFA_IntegrityProbe_HappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := writerTestPDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	report, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if len(report.RolledBack) != 0 {
		t.Errorf("unexpected rollback on structural-only fixture: %v", report.RolledBack)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
}

// TestRemediate_Pages12_PDFA4_then_PDFUA2 pipelines two profiles on the
// same source and asserts the second pass leaves no residual. Before
// the validator fix, PDF/A-4's required pdfaid:rev=2020 tripped the
// PDF/UA-2 rev-prefix check because the check matched any ":rev" local
// name regardless of namespace URI.
func TestRemediate_Pages12_PDFA4_then_PDFUA2(t *testing.T) {
	ctx := context.Background()
	data := pages12Fixture(t)

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.RemediatePDFA(ctx, models.PDFA4); err != nil {
		r.Close()
		t.Fatalf("RemediatePDFA: %v", err)
	}
	var a4 bytes.Buffer
	if err := w.WriteTo(ctx, &a4); err != nil {
		r.Close()
		t.Fatalf("WriteTo: %v", err)
	}
	r.Close()

	r2, err := Open(ctx, bytes.NewReader(a4.Bytes()), int64(a4.Len()))
	if err != nil {
		t.Fatalf("Open remediated: %v", err)
	}
	defer r2.Close()

	w2 := NewWriter(r2)
	report, err := w2.RemediatePDFUA(ctx, models.PDFUA2)
	if err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	if len(report.Residual) != 0 {
		for _, v := range report.Residual {
			t.Errorf("unexpected residual: %v", v)
		}
	}
}

// TestWriter_RemediatePDFA_Pages12_NoSilentBreakage is the motivating
// regression test. Applying PDF/A-4 with aggressive fixes to
// pages_12.pdf historically left the output unable to yield any
// extractable text. The safety net must surface that either as a
// successful remediation with text preserved (rollback may have kicked
// in) or as ErrRemediationBrokePDF. A successful call that returned no
// text is not acceptable.
func TestWriter_RemediatePDFA_Pages12_NoSilentBreakage(t *testing.T) {
	ctx := context.Background()
	data := pages12Fixture(t)

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Extraction errors (missing fonts, OCR-only pages) are acceptable
	// here; the test only needs whatever text the fixture actually
	// yields. An empty string is a valid baseline.
	baselineText, _ := r.ExtractAllText(ctx) //nolint:errcheck // baseline-only.
	baselinePages, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	t.Logf("baseline: %d pages, %d text bytes", baselinePages, len(baselineText))
	r.Close()

	r2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r2.Close()

	w := NewWriter(r2)
	report, remErr := w.RemediatePDFA(ctx, models.PDFA4, WithAggressiveFixes())
	if remErr != nil {
		if !errors.Is(remErr, ErrRemediationBrokePDF) {
			t.Fatalf("RemediatePDFA returned unexpected error: %v", remErr)
		}
		t.Logf("hard-failed as expected: %v", remErr)
		return
	}
	t.Logf("fixed: %d, rolled back: %d, residual: %d",
		len(report.Fixed), len(report.RolledBack), len(report.Residual))

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopen remediated: %v", err)
	}
	defer r3.Close()

	postPages, err := r3.PageCount(ctx)
	if err != nil {
		t.Fatalf("post PageCount: %v", err)
	}
	if postPages != baselinePages {
		t.Errorf("page count regressed: before=%d after=%d", baselinePages, postPages)
	}
	// Tolerate extraction errors on the post side: the test asserts
	// text is not silently wiped, an empty post string is a valid
	// comparison input.
	postText, _ := r3.ExtractAllText(ctx) //nolint:errcheck // post-check only.
	t.Logf("post: %d pages, %d text bytes", postPages, len(postText))

	if len(baselineText) > 0 && strings.TrimSpace(postText) == "" {
		t.Fatalf("silent text loss: baseline had %d bytes, post has none; rollback field=%v",
			len(baselineText), report.RolledBack)
	}
}

// TestWriter_RemediatePDFA_Pages12_StrictTolerance exercises the
// rollback branch: any text drop is treated as regression. The call
// must either return ErrRemediationBrokePDF or produce an output whose
// text length is within the strict bound after rolling back
// content-tier fixes.
func TestWriter_RemediatePDFA_Pages12_StrictTolerance(t *testing.T) {
	ctx := context.Background()
	data := pages12Fixture(t)

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// The test skips on empty baseline, so extraction failure is
	// treated the same as no text. The err is intentionally ignored.
	baselineText, _ := r.ExtractAllText(ctx) //nolint:errcheck // baseline-only.
	r.Close()
	if len(baselineText) == 0 {
		t.Skip("fixture has no extractable text; cannot exercise text-drop path")
	}

	r2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r2.Close()

	w := NewWriter(r2)
	report, remErr := w.RemediatePDFA(ctx, models.PDFA4,
		WithAggressiveFixes(),
		WithRegressionTolerance(RegressionTolerance{
			MaxTextDropRatio:  0.0,
			MaxImageDropRatio: 1.0,
		}),
	)
	if remErr != nil {
		if !errors.Is(remErr, ErrRemediationBrokePDF) {
			t.Fatalf("RemediatePDFA returned unexpected error: %v", remErr)
		}
		t.Logf("strict tolerance hard-failed as expected: %v", remErr)
		return
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r3.Close()
	// Extraction errors count as zero text for the comparison below.
	postText, _ := r3.ExtractAllText(ctx) //nolint:errcheck // post-check only.
	if len(postText) < len(baselineText) {
		t.Errorf("strict tolerance did not enforce text parity: before=%d after=%d rolled_back=%v",
			len(baselineText), len(postText), report.RolledBack)
	}
}

const facturXSampleCII = `<?xml version="1.0" encoding="UTF-8"?>
<rsm:CrossIndustryInvoice xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100" xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100" xmlns:udt="urn:un:unece:uncefact:data:standard:UnqualifiedDataType:100">
  <rsm:ExchangedDocumentContext>
    <ram:BusinessProcessSpecifiedDocumentContextParameter>
      <ram:ID>A1</ram:ID>
    </ram:BusinessProcessSpecifiedDocumentContextParameter>
  </rsm:ExchangedDocumentContext>
  <rsm:ExchangedDocument>
    <ram:ID>INV-001</ram:ID>
    <ram:IssueDateTime>
      <udt:DateTimeString format="102">20260517</udt:DateTimeString>
    </ram:IssueDateTime>
  </rsm:ExchangedDocument>
  <rsm:SupplyChainTradeTransaction>
    <ram:ApplicableHeaderTradeAgreement>
      <ram:SellerTradeParty>
        <ram:Name>Acme Co</ram:Name>
      </ram:SellerTradeParty>
    </ram:ApplicableHeaderTradeAgreement>
    <ram:ApplicableHeaderTradeSettlement>
      <ram:InvoiceCurrencyCode>EUR</ram:InvoiceCurrencyCode>
      <ram:SpecifiedTradeSettlementHeaderMonetarySummation>
        <ram:GrandTotalAmount currencyID="EUR">100.00</ram:GrandTotalAmount>
      </ram:SpecifiedTradeSettlementHeaderMonetarySummation>
    </ram:ApplicableHeaderTradeSettlement>
  </rsm:SupplyChainTradeTransaction>
</rsm:CrossIndustryInvoice>`

func TestWriter_RemediateFacturX_RoundTrip(t *testing.T) {
	ctx := context.Background()
	data := writerTestPDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	report, err := w.RemediateFacturX(ctx, FacturXConfig{
		InvoiceXML: []byte(facturXSampleCII),
		Level:      models.FacturXEN16931,
	})
	if err != nil {
		t.Fatalf("RemediateFacturX: %v", err)
	}
	if len(report.Fixed) == 0 {
		t.Error("expected at least one fix in report")
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()

	open, err := r2.XMPMetadata(ctx)
	if err != nil {
		t.Fatalf("XMPMetadata: %v", err)
	}
	if open == nil {
		t.Fatal("XMP stream missing from remediated output")
	}
	rc, err := open(ctx)
	if err != nil {
		t.Fatalf("opening XMP: %v", err)
	}
	xmpBytes, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("reading XMP: %v", err)
	}
	xmp := string(xmpBytes)
	for _, want := range []string{
		`xmlns:fx="urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#"`,
		"<fx:DocumentType>INVOICE</fx:DocumentType>",
		"<fx:DocumentFileName>factur-x.xml</fx:DocumentFileName>",
		"<fx:Version>1.0</fx:Version>",
		"<fx:ConformanceLevel>EN 16931</fx:ConformanceLevel>",
		"pdfaExtension:schemas",
	} {
		if !strings.Contains(xmp, want) {
			t.Errorf("XMP missing %q\n--- XMP ---\n%s", want, xmp)
		}
	}

	files, err := r2.EmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("EmbeddedFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("attachment count: got %d, want 1", len(files))
	}
	if files[0].Name != DefaultFacturXFileName {
		t.Errorf("attachment name: got %q, want %q", files[0].Name, DefaultFacturXFileName)
	}
	if files[0].MIMEType != "application/xml" {
		t.Errorf("attachment MIME: got %q, want %q", files[0].MIMEType, "application/xml")
	}
	rc2, err := files[0].Open(ctx)
	if err != nil {
		t.Fatalf("opening attachment: %v", err)
	}
	got, err := io.ReadAll(rc2)
	rc2.Close()
	if err != nil {
		t.Fatalf("reading attachment: %v", err)
	}
	if string(got) != facturXSampleCII {
		t.Errorf("attachment payload diverged from input invoice XML")
	}

	// The serializer hex-escapes the forward slash in "application/xml",
	// so the raw stream check uses the escaped form (#2F).
	rawPDF := out.String()
	if !strings.Contains(rawPDF, "/AFRelationship /Data") {
		t.Error("output PDF missing /AFRelationship /Data on filespec")
	}
	if !strings.Contains(rawPDF, "/Subtype /application#2Fxml") {
		t.Error("output PDF missing /Subtype /application/xml on embedded stream")
	}
}

func TestWriter_RemediateFacturX_XRechnungLevel(t *testing.T) {
	ctx := context.Background()
	data := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	if _, err := w.RemediateFacturX(ctx, FacturXConfig{
		InvoiceXML: []byte(facturXSampleCII),
		Level:      models.FacturXXRechnung,
	}); err != nil {
		t.Fatalf("RemediateFacturX: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()
	open, err := r2.XMPMetadata(ctx)
	if err != nil || open == nil {
		t.Fatalf("XMPMetadata: %v", err)
	}
	rc, err := open(ctx)
	if err != nil {
		t.Fatalf("opening XMP: %v", err)
	}
	xmpBytes, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("reading XMP: %v", err)
	}
	if !strings.Contains(string(xmpBytes), "<fx:ConformanceLevel>XRECHNUNG</fx:ConformanceLevel>") {
		t.Errorf("XMP missing XRECHNUNG conformance level:\n%s", xmpBytes)
	}
}

func TestWriter_RemediateFacturX_AppliesDefaults(t *testing.T) {
	ctx := context.Background()
	data := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	// Profile, FileName, DocumentType, Relationship omitted on purpose.
	if _, err := w.RemediateFacturX(ctx, FacturXConfig{
		InvoiceXML: []byte(facturXSampleCII),
		Level:      models.FacturXBasic,
	}); err != nil {
		t.Fatalf("RemediateFacturX: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	rawPDF := out.String()
	if !strings.Contains(rawPDF, DefaultFacturXFileName) {
		t.Errorf("expected default file name %q in output", DefaultFacturXFileName)
	}
	if !strings.Contains(rawPDF, "/AFRelationship /"+DefaultFacturXRelationship) {
		t.Errorf("expected default relationship %q in output", DefaultFacturXRelationship)
	}
}

func TestReader_ValidateFacturX_RoundTrip(t *testing.T) {
	// Writer.RemediateFacturX output must satisfy Reader.ValidateFacturX
	// without raising any hybrid-invoice (INVOICE.*) violations. The
	// PDF/A-3 archival base may still surface residual violations on
	// the minimal fixture; this test asserts only that no INVOICE rule
	// other than the PDF/A-3 base aggregator is reported.
	ctx := context.Background()
	data := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.RemediateFacturX(ctx, FacturXConfig{
		InvoiceXML: []byte(facturXSampleCII),
		Level:      models.FacturXEN16931,
	}); err != nil {
		t.Fatalf("RemediateFacturX: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()

	violations, err := r2.ValidateFacturX(ctx)
	if err != nil {
		t.Fatalf("ValidateFacturX: %v", err)
	}
	for _, v := range violations {
		msg := v.Error()
		if strings.Contains(msg, "clause INVOICE") &&
			!strings.Contains(msg, "PDF/A-3 violations on the archival base") {
			t.Errorf("unexpected hybrid-invoice violation: %v", v)
		}
		if strings.Contains(msg, "clause CII") {
			t.Errorf("unexpected CII structural violation: %v", v)
		}
	}
}

func TestReader_ValidateZUGFeRD_VersionMismatch(t *testing.T) {
	// A document produced as Factur-X (filename factur-x.xml, fx
	// namespace) must NOT validate as ZUGFeRD 1.0 or 2.0: the legacy
	// filename is absent so the variant-specific filespec check fires.
	ctx := context.Background()
	data := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.RemediateFacturX(ctx, FacturXConfig{
		InvoiceXML: []byte(facturXSampleCII),
		Level:      models.FacturXBasic,
	}); err != nil {
		t.Fatalf("RemediateFacturX: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()

	violations, err := r2.ValidateZUGFeRD(ctx, models.ZUGFeRD2)
	if err != nil {
		t.Fatalf("ValidateZUGFeRD(2.0): %v", err)
	}
	saw := false
	for _, v := range violations {
		if strings.Contains(v.Error(), `no filespec named "zugferd-invoice.xml"`) {
			saw = true
		}
	}
	if !saw {
		t.Error("expected hybrid-invoice filespec mismatch when auditing Factur-X file as ZUGFeRD 2.0")
	}
}

// facturXTestXSD is a deliberately minimal CII-shape XSD that only
// declares the document root in the canonical CII namespace. The
// integration test in [TestReader_ValidateFacturX_WithXSD] uses it
// to drive the XSD pass through the embedded factur-x.xml without
// pulling the full UN/CEFACT D16B hierarchy into the repository.
const facturXTestXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
           targetNamespace="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
           elementFormDefault="qualified">
  <xs:complexType name="CrossIndustryInvoiceType">
    <xs:sequence>
      <xs:any namespace="##any" processContents="skip" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
  </xs:complexType>
  <xs:element name="CrossIndustryInvoice" type="rsm:CrossIndustryInvoiceType"/>
</xs:schema>`

func TestReader_ValidateFacturX_WithXSD(t *testing.T) {
	// Round-trip: produce a Factur-X PDF, open the result with the
	// CII schema option set, validate, and confirm no CII_XSD
	// violations surface against a passing fixture. A wholly missing
	// schema set in earlier tests proves the XSD pass is gated on
	// the option.
	ctx := context.Background()
	data := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.RemediateFacturX(ctx, FacturXConfig{
		InvoiceXML: []byte(facturXSampleCII),
		Level:      models.FacturXEN16931,
	}); err != nil {
		t.Fatalf("RemediateFacturX: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	schemaFS := fstest.MapFS{"cii.xsd": &fstest.MapFile{Data: []byte(facturXTestXSD)}}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()),
		WithCIISchemas(schemaFS, "cii.xsd"))
	if err != nil {
		t.Fatalf("reopen with schema: %v", err)
	}
	defer r2.Close()

	violations, err := r2.ValidateFacturX(ctx)
	if err != nil {
		t.Fatalf("ValidateFacturX: %v", err)
	}
	for _, v := range violations {
		if strings.Contains(v.Error(), "clause CII_XSD") {
			t.Errorf("unexpected CII_XSD violation on passing fixture: %v", v)
		}
	}
}

func TestReader_ValidateFacturX_XSDBadSchema(t *testing.T) {
	ctx := context.Background()
	data := writerTestPDF()
	schemaFS := fstest.MapFS{"missing.xsd": &fstest.MapFile{Data: []byte("not an xsd")}}
	_, err := Open(ctx, bytes.NewReader(data), int64(len(data)),
		WithCIISchemas(schemaFS, "missing.xsd"))
	if err == nil {
		t.Error("expected error from Open with malformed XSD")
	}
}

func TestReader_ValidateZUGFeRD_BadVersion(t *testing.T) {
	ctx := context.Background()
	data := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	_, err = r.ValidateZUGFeRD(ctx, models.ZUGFeRDVersion("9.9"))
	if !errors.Is(err, ErrInvalidZUGFeRDVersion) {
		t.Errorf("err: got %v, want errors.Is(_, ErrInvalidZUGFeRDVersion)", err)
	}
}

func TestWriter_RemediateFacturX_RejectsBadConfig(t *testing.T) {
	ctx := context.Background()
	data := writerTestPDF()

	cases := []struct {
		name string
		cfg  FacturXConfig
		want error
	}{
		{
			name: "empty invoice",
			cfg:  FacturXConfig{Level: models.FacturXBasic},
			want: ErrEmptyInvoiceXML,
		},
		{
			name: "invalid level",
			cfg:  FacturXConfig{InvoiceXML: []byte("<x/>"), Level: "TURBO"},
			want: ErrInvalidFacturXLevel,
		},
		{
			name: "non-PDF/A-3 profile",
			cfg: FacturXConfig{
				InvoiceXML: []byte("<x/>"),
				Level:      models.FacturXBasic,
				Profile:    models.PDFA2b,
			},
			want: ErrInvalidFacturXProfile,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			w := NewWriter(r)
			_, err = w.RemediateFacturX(ctx, tc.cfg)
			if !errors.Is(err, tc.want) {
				t.Errorf("err: got %v, want errors.Is(_, %v)", err, tc.want)
			}
		})
	}
}
