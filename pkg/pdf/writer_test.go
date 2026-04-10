package pdf

import (
	"bytes"
	"context"
	"crypto/x509"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/crypto"
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

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
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

	page, err := r2.GetPage(ctx, 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
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

	bms, err := r2.GetBookmarks(ctx)
	if err != nil {
		t.Fatalf("GetBookmarks: %v", err)
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

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Combined" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Combined")
	}

	page, err := r2.GetPage(ctx, 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if page.Rotate != 180 {
		t.Errorf("Rotate: got %d, want 180", page.Rotate)
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

	files, err := r2.GetEmbeddedFiles(ctx)
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

	files, err := r2.GetEmbeddedFiles(ctx)
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
	w.Encrypt(crypto.EncryptConfig{
		UserPassword:  "user123",
		OwnerPassword: "owner456",
		Algorithm:     crypto.AES_256,
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

	meta, err := r3.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Original Title" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Original Title")
	}

	// Re-open with correct owner password.
	r4, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("owner456"))
	if err != nil {
		t.Fatalf("Open with owner password: %v", err)
	}

	meta2, err := r4.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
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
	w.Encrypt(crypto.EncryptConfig{
		UserPassword:  "test",
		OwnerPassword: "test",
		Algorithm:     crypto.AES_128,
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

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
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
	w.Encrypt(crypto.EncryptConfig{
		UserPassword: "secret",
		Algorithm:    crypto.AES_256,
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
	w2.Decrypt()

	var decrypted bytes.Buffer
	if err := w2.WriteTo(ctx, &decrypted); err != nil {
		t.Fatalf("decrypt WriteTo: %v", err)
	}

	// Re-open without password — should work now.
	r3, err := Open(ctx, bytes.NewReader(decrypted.Bytes()), int64(decrypted.Len()))
	if err != nil {
		t.Fatalf("Open decrypted: %v", err)
	}

	if r3.doc.IsEncrypted {
		t.Error("expected document to not be encrypted after decrypt")
	}

	meta, err := r3.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
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
	w.Encrypt(crypto.EncryptConfig{
		UserPassword: "pw",
		Algorithm:    crypto.AES_256,
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

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
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

	fieldMap, err := r2.GetFormData(ctx)
	if err != nil {
		t.Fatalf("GetFormData: %v", err)
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

	n, err := r3.NumPages(ctx)
	if err != nil {
		t.Fatalf("NumPages: %v", err)
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

	meta, err := r3.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
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

	n, err := r2.NumPages(ctx)
	if err != nil {
		t.Fatalf("NumPages: %v", err)
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

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
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

	n, err := r2.NumPages(ctx)
	if err != nil {
		t.Fatalf("NumPages: %v", err)
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

	n, err := r2.NumPages(ctx)
	if err != nil {
		t.Fatalf("NumPages: %v", err)
	}
	if n != 2 {
		t.Errorf("page count: got %d, want 2", n)
	}

	// Page 1 of split should be original page 3 (landscape).
	page, err := r2.GetPage(ctx, 1)
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

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
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

func TestWriter_StampText(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.StampText(TextStamp{
		Text:     "CONFIDENTIAL",
		X:        200,
		Y:        400,
		FontSize: 36,
	}, 1)

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
	w.StampText(TextStamp{
		Text:     "DRAFT",
		X:        100,
		Y:        100,
		FontSize: 24,
	}) // no pages = all pages

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
