package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// webCapturePDF builds a two-page document carrying two Web Capture content sets
// in the /Names /IDS tree (ISO 32000-1, 14.10.4): one that produced both pages,
// and one that produced only the page the delete removes.
func webCapturePDF(t *testing.T) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /IDS 7 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Names [(spanning) 8 0 R (page one only) 9 0 R] >>",
		"<< /Type /SPS /S /SPS /O [3 0 R 4 0 R] >>",
		"<< /Type /SPS /S /SPS /O [3 0 R] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestDeletePagesPrunesWebCaptureSets pins that a content set stops naming a
// page the delete removed.
//
// A content set lists what one capture produced in /O (ISO 32000-1, Table 356),
// so one naming a deleted page is a live reference that keeps the page in the
// output.
//
// # Corpus evidence
//
//   - govdocs thread_400_400742.pdf: held 730 bytes of a deleted page through
//     a content set after every other path had been closed.
func TestDeletePagesPrunesWebCaptureSets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := webCapturePDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := DeletePages(ctx, alloc, doc, pages, []int{1}); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}

	// The set that produced both pages keeps its entry and loses the dead one.
	spanning, ok := alloc.Pending()[8].(ast.PdfDict)
	if !ok {
		t.Fatalf("the spanning content set was not rewritten: %T", alloc.Pending()[8])
	}
	produced, ok := spanning["O"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/O is %T, want an array", spanning["O"])
	}
	if len(produced) != 1 {
		t.Fatalf("/O holds %d objects, want 1: only the deleted page should go", len(produced))
	}
	if ref, isRef := produced[0].(ast.PdfIndirectRef); !isRef || ref.ObjectNum != 4 {
		t.Errorf("/O kept %v, want object 4, the page that stayed", produced[0])
	}

	// The set whose every object is gone leaves the name tree entirely.
	tree, ok := alloc.Pending()[7].(ast.PdfDict)
	if !ok {
		t.Fatalf("the /IDS name tree was not rewritten: %T", alloc.Pending()[7])
	}
	names, ok := tree["Names"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Names is %T, want an array", tree["Names"])
	}
	if len(names) != 2 {
		t.Fatalf("/Names holds %d entries, want 1 key/value pair", len(names)/2)
	}
	if key, _ := names[0].(ast.PdfString); string(key) != "spanning" {
		t.Errorf("the surviving key is %q, want \"spanning\"", key)
	}
}
