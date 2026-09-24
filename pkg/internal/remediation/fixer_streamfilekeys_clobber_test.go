package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixStreamFileKeys_KeepsTheLZWFixersPendingStreamWrite pins that removing
// the external-file keys rewrites the stream lzw queued, not the document's
// copy.
//
// Both fixers answer rules that bind on EVERY stream (ISO 19005-1, 6.1.7 test
// 3 and 6.1.10), and lzw runs first. fixStreamFileKeys read the named stream
// with document.ResolveStream and wrote it back with its original body, so a
// stream that also carried LZWDecode went out LZW-compressed again, with the
// FlateDecode re-encoding lost and 6.1.10 back in the output.
func TestFixStreamFileKeys_KeepsTheLZWFixersPendingStreamWrite(t *testing.T) {
	t.Parallel()

	const content = "0 0 m 10 10 l S\n"
	ctx := context.Background()
	body := lzwBody(t, content)
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Filter /LZWDecode /F (external.dat) /Length %d >>\nstream\n%s\nendstream", len(body), body),
	))

	const streamObj = 4

	if _, err := fixLZW(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixLZW: %v", err)
	}
	seeded, ok := pendingStream(t, alloc, streamObj)
	if !ok || seeded.Dict["Filter"] != ast.PdfName("FlateDecode") {
		t.Fatalf("control: lzw did not queue a FlateDecode stream at object %d, so there is no "+
			"earlier edit to lose; pending = %v", streamObj, alloc.Pending()[streamObj])
	}

	violations := []compliance.Violation{{
		Rule:    compliance.Rule{Specification: "ISO_19005_1", Clause: "6.1.7", TestNumber: 3},
		Context: fmt.Sprintf("object %d: stream dict contains [F]", streamObj),
	}}
	if _, err := fixStreamFileKeys(ctx, alloc, doc, Options{}, violations, "PDF/A-1b"); err != nil {
		t.Fatalf("fixStreamFileKeys: %v", err)
	}

	got, ok := pendingStream(t, alloc, streamObj)
	if !ok {
		t.Fatalf("object %d is no longer pending", streamObj)
	}
	if _, has := got.Dict["F"]; has {
		t.Fatalf("/F survived on object %d: the repair did not run, so the check below proves "+
			"nothing", streamObj)
	}
	if f := got.Dict["Filter"]; f != ast.PdfName("FlateDecode") {
		t.Errorf("/Filter is %v, want /FlateDecode: fixStreamFileKeys rewrote the document's "+
			"stream and discarded lzw's re-encoding", f)
	}
	if decoded := pendingContent(t, doc, alloc, streamObj); decoded != content {
		t.Errorf("object %d decodes to %q, want %q", streamObj, decoded, content)
	}
}
