package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// buildNameTreePDF constructs a minimal PDF with a /Names -> /Dests name tree.
// The name tree maps:
//
//	"Chapter1" → [page1Ref /XYZ 0 100 0]
//	"Chapter2" → [page2Ref /XYZ 0 200 0]
//
// The tree has a root intermediate node with two leaf children (kids),
// exercising the /Kids + /Limits traversal logic.
func buildNameTreePDF() ([]byte, ast.PdfIndirectRef, ast.PdfIndirectRef) {
	var buf bytes.Buffer

	buf.WriteString("%PDF-1.4\n")

	// Object 1: Catalog.
	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Names 5 0 R >>\nendobj\n")

	// Object 2: Pages.
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>\nendobj\n")

	// Object 3: Page 1.
	off3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")

	// Object 4: Page 2.
	off4 := buf.Len()
	buf.WriteString("4 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")

	// Object 5: /Names dictionary containing /Dests tree root.
	off5 := buf.Len()
	buf.WriteString("5 0 obj\n<< /Dests 6 0 R >>\nendobj\n")

	// Object 6: Name tree root (intermediate node with /Kids).
	off6 := buf.Len()
	buf.WriteString("6 0 obj\n<< /Kids [7 0 R 8 0 R] >>\nendobj\n")

	// Object 7: Leaf node for "Chapter1".
	off7 := buf.Len()
	buf.WriteString("7 0 obj\n<< /Limits [(Chapter1) (Chapter1)] /Names [(Chapter1) [3 0 R /XYZ 0 100 0]] >>\nendobj\n")

	// Object 8: Leaf node for "Chapter2".
	off8 := buf.Len()
	buf.WriteString("8 0 obj\n<< /Limits [(Chapter2) (Chapter2)] /Names [(Chapter2) [4 0 R /XYZ 0 200 0]] >>\nendobj\n")

	// XRef table.
	xrefOff := buf.Len()
	buf.WriteString("xref\n0 9\n")
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range []int{off1, off2, off3, off4, off5, off6, off7, off8} {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString("trailer\n<< /Size 9 /Root 1 0 R >>\n")
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOff))

	page1Ref := ast.PdfIndirectRef{ObjectNum: 3, GenNum: 0}
	page2Ref := ast.PdfIndirectRef{ObjectNum: 4, GenNum: 0}
	return buf.Bytes(), page1Ref, page2Ref
}

func TestResolveNamedDest_FoundInNameTree(t *testing.T) {
	pdfData, page1Ref, _ := buildNameTreePDF()

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	destArr, err := ResolveNamedDest(context.Background(), doc, "Chapter1")
	if err != nil {
		t.Fatalf("ResolveNamedDest: %v", err)
	}
	if destArr == nil {
		t.Fatal("expected destination array, got nil")
	}
	if len(destArr) < 1 {
		t.Fatal("destination array is empty")
	}
	ref, ok := destArr[0].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("expected PdfIndirectRef, got %T", destArr[0])
	}
	if ref != page1Ref {
		t.Errorf("expected page ref %v, got %v", page1Ref, ref)
	}
}

func TestResolveNamedDest_FoundSecondLeaf(t *testing.T) {
	pdfData, _, page2Ref := buildNameTreePDF()

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	destArr, err := ResolveNamedDest(context.Background(), doc, "Chapter2")
	if err != nil {
		t.Fatalf("ResolveNamedDest: %v", err)
	}
	if destArr == nil {
		t.Fatal("expected destination array for Chapter2, got nil")
	}
	ref, ok := destArr[0].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("expected PdfIndirectRef, got %T", destArr[0])
	}
	if ref != page2Ref {
		t.Errorf("expected page ref %v, got %v", page2Ref, ref)
	}
}

func TestResolveNamedDest_NotFound(t *testing.T) {
	pdfData, _, _ := buildNameTreePDF()

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	destArr, err := ResolveNamedDest(context.Background(), doc, "NonExistent")
	if err != nil {
		t.Fatalf("ResolveNamedDest: %v", err)
	}
	if destArr != nil {
		t.Errorf("expected nil for non-existent name, got %v", destArr)
	}
}

