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

// indirectMediaBoxPDF builds two pages: the first declares a degenerate
// /MediaBox the fixer has to replace, the second declares a usable one through
// an indirect reference.
func indirectMediaBoxPDF() []byte {
	content := "BT /F1 12 Tf 100 500 Td (hello) Tj ET"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 0 0] /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox 5 0 R >>",
		"[0 0 800 600]",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
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

// TestDegenerateMediaBoxTakesAnIndirectSiblingsSize pins that the replacement
// for a degenerate /MediaBox comes from a size the document actually uses, even
// when that page writes its box indirectly.
//
// A degenerate box has no default and must be replaced, and the replacement is
// chosen from the document's own pages precisely so the page's content still
// falls inside it. Any object may be given as an indirect reference (ISO
// 32000-1, 7.3.10), and type-asserting the array skipped every page that does,
// so the fallback silently became US Letter: a page sitting beside an 800x600
// sibling was given 612x792 and anything past 612 units was cropped away.
func TestDegenerateMediaBoxTakesAnIndirectSiblingsSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := indirectMediaBoxPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	if _, err := fixPageBoxes(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixPageBoxes: %v", err)
	}

	page, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Fatalf("the degenerate page was not rewritten: %T", alloc.Pending()[3])
	}
	box, ok := page["MediaBox"].(ast.PdfArray)
	if !ok || len(box) != 4 {
		t.Fatalf("/MediaBox = %v, want a four-element array", page["MediaBox"])
	}
	if document.Float(box[2]) != 800 || document.Float(box[3]) != 600 {
		t.Errorf("/MediaBox = %v, want the sibling's 800x600 rather than the US Letter default", box)
	}
}
