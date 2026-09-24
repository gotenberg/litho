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

// formFlateContent is the body of the compressed form XObject under test. It
// draws a caption and carries one operator ISO 32000-1 does not define, so the
// fixer has a reason to rewrite the stream.
const formFlateContent = "BT\n/F1 12 Tf\n1 0 0 1 0 40 Tm\n[(Caption)] TJ\nET\nzz\n"

// compressedFormPDF builds a one-page document whose /Resources/XObject holds a
// FlateDecode form XObject. The page's own content invokes it.
func compressedFormPDF(t *testing.T) []byte {
	t.Helper()

	var deflated bytes.Buffer
	zw := zlib.NewWriter(&deflated)
	if _, err := zw.Write([]byte(formFlateContent)); err != nil {
		t.Fatalf("deflating form content: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing deflate writer: %v", err)
	}
	form := deflated.Bytes()

	const pageContent = "q\n/Fm0 Do\nQ\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /XObject << /Fm0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pageContent), pageContent),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 50]"+
			" /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", len(form), form),
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

// TestFixUnknownOpsDecodesFormXObjects pins that a compressed form XObject is
// parsed as content, not as its own compressed bytes.
//
// [ast.PdfStream.Bytes] returns the raw body, before filter decoding. Handing
// that to the content lexer makes every byte of a FlateDecode form read as an
// undefined operator (ISO 19005-2, 6.2.2), so the fixer blanked the whole form
// and wrote the empty result back under the original /Filter, losing whatever
// the form drew.
//
// # Corpus evidence
//
//   - pdfjs issue11678.pdf: page 7 lost the figure labels held in two such
//     forms, and the 18 "unknown operators" the run reported were 16 stretches
//     of compressed data.
func TestFixUnknownOpsDecodesFormXObjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := compressedFormPDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	fixes, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("the form holds an undefined operator, so the fixer should report a removal")
	}

	queued, ok := alloc.Pending()[5].(ast.PdfStream)
	if !ok {
		t.Fatalf("form XObject was not rewritten: %T", alloc.Pending()[5])
	}
	body, err := queued.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading rewritten form: %v", err)
	}
	if !bytes.Contains(body, []byte("(Caption)")) {
		t.Errorf("the form lost its text; body = %q", body)
	}
	if bytes.Contains(body, []byte("zz")) {
		t.Errorf("the undefined operator survived; body = %q", body)
	}
	if _, kept := queued.Dict["Filter"]; kept {
		t.Error("the rewritten body is decoded content, so keeping /Filter leaves a stream a reader decodes into nothing")
	}
	if _, kept := queued.Dict["DecodeParms"]; kept {
		t.Error("/DecodeParms describes the filter chain that no longer applies to the rewritten body")
	}
	if bbox, kept := queued.Dict["BBox"]; !kept {
		t.Error("the form lost its /BBox, which ISO 32000-1, Table 95 requires")
	} else if _, isArray := bbox.(ast.PdfArray); !isArray {
		t.Errorf("/BBox = %T, want an array", bbox)
	}
}