func TestResolveNamedDest_LimitsSkipsBranch(t *testing.T) {
	pdfData, _, _ := buildNameTreePDF()

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	// "AAA" sorts before "Chapter1", so /Limits pruning should skip both kids.
	destArr, err := ResolveNamedDest(context.Background(), doc, "AAA")
	if err != nil {
		t.Fatalf("ResolveNamedDest: %v", err)
	}
	if destArr != nil {
		t.Errorf("expected nil for out-of-limits name, got %v", destArr)
	}
}

func TestResolveNamedDest_NoNamesDict(t *testing.T) {
	// Minimal PDF without /Names in catalog.
	data := minimalPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	destArr, err := ResolveNamedDest(context.Background(), doc, "Anything")
	if err != nil {
		t.Fatalf("ResolveNamedDest: %v", err)
	}
	if destArr != nil {
		t.Errorf("expected nil when no /Names dict exists, got %v", destArr)
	}
}

// buildLegacyDestsPDF builds a PDF using the legacy /Dests dictionary (PDF 1.1).
func buildLegacyDestsPDF() ([]byte, ast.PdfIndirectRef) {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.1\n")

	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Dests 4 0 R >>\nendobj\n")

	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	off3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")

	off4 := buf.Len()
	buf.WriteString("4 0 obj\n<< /Intro [3 0 R /Fit] >>\nendobj\n")

	xrefOff := buf.Len()
	buf.WriteString("xref\n0 5\n")
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range []int{off1, off2, off3, off4} {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString("trailer\n<< /Size 5 /Root 1 0 R >>\n")
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOff))

	return buf.Bytes(), ast.PdfIndirectRef{ObjectNum: 3, GenNum: 0}
}

func TestResolveNamedDest_LegacyDestsDict(t *testing.T) {
	pdfData, pageRef := buildLegacyDestsPDF()

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	destArr, err := ResolveNamedDest(context.Background(), doc, "Intro")
	if err != nil {
		t.Fatalf("ResolveNamedDest: %v", err)
	}
	if destArr == nil {
		t.Fatal("expected destination for legacy /Dests lookup")
	}
	ref, ok := destArr[0].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("expected PdfIndirectRef, got %T", destArr[0])
	}
	if ref != pageRef {
		t.Errorf("expected %v, got %v", pageRef, ref)
	}
}

func TestSearchNameTree_DirectUnit(t *testing.T) {
	// Unit test searchNameTree with in-memory AST objects (no PDF file).
	// Simulate a flat leaf node.
	doc := &Document{
		Index:       xref.Index{Entries: make(map[int]xref.XRefEntry)},
		ObjectCache: make(map[int]ast.PdfObject),
		resolving:   make(map[int]bool),
	}

	pageRef := ast.PdfIndirectRef{ObjectNum: 10, GenNum: 0}
	leafNode := ast.PdfDict{
		"Names": ast.PdfArray{
			ast.PdfString("dest1"),
			ast.PdfArray{pageRef, ast.PdfName("Fit")},
			ast.PdfString("dest2"),
			ast.PdfArray{pageRef, ast.PdfName("XYZ")},
		},
	}

	result, err := searchNameTree(context.Background(), doc, leafNode, "dest1")
	if err != nil {
		t.Fatalf("searchNameTree: %v", err)
	}
	if result == nil {
		t.Fatal("expected result for dest1")
	}
	if len(result) < 1 {
		t.Fatal("result array is empty")
	}

	result2, err := searchNameTree(context.Background(), doc, leafNode, "dest2")
	if err != nil {
		t.Fatal(err)
	}
	if result2 == nil {
		t.Fatal("expected result for dest2")
	}

	result3, err := searchNameTree(context.Background(), doc, leafNode, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if result3 != nil {
		t.Errorf("expected nil for missing key, got %v", result3)
	}
}
