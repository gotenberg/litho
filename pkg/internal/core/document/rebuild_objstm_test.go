package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// buildCompressedCatalogRebuildPDF builds a PDF whose /Root catalog (object 1)
// lives COMPRESSED inside object stream 8, with a deliberately broken startxref
// so the xref chain parse fails and litho falls back to the raw-scan rebuild.
// The rebuild finds only the uncompressed objects (pages 2, page 3, ObjStm 8),
// so without object-stream expansion the compressed catalog is unrecoverable
// and the page tree is lost. In real files a slightly-off startxref offset
// triggers the same rebuild.
//
// # Corpus evidence
//
//   - pdfjs issue11330, issue14269, issue15833: a slightly-off startxref offset
//     triggers the same rebuild.
func buildCompressedCatalogRebuildPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")

	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] >>\nendobj\n")

	// Object stream 8 holds the catalog (object 1) at index 0.
	data := "<< /Type /Catalog /Pages 2 0 R >>"
	header := "1 0 " // obj 1 at data offset 0
	objStm := header + data
	var comp bytes.Buffer
	zw := zlib.NewWriter(&comp)
	_, _ = zw.Write([]byte(objStm))
	_ = zw.Close()

	buf.WriteString(fmt.Sprintf("8 0 obj\n<< /Type /ObjStm /N 1 /First %d /Filter /FlateDecode /Length %d >>\nstream\n", len(header), comp.Len()))
	buf.Write(comp.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	// startxref 0 points at the %PDF header, not an xref table/stream: the chain
	// parse fails and the rebuild runs.
	buf.WriteString("startxref\n0\n%%EOF\n")
	return buf.Bytes()
}

// TestRebuild_RecoversCompressedCatalog pins that a raw-scan rebuild expands
// recovered object streams so a compressed catalog (and thus the page tree)
// survives an unusable xref chain.
func TestRebuild_RecoversCompressedCatalog(t *testing.T) {
	data := buildCompressedCatalogRebuildPDF()
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	e1, ok := doc.xrefEntries[1]
	if !ok || e1.Type != xref.XRefTypeCompressed {
		t.Fatalf("obj 1 entry = %+v (present=%v), want a compressed entry registered by object-stream expansion", e1, ok)
	}

	cat, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog after rebuild: %v (the compressed catalog in ObjStm 8 was not recovered)", err)
	}
	pages, err := ResolvePages(ctx, doc, cat)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) != 1 {
		t.Errorf("pages = %d, want 1", len(pages))
	}
}
