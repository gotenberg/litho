package document

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// minimalPDF builds a valid-ish PDF with a correct xref table.
func minimalPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
			"xref\n0 3\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n" +
			"trailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n109\n%%EOF\n")
}

// corruptedPDF returns a PDF where the startxref offset is wrong, but the
// object headers and trailer are still present in the file.
func corruptedPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
			"xref\n0 3\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n" +
			"trailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n99999\n%%EOF\n")
}

// noXRefPDF returns a PDF with no xref table at all, just objects and a trailer.
func noXRefPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
			"trailer\n<< /Size 3 /Root 1 0 R >>\n%%EOF\n")
}

// missingStartXRefPDF returns a PDF where startxref keyword is absent entirely.
func missingStartXRefPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
			"trailer\n<< /Size 3 /Root 1 0 R >>\n%%EOF\n")
}

func TestNewDocument_ValidXRef(t *testing.T) {
	data := minimalPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument failed on valid PDF: %v", err)
	}
	if doc.Rebuilt {
		t.Error("expected Rebuilt=false for valid PDF")
	}
	if len(doc.Index.Entries) == 0 {
		t.Error("expected non-empty xref entries")
	}
}

func TestNewDocument_CorruptedXRef_Rebuilds(t *testing.T) {
	data := corruptedPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument failed on corrupted PDF: %v", err)
	}
	if !doc.Rebuilt {
		t.Error("expected Rebuilt=true for corrupted PDF")
	}

	// Should have found both objects.
	if _, ok := doc.Index.Entries[1]; !ok {
		t.Error("rebuilt xref missing object 1")
	}
	if _, ok := doc.Index.Entries[2]; !ok {
		t.Error("rebuilt xref missing object 2")
	}
	if doc.Index.Trailer == nil {
		t.Error("rebuilt xref missing trailer")
	}
}

func TestNewDocument_MissingStartXRef_Rebuilds(t *testing.T) {
	data := missingStartXRefPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument failed: %v", err)
	}
	if !doc.Rebuilt {
		t.Error("expected Rebuilt=true when startxref is missing")
	}
	if doc.Index.Trailer == nil {
		t.Error("expected trailer to be found during rebuild")
	}
}

func TestNewDocument_NoXRef_Rebuilds(t *testing.T) {
	data := noXRefPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument failed: %v", err)
	}
	if !doc.Rebuilt {
		t.Error("expected Rebuilt=true for PDF without xref table")
	}
	if _, ok := doc.Index.Entries[1]; !ok {
		t.Error("rebuilt xref missing object 1")
	}
}

func TestRebuildXRef_ObjectOffsets(t *testing.T) {
	data := corruptedPDF()
	doc := &Document{
		r:     bytes.NewReader(data),
		size:  int64(len(data)),
		Index: xref.Index{Entries: make(map[int]xref.XRefEntry)},
	}

	if err := doc.rebuildXRef(context.Background()); err != nil {
		t.Fatalf("rebuildXRef failed: %v", err)
	}

	// Verify offsets point to valid "N G obj" locations.
	for objNum, entry := range doc.Index.Entries {
		if entry.Type != 1 {
			continue
		}
		// Read a few bytes at the offset and verify it starts with "N G obj".
		buf := make([]byte, 20)
		_, err := doc.r.ReadAt(buf, entry.Offset)
		if err != nil {
			t.Errorf("object %d: failed to read at offset %d: %v", objNum, entry.Offset, err)
			continue
		}
		if !bytes.Contains(buf, []byte("obj")) {
			t.Errorf("object %d: offset %d does not point to 'obj' header: %q", objNum, entry.Offset, buf)
		}
	}
}

