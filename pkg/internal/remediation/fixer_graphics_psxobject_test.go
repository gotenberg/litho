package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// psXObjectPDF builds a one-page document whose only XObject is a FlateDecode
// PostScript XObject, which ISO 19005-1, 6.2.7 and ISO 19005-2, 6.2.9.3 forbid.
func psXObjectPDF(t *testing.T) []byte {
	t.Helper()

	var deflated bytes.Buffer
	zw := zlib.NewWriter(&deflated)
	if _, err := zw.Write([]byte("%!PS-Adobe-3.0\nshowpage\n")); err != nil {
		t.Fatalf("deflating PostScript: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing deflate writer: %v", err)
	}
	ps := deflated.Bytes()

	const content = "q /PS0 Do Q\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /XObject << /PS0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /PS /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream",
			len(ps), ps),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestFixGraphicsPSXObjectDropsTheFilter pins that the no-op form replacing a
// PostScript XObject is a stream a reader can actually decode.
//
// The conversion empties the body so an existing Do still resolves to something
// valid. Copying the dict wholesale kept /Filter /FlateDecode over that empty
// body, and an empty FlateDecode stream decodes to an error rather than to the
// intended no-op (ISO 32000-1, 7.3.8.2).
func TestFixGraphicsPSXObjectDropsTheFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := psXObjectPDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

	notes, err := fixGraphics(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixGraphics: %v", err)
	}
	if len(notes) == 0 {
		t.Fatal("a PostScript XObject is forbidden, so the fixer should report a replacement")
	}

	queued, ok := alloc.Pending()[5].(ast.PdfStream)
	if !ok {
		t.Fatalf("the PostScript XObject was not rewritten: %T", alloc.Pending()[5])
	}
	if subtype, _ := queued.Dict["Subtype"].(ast.PdfName); subtype != "Form" {
		t.Errorf("/Subtype = %v, want Form", queued.Dict["Subtype"])
	}
	if _, filtered := queued.Dict["Filter"]; filtered {
		t.Error("the empty body still declares /Filter, so a reader decodes it into an error")
	}
	if _, filtered := queued.Dict["DecodeParms"]; filtered {
		t.Error("/DecodeParms describes a filter chain the empty body no longer uses")
	}
	if _, err := doc.DecodeStream(ctx, queued); err != nil {
		t.Errorf("the replacement form does not decode: %v", err)
	}
}
