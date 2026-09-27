package writer

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCopier_MapToStandsAnObjectIn pins that a source object mapped to an
// output object is never copied: every reference to it the copier meets is
// rewritten to the output object, and the objects it references stay
// uncopied too.
func TestCopier_MapToStandsAnObjectIn(t *testing.T) {
	t.Parallel()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		// 3 references 4, which references 5: mapping 4 cuts 5 off.
		"<< /Parent 4 0 R >>",
		"<< /Next 5 0 R >>",
		"<< /Leaf true >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, obj := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xrefAt := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefAt)
	src := b.Bytes()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(src), int64(len(src)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := NewObjAllocator(0)
	standIn := alloc.Alloc(ast.PdfDict{"StandIn": ast.PdfBool(true)})
	copier := NewCopier(doc, alloc)
	copier.MapTo(4, standIn)

	copied, err := copier.CopyRef(ctx, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("CopyRef: %v", err)
	}
	dict, ok := alloc.Pending()[copied.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("the copy of object 3 is %T, want a dictionary", alloc.Pending()[copied.ObjectNum])
	}
	if got := dict["Parent"]; got != standIn {
		t.Errorf("/Parent = %v, want the stand-in %v", got, standIn)
	}
	if got := len(alloc.Pending()); got != 2 {
		t.Errorf("the output holds %d objects, want 2: the stand-in and the copy of object 3", got)
	}
}
