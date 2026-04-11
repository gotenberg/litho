package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// minimalPDFNoStructTree builds a valid single-page PDF with a Catalog that
// does not carry a /StructTreeRoot entry. Used as the input fixture for the
// [fixStructTree] fixer.
func minimalPDFNoStructTree() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\n" +
			"startxref\n198\n%%EOF\n")
}

func TestFixStructTree_AddsMinimalStructureTree(t *testing.T) {
	ctx := context.Background()
	data := minimalPDFNoStructTree()

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	alloc := writer.NewObjAllocator(3) // xref has objects 1..3

	descs, err := fixStructTree(ctx, alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %d", len(descs))
	}

	// The fixer must have written a new catalog referencing a new
	// StructTreeRoot object.
	pending := alloc.Pending()
	newCatalogObj, ok := pending[1]
	if !ok {
		t.Fatal("expected pending entry for object 1 (catalog)")
	}
	newCatalog, ok := newCatalogObj.(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog: got %T, want ast.PdfDict", newCatalogObj)
	}
	stRef, ok := newCatalog["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("catalog /StructTreeRoot: got %T, want ast.PdfIndirectRef", newCatalog["StructTreeRoot"])
	}

	// Follow the /StructTreeRoot reference.
	stObj, ok := pending[stRef.ObjectNum]
	if !ok {
		t.Fatalf("expected pending entry for StructTreeRoot at obj %d", stRef.ObjectNum)
	}
	stDict, ok := stObj.(ast.PdfDict)
	if !ok {
		t.Fatalf("StructTreeRoot: got %T, want ast.PdfDict", stObj)
	}
	if stDict["Type"] != ast.PdfName("StructTreeRoot") {
		t.Errorf("StructTreeRoot /Type: got %v, want StructTreeRoot", stDict["Type"])
	}

	// The tree root's /K points at the document element.
	docElemRef, ok := stDict["K"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("StructTreeRoot /K: got %T, want ast.PdfIndirectRef", stDict["K"])
	}
	docElemObj, ok := pending[docElemRef.ObjectNum]
	if !ok {
		t.Fatalf("expected pending entry for document element at obj %d", docElemRef.ObjectNum)
	}
	docElem, ok := docElemObj.(ast.PdfDict)
	if !ok {
		t.Fatalf("document element: got %T, want ast.PdfDict", docElemObj)
	}
	if docElem["Type"] != ast.PdfName("StructElem") {
		t.Errorf("document element /Type: got %v, want StructElem", docElem["Type"])
	}
	if docElem["S"] != ast.PdfName("Document") {
		t.Errorf("document element /S: got %v, want Document", docElem["S"])
	}

	// --- Regression guard for the ParentTreeRoot bug ---
	//
	// Per ISO 32000-1 §14.7.2 Table 323, a structure element's parent
	// pointer is named /Parent. The fixer used to incorrectly set
	// /ParentTreeRoot instead, producing an invalid structure tree.
	parentRef, ok := docElem["Parent"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("document element /Parent: got %T (%v), want ast.PdfIndirectRef",
			docElem["Parent"], docElem["Parent"])
	}
	if parentRef != stRef {
		t.Errorf("document element /Parent: got obj %d, want tree root obj %d",
			parentRef.ObjectNum, stRef.ObjectNum)
	}
	if _, hasBug := docElem["ParentTreeRoot"]; hasBug {
		t.Error("document element should not have /ParentTreeRoot key")
	}
}

func TestFixStructTree_SkipsIfAlreadyPresent(t *testing.T) {
	ctx := context.Background()
	// A catalog that already has /StructTreeRoot should be left alone.
	data := []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"4 0 obj\n<< /Type /StructTreeRoot >>\nendobj\n" +
			"xref\n0 5\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000081 00000 n \r\n" +
			"0000000138 00000 n \r\n" +
			"0000000221 00000 n \r\n" +
			"trailer\n<< /Size 5 /Root 1 0 R >>\n" +
			"startxref\n264\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(4)

	descs, err := fixStructTree(ctx, alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixStructTree: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("expected no descriptions on skip, got %v", descs)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("expected no pending allocations on skip, got %d", len(alloc.Pending()))
	}
}
