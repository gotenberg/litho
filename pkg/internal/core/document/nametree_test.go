package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

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

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
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

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
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

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
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

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
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
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
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

	doc, err := NewDocument(context.Background(), bytes.NewReader(pdfData), int64(len(pdfData)), nil)
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
		xrefEntries: make(map[int]xref.XRefEntry),
		objectCache: make(map[int]ast.PdfObject),
		fontCache:   make(map[int]any),
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

// TestSearchNameTree_ByteExactKey pins the contract that name tree
// lookups use the raw byte string as the key. Producers disambiguate
// two destinations sharing a visible title by a trailing 0x01 in the
// named destination identifier. Routing the key through
// [DecodePdfString] would drop that byte (it is undefined in
// PDFDocEncoding) and the second lookup would hit the first
// destination's page.
//
// # Corpus evidence
//
//   - pdfcpu bookmarkDuplicates.pdf: two destinations sharing the visible
//     title "kid1" are disambiguated by a trailing 0x01 in the named
//     destination identifier.
func TestSearchNameTree_ByteExactKey(t *testing.T) {
	doc := &Document{
		xrefEntries: make(map[int]xref.XRefEntry),
		objectCache: make(map[int]ast.PdfObject),
		fontCache:   make(map[int]any),
	}

	pageA := ast.PdfIndirectRef{ObjectNum: 10, GenNum: 0}
	pageB := ast.PdfIndirectRef{ObjectNum: 20, GenNum: 0}
	leafNode := ast.PdfDict{
		"Names": ast.PdfArray{
			ast.PdfString("kid1"),
			ast.PdfArray{pageA, ast.PdfName("Fit")},
			ast.PdfString("kid1\x01"),
			ast.PdfArray{pageB, ast.PdfName("Fit")},
		},
	}

	cases := []struct {
		key  string
		want ast.PdfIndirectRef
	}{
		{"kid1", pageA},
		{"kid1\x01", pageB},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("key=%q", c.key), func(t *testing.T) {
			result, err := searchNameTree(context.Background(), doc, leafNode, c.key)
			if err != nil {
				t.Fatalf("searchNameTree: %v", err)
			}
			if result == nil {
				t.Fatal("expected result, got nil")
			}
			ref, ok := result[0].(ast.PdfIndirectRef)
			if !ok {
				t.Fatalf("expected PdfIndirectRef, got %T", result[0])
			}
			if ref != c.want {
				t.Errorf("got %v, want %v", ref, c.want)
			}
		})
	}
}

func TestSearchNameTree_DepthCapBreaksCycle(t *testing.T) {
	doc := &Document{
		xrefEntries: make(map[int]xref.XRefEntry),
		objectCache: make(map[int]ast.PdfObject),
		fontCache:   make(map[int]any),
	}

	// PdfDict is a map (reference type), so wrapping the same dict in
	// its own Kids array creates a true self-cycle. Without the depth
	// cap searchNameTree would recurse until the runtime's stack
	// overflows.
	cyclic := ast.PdfDict{}
	cyclic["Kids"] = ast.PdfArray{cyclic}

	result, err := searchNameTree(context.Background(), doc, cyclic, "anything")
	if err != nil {
		t.Fatalf("searchNameTree on cycle: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil on cyclic name tree, got %v", result)
	}
}

func TestSearchNameTree_RespectsContextCancel(t *testing.T) {
	doc := &Document{
		xrefEntries: make(map[int]xref.XRefEntry),
		objectCache: make(map[int]ast.PdfObject),
		fontCache:   make(map[int]any),
	}

	leafNode := ast.PdfDict{
		"Names": ast.PdfArray{
			ast.PdfString("dest1"),
			ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 10}, ast.PdfName("Fit")},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := searchNameTree(ctx, doc, leafNode, "dest1"); err == nil {
		t.Fatal("expected ctx.Err() when called with cancelled context")
	}
}

// TestResolveNamedDest_FanoutBomb pins that a name tree whose every node lists
// the same /Kids child twice (with no pruning /Limits) cannot fan out 2^depth
// when the searched key is absent. Without a visited-set the search explored
// every path and hung; the guard bounds it to one visit per node.
func TestResolveNamedDest_FanoutBomb(t *testing.T) {
	t.Parallel()

	const levels = 60
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offs := []int{}
	writeObj := func(body string) {
		offs = append(offs, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", len(offs), body)
	}
	writeObj("<< /Type /Catalog /Pages 2 0 R /Names << /Dests 5 0 R >> >>")
	writeObj("<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj("<< /Type /Page /Parent 2 0 R >>")
	writeObj("<< /Length 0 >>\nstream\n\nendstream")
	for i := range levels {
		objn := 5 + i
		child := objn + 1
		if i == levels-1 {
			writeObj("<< /Names [(zzz) [3 0 R /Fit]] >>")
			continue
		}
		writeObj(fmt.Sprintf("<< /Kids [%d 0 R %d 0 R] >>", child, child))
	}
	xrefOff := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, xrefOff)

	doc, err := NewDocument(context.Background(), bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_, _ = ResolveNamedDest(context.Background(), doc, "missing-key")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("ResolveNamedDest did not complete: name-tree fan-out not bounded")
	}
}