func TestRebuildXRef_TrailerHasRoot(t *testing.T) {
	data := corruptedPDF()
	doc := &Document{
		r:     bytes.NewReader(data),
		size:  int64(len(data)),
		Index: xref.Index{Entries: make(map[int]xref.XRefEntry)},
	}

	if err := doc.rebuildXRef(context.Background()); err != nil {
		t.Fatalf("rebuildXRef failed: %v", err)
	}

	if doc.Index.Trailer == nil {
		t.Fatal("expected trailer dictionary")
	}
	if _, ok := doc.Index.Trailer["Root"]; !ok {
		t.Error("trailer missing /Root entry")
	}
	if _, ok := doc.Index.Trailer["Size"]; !ok {
		t.Error("trailer missing /Size entry")
	}
}

func TestRebuildXRef_NoObjects(t *testing.T) {
	// A file with just a header and no objects should fail rebuild.
	data := []byte("%PDF-1.4\n%%EOF\n")
	doc := &Document{
		r:     bytes.NewReader(data),
		size:  int64(len(data)),
		Index: xref.Index{Entries: make(map[int]xref.XRefEntry)},
	}

	err := doc.rebuildXRef(context.Background())
	if err == nil {
		t.Fatal("expected error for empty PDF")
	}
	if !errors.Is(err, ErrDamagedXRef) {
		t.Errorf("expected ErrDamagedXRef, got: %v", err)
	}
}

func TestRebuildXRef_NoTrailer(t *testing.T) {
	// Objects exist but no trailer keyword. Rebuild synthesizes a
	// minimal trailer so the catalog-recovery path can still run.
	data := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")
	doc := &Document{
		r:     bytes.NewReader(data),
		size:  int64(len(data)),
		Index: xref.Index{Entries: make(map[int]xref.XRefEntry)},
	}

	if err := doc.rebuildXRef(context.Background()); err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if _, ok := doc.Index.Entries[1]; !ok {
		t.Fatal("expected object 1 in rebuilt entries")
	}
	if doc.Index.Trailer == nil {
		t.Fatal("expected a synthesized trailer")
	}
	if _, hasRoot := doc.Index.Trailer["Root"]; hasRoot {
		t.Fatal("synthesized trailer must omit /Root so recovery kicks in")
	}
	if size, ok := doc.Index.Trailer["Size"].(ast.PdfInt); !ok || size != 2 {
		t.Fatalf("expected /Size=2, got %v", doc.Index.Trailer["Size"])
	}
}

func TestNewDocument_TotallyBroken(t *testing.T) {
	// No header: should fail with ErrMissingHeader, not attempt rebuild.
	data := []byte("this is not a PDF")
	_, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if !errors.Is(err, ErrMissingHeader) {
		t.Errorf("expected ErrMissingHeader, got: %v", err)
	}
}

func TestNewDocument_HeaderAfterPrefix(t *testing.T) {
	// PDF spec puts %PDF- on the first line, but Acrobat (and litho)
	// tolerate a leading prefix up to headerScanLimit bytes. Build a
	// minimal one-page document with a 700-byte explanatory prefix to
	// confirm the version is still extracted from the right offset.
	prefix := bytes.Repeat([]byte("# explanatory prefix\n"), 35)
	if len(prefix) < 600 {
		t.Fatalf("test prefix too short: %d", len(prefix))
	}
	body := minimalPDF()
	data := append(append([]byte{}, prefix...), body...)

	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument with prefix: %v", err)
	}
	if doc.Version == "" {
		t.Errorf("expected non-empty Version after header at offset %d", len(prefix))
	}
}

func TestNewDocument_HeaderBeyondScanLimit(t *testing.T) {
	// A header buried past headerScanLimit must still be rejected;
	// otherwise litho would tolerate documents whose body is too far
	// from the start to safely cache.
	prefix := bytes.Repeat([]byte("x"), headerScanLimit+1)
	body := minimalPDF()
	data := append(append([]byte{}, prefix...), body...)

	_, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if !errors.Is(err, ErrMissingHeader) {
		t.Errorf("expected ErrMissingHeader for header beyond scan limit, got: %v", err)
	}
}
