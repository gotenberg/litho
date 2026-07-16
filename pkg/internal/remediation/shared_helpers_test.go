package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// newTestDoc parses the given byte slice as a PDF and returns a document
// plus a fresh object allocator sized for the document's xref. It is the
// standard entry point for unit-testing a single fixer in isolation. The
// ctx argument uses [context.Background] and tests should override with
// a cancel context only when explicitly exercising cancellation paths.
func newTestDoc(t *testing.T, data []byte) (*document.Document, *writer.ObjAllocator) {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	maxObj := 0
	for objNum := range doc.Index.Entries {
		if objNum > maxObj {
			maxObj = objNum
		}
	}
	return doc, writer.NewObjAllocator(maxObj)
}

// pendingDict returns the allocator's pending dict at the given object
// number or fails the test. Used to verify a fixer wrote the expected
// mutation.
func pendingDict(t *testing.T, alloc *writer.ObjAllocator, objNum int) ast.PdfDict {
	t.Helper()
	obj, ok := alloc.Pending()[objNum]
	if !ok {
		t.Fatalf("expected pending entry for object %d", objNum)
	}
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d: got %T, want ast.PdfDict", objNum, obj)
	}
	return dict
}

// minimalCatalogPDF returns a minimal single-page PDF whose catalog
// contains only the given extra key/value pairs in addition to /Type and
// /Pages. This is the standard fixture builder for catalog-touching
// fixers.
//
// The resulting PDF has:
//   - 1 0 obj → Catalog (with extras)
//   - 2 0 obj → Pages
//   - 3 0 obj → Page
//
// Callers should not rely on specific xref offsets beyond the number of
// objects.
func minimalCatalogPDF(extras string) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R"
	if extras != "" {
		catalog += " " + extras
	}
	catalog += " >>"

	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n" + catalog + "\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000100 00000 n \r\n" +
			"0000000160 00000 n \r\n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\n" +
			"startxref\n250\n%%EOF\n")
}

// rootlessCatalogPDF returns the same single-page document as
// [minimalCatalogPDF] but with a trailer that omits /Root, mirroring a damaged
// incremental update whose active trailer lost the catalog pointer. The reader
// recovers the catalog (1 0 obj is a /Type /Catalog with /Pages), so a fixer
// must recover it the same way instead of aborting on the missing /Root.
func rootlessCatalogPDF(extras string) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R"
	if extras != "" {
		catalog += " " + extras
	}
	catalog += " >>"

	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n" + catalog + "\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000100 00000 n \r\n" +
			"0000000160 00000 n \r\n" +
			"trailer\n<< /Size 4 >>\n" +
			"startxref\n250\n%%EOF\n")
}
