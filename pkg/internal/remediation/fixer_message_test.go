package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// isoDatePDF builds a document whose /Info carries ISO 8601 dates, the form
// fixInfoDate converts.
func isoDatePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /CreationDate (2005-03-14T10:11:12Z) /ModDate (2006-04-15T11:12:13Z) >>",
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
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info 4 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, start)
	return b.Bytes()
}

// TestFixMessagesNameAKeyWithOneSlash pins that a reported PDF name carries a
// single slash.
//
// [ast.PdfName] renders its own leading slash, so a "/%s" format verb applied
// to one produced "//CreationDate" and "//DocMDP" in the fix list a user reads.
func TestFixMessagesNameAKeyWithOneSlash(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := isoDatePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

	fixes, err := fixInfoDate(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixInfoDate: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("no date was converted, so the message under test was never built")
	}
	for _, fix := range fixes {
		if strings.Contains(fix, "//") {
			t.Errorf("fix message names a key with two slashes: %q", fix)
		}
	}
	if want := "converted /Info /CreationDate to D: format"; fixes[0] != want {
		t.Errorf("fix message = %q, want %q", fixes[0], want)
	}
}
