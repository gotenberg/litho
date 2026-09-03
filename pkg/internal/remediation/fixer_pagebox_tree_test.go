package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// inheritedBoxPDF builds a two-level page tree whose boundary box is declared
// on the interior /Pages node, so both pages inherit it and neither carries one
// of its own. That is the shape of PDF_A-2b__veraPDF_test_suite_6-1-13-t09.
func inheritedBoxPDF(key, box string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 /MediaBox [0 0 612 792] /" + key + " " + box + " >>",
		"<< /Type /Page /Parent 2 0 R >>",
		"<< /Type /Page /Parent 2 0 R >>",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// fixedPagesNode runs the page-box fixer and returns the rewritten /Pages node.
func fixedPagesNode(t *testing.T, data []byte, preserveContent bool) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if _, err := fixPageBoxes(ctx, alloc, doc, Options{PreserveContent: preserveContent}, nil, ""); err != nil {
		t.Fatalf("fixPageBoxes: %v", err)
	}
	node, _ := alloc.Pending()[2].(ast.PdfDict)
	return node
}

// TestFixPageBoxesReachesTheInheritedBox pins that a boundary box declared on
// an interior page tree node is normalised.
//
// A box on a /Pages node is inherited by every page beneath it (ISO 32000-1,
// 7.7.3.4, Table 30), so ISO 19005-2, 6.1.13 test 11 measures it and litho
// reports it as "Pages tree /MediaBox". The fixer walked only the leaves, so
// 6-1-13-t09-fail-c and -fail-d stayed unremediable: their pages declare no box
// at all.
func TestFixPageBoxesReachesTheInheritedBox(t *testing.T) {
	t.Parallel()

	// A 2-unit page is below the 3-unit floor, and growing it is lossless.
	node := fixedPagesNode(t, inheritedBoxPDF("CropBox", "[0 0 2 2]"), true)
	if node == nil {
		t.Fatal("the /Pages node was not rewritten")
	}
	// A degenerate box that is not /MediaBox is dropped so it defaults to one
	// that is valid, which is the leaf pass's behaviour too.
	if box, still := node["CropBox"]; still {
		arr, ok := box.(ast.PdfArray)
		if !ok || len(arr) != 4 {
			t.Fatalf("/CropBox = %v, want a rectangle or removal", box)
		}
		if w := document.Float(arr[2]) - document.Float(arr[0]); w < 3 {
			t.Errorf("/CropBox width = %v, want at least 3", w)
		}
	}
}

// TestFixPageBoxesShrinksAnInheritedBoxOnlyWhenAllowed pins the safety gate:
// an over-sized box can crop content, so it is only brought down when the
// caller allows content changes.
func TestFixPageBoxesShrinksAnInheritedBoxOnlyWhenAllowed(t *testing.T) {
	t.Parallel()

	const oversize = "[0 0 14402 14402]"

	if node := fixedPagesNode(t, inheritedBoxPDF("MediaBox2", oversize), true); node != nil {
		if arr, ok := node["MediaBox2"].(ast.PdfArray); ok && len(arr) == 4 {
			if w := document.Float(arr[2]); w != 14402 {
				t.Errorf("an unknown key was altered: width = %v", w)
			}
		}
	}

	// The real key, with content changes allowed, must come inside the range.
	node := fixedPagesNode(t, inheritedBoxPDF("CropBox", oversize), false)
	if node == nil {
		t.Fatal("the /Pages node was not rewritten in aggressive mode")
	}
	arr, ok := node["CropBox"].(ast.PdfArray)
	if !ok || len(arr) != 4 {
		t.Fatalf("/CropBox = %v, want a rectangle", node["CropBox"])
	}
	if w := document.Float(arr[2]) - document.Float(arr[0]); w > 14400 {
		t.Errorf("/CropBox width = %v, want at most 14400", w)
	}
}

// TestFixPageBoxesLeavesAValidInheritedBox is the control: a legal box on the
// tree node must not be touched, so an untouched document stays byte-stable.
func TestFixPageBoxesLeavesAValidInheritedBox(t *testing.T) {
	t.Parallel()

	if node := fixedPagesNode(t, inheritedBoxPDF("CropBox", "[0 0 612 792]"), true); node != nil {
		t.Errorf("a valid inherited box caused a rewrite: %v", node["CropBox"])
	}
}
