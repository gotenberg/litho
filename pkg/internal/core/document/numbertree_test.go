package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// buildNumberTreePDF constructs a minimal PDF holding a number tree rooted at
// object 4:
//
//	4: /Kids [5 0 R 6 0 R]
//	5: /Nums [0 (ZERO) 2 (TWO)]
//	6: /Nums [1 (ONE)]
//	7: /Nums [5 (FIVE)] /Kids [6 0 R]   (both entries on one node)
//
// The kids are indirect references on purpose: a /ParentTree in a real file
// reaches its leaves that way, and a fixture whose refs did not resolve would
// make every lookup return nil and every "not found" assertion pass for the
// wrong reason.
func buildNumberTreePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")

	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	off3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /StructParents 0 >>\nendobj\n")
	off4 := buf.Len()
	buf.WriteString("4 0 obj\n<< /Kids [5 0 R 6 0 R] >>\nendobj\n")
	off5 := buf.Len()
	buf.WriteString("5 0 obj\n<< /Nums [0 (ZERO) 2 (TWO)] >>\nendobj\n")
	off6 := buf.Len()
	buf.WriteString("6 0 obj\n<< /Nums [1 (ONE)] >>\nendobj\n")
	off7 := buf.Len()
	buf.WriteString("7 0 obj\n<< /Nums [5 (FIVE)] /Kids [6 0 R] >>\nendobj\n")

	xrefOff := buf.Len()
	buf.WriteString("xref\n0 8\n")
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range []int{off1, off2, off3, off4, off5, off6, off7} {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString("trailer\n<< /Size 8 /Root 1 0 R >>\n")
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOff))

	return buf.Bytes()
}

// buildCyclicNumberTreePDF roots a tree at object 4 whose /Kids names itself
// before a real leaf. A walk with no visited set fans out until it overflows.
func buildCyclicNumberTreePDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")

	off1 := buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	off2 := buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	off3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
	off4 := buf.Len()
	buf.WriteString("4 0 obj\n<< /Kids [4 0 R 5 0 R] >>\nendobj\n")
	off5 := buf.Len()
	buf.WriteString("5 0 obj\n<< /Nums [7 (SEVEN)] >>\nendobj\n")

	xrefOff := buf.Len()
	buf.WriteString("xref\n0 6\n")
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range []int{off1, off2, off3, off4, off5} {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\n")
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOff))

	return buf.Bytes()
}

func numberTreeDoc(t *testing.T, data []byte) *Document {
	t.Helper()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

func TestLookupNumberTree_FoundInFirstLeaf(t *testing.T) {
	// Positive control: if the fixture's indirect kids did not resolve, this
	// fails rather than letting the "not found" tests pass vacuously.
	doc := numberTreeDoc(t, buildNumberTreePDF())
	root := ast.PdfIndirectRef{ObjectNum: 4}

	got, err := LookupNumberTree(context.Background(), doc, root, 0)
	if err != nil {
		t.Fatalf("LookupNumberTree: %v", err)
	}
	if got != ast.PdfString("ZERO") {
		t.Errorf("key 0 = %v (%T), want ZERO", got, got)
	}
}

func TestLookupNumberTree_FoundInSecondLeaf(t *testing.T) {
	// Reached only by descending past the first leaf, so this pins the /Kids
	// walk rather than a lucky hit on the first child.
	doc := numberTreeDoc(t, buildNumberTreePDF())
	root := ast.PdfIndirectRef{ObjectNum: 4}

	got, err := LookupNumberTree(context.Background(), doc, root, 1)
	if err != nil {
		t.Fatalf("LookupNumberTree: %v", err)
	}
	if got != ast.PdfString("ONE") {
		t.Errorf("key 1 = %v (%T), want ONE", got, got)
	}
}

func TestLookupNumberTree_SecondPairInOneLeaf(t *testing.T) {
	// /Nums is alternating key/value, so the walk has to step two at a time.
	doc := numberTreeDoc(t, buildNumberTreePDF())
	root := ast.PdfIndirectRef{ObjectNum: 4}

	got, err := LookupNumberTree(context.Background(), doc, root, 2)
	if err != nil {
		t.Fatalf("LookupNumberTree: %v", err)
	}
	if got != ast.PdfString("TWO") {
		t.Errorf("key 2 = %v (%T), want TWO", got, got)
	}
}

func TestLookupNumberTree_AbsentKeyIsNotFound(t *testing.T) {
	doc := numberTreeDoc(t, buildNumberTreePDF())
	root := ast.PdfIndirectRef{ObjectNum: 4}

	got, err := LookupNumberTree(context.Background(), doc, root, 99)
	if err != nil {
		t.Fatalf("LookupNumberTree: %v", err)
	}
	if got != nil {
		t.Errorf("key 99 = %v, want nil", got)
	}
}

func TestLookupNumberTree_NumsWinsOverKidsOnTheSameNode(t *testing.T) {
	// Object 7 carries both. A malformed file can do this, and reading the
	// leaves first means a direct hit needs no descent.
	doc := numberTreeDoc(t, buildNumberTreePDF())
	node := ast.PdfIndirectRef{ObjectNum: 7}

	got, err := LookupNumberTree(context.Background(), doc, node, 5)
	if err != nil {
		t.Fatalf("LookupNumberTree: %v", err)
	}
	if got != ast.PdfString("FIVE") {
		t.Errorf("key 5 = %v (%T), want FIVE", got, got)
	}

	// The /Kids on that same node still has to be walked for a key /Nums does
	// not hold.
	got, err = LookupNumberTree(context.Background(), doc, node, 1)
	if err != nil {
		t.Fatalf("LookupNumberTree: %v", err)
	}
	if got != ast.PdfString("ONE") {
		t.Errorf("key 1 through the node's /Kids = %v, want ONE", got)
	}
}

func TestLookupNumberTree_CycleTerminates(t *testing.T) {
	// The root's /Kids names the root. Without the visited set this never
	// returns; with it, the absent key still ends as "not found".
	doc := numberTreeDoc(t, buildCyclicNumberTreePDF())
	root := ast.PdfIndirectRef{ObjectNum: 4}

	got, err := LookupNumberTree(context.Background(), doc, root, 99)
	if err != nil {
		t.Fatalf("LookupNumberTree: %v", err)
	}
	if got != nil {
		t.Errorf("key 99 = %v, want nil", got)
	}
}

func TestLookupNumberTree_CycleDoesNotBreakTheRealLeaf(t *testing.T) {
	// The guard must skip the repeat visit, not abandon the walk: the leaf
	// listed after the cyclic entry still has to be found.
	doc := numberTreeDoc(t, buildCyclicNumberTreePDF())
	root := ast.PdfIndirectRef{ObjectNum: 4}

	got, err := LookupNumberTree(context.Background(), doc, root, 7)
	if err != nil {
		t.Fatalf("LookupNumberTree: %v", err)
	}
	if got != ast.PdfString("SEVEN") {
		t.Errorf("key 7 = %v (%T), want SEVEN", got, got)
	}
}
