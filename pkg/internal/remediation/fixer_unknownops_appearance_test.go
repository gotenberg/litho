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

// appearanceOpsPDF builds a one-page document whose annotation carries a
// FlateDecode appearance stream holding an operator ISO 32000-1 does not
// define, alongside a text-showing operator that has to survive the repair.
func appearanceOpsPDF(t *testing.T, stateDict bool) []byte {
	t.Helper()

	body := []byte("q BT /F1 12 Tf 0 20 Td (Caption) Tj ET zz Q\n")
	var deflated bytes.Buffer
	zw := zlib.NewWriter(&deflated)
	if _, err := zw.Write(body); err != nil {
		t.Fatalf("deflating appearance: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing deflate writer: %v", err)
	}
	ap := deflated.Bytes()

	// /AP/N is a stream, or a subdictionary keyed by appearance state.
	apEntry := "<< /N 7 0 R >>"
	if stateDict {
		apEntry = "<< /N << /Yes 7 0 R >> >>"
	}

	const page = "BT /F1 12 Tf 20 700 Td (body) Tj ET\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> /Annots [6 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Annot /Subtype /Widget /Rect [100 100 200 150] /F 4 /AP " + apEntry + " >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 50]"+
			" /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", len(ap), ap),
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

func repairedAppearance(t *testing.T, stateDict bool) ast.PdfStream {
	t.Helper()

	ctx := context.Background()
	data := appearanceOpsPDF(t, stateDict)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

	fixes, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("the appearance stream holds an undefined operator, so a removal should be reported")
	}
	queued, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("the appearance stream was not rewritten: %T", alloc.Pending()[7])
	}
	return queued
}

// TestFixUnknownOpsStripsAppearanceStreams pins that an undefined operator is
// removed from an annotation appearance stream.
//
// An appearance stream is a content stream (ISO 32000-1, 12.5.5), so the ISO
// 19005-2, 6.2.2 prohibition reaches it, but it hangs off /Annots[]/AP rather
// than the page resources this fixer walked, so veraPDF kept reporting an
// operator litho had left in place.
func TestFixUnknownOpsStripsAppearanceStreams(t *testing.T) {
	t.Parallel()

	queued := repairedAppearance(t, false)
	body, err := queued.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading the rewritten appearance: %v", err)
	}
	if bytes.Contains(body, []byte("zz")) {
		t.Errorf("the undefined operator survived: %q", body)
	}
	if !bytes.Contains(body, []byte("(Caption)")) {
		t.Errorf("the appearance lost its text: %q", body)
	}
	if _, kept := queued.Dict["Filter"]; kept {
		t.Error("the rewritten body is decoded content, so keeping /Filter leaves a stream a reader decodes into nothing")
	}
	if _, kept := queued.Dict["BBox"]; !kept {
		t.Error("the appearance lost its /BBox, which ISO 32000-1, Table 95 requires of a form")
	}
}

// TestFixUnknownOpsStripsAppearanceStateDicts pins the same repair when /AP/N
// is a subdictionary of appearance states rather than a single stream, the
// shape a checkbox or radio button uses (ISO 32000-1, 12.5.5).
func TestFixUnknownOpsStripsAppearanceStateDicts(t *testing.T) {
	t.Parallel()

	queued := repairedAppearance(t, true)
	body, err := queued.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading the rewritten appearance: %v", err)
	}
	if bytes.Contains(body, []byte("zz")) {
		t.Errorf("the undefined operator survived in a state subdictionary: %q", body)
	}
	if !bytes.Contains(body, []byte("(Caption)")) {
		t.Errorf("the appearance lost its text: %q", body)
	}
}
