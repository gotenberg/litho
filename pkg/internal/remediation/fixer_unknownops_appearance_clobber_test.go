package remediation

import (
	"bytes"
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestStripUnknownFromStreamObject_KeepsAnEarlierFixersPendingStreamWrite pins
// that the appearance-stream path reads the stream through the allocator.
//
// [stripUnknownFromStreamObject] used to read the appearance stream with
// document.ResolveStream (fixer_unknownops.go) and write the stripped result
// back at that stream's own object number. It consulted no alloc-aware helper,
// so an earlier fixer's edit to the same stream was discarded.
//
// THIS IS THE SIBLING PATH, and it needs its own test rather than inheriting
// the one written for stripUnknownInResources. That function reaches XObject
// streams through the page's /Resources and took a DIFFERENT read, via
// doc.Resolve plus a type assertion, which is why a census grepping for
// document.ResolveStream found one read site but not the other at all. One
// registry entry, one defect class, TWO read sites, and a test that covers
// only one of them would leave the other fixed on shape alone.
//
// Peer and ordering are the same as the resources path and are structural:
// graphics writes XObject streams in fixXObjects, transparency in
// fixXObjectTransparency, nested-resources in fixFormXObjectResources, at
// fixer.go:106, :107 and :131 respectively, all ahead of unknown-ops at :142.
// An appearance stream is a Form XObject (ISO 32000-1, 12.5.5), so those
// writers reach it too.
//
// appearanceOpsPDF is already proven live by the package itself: object 7 is
// the /AP /N stream, stored INDIRECTLY, and repairedAppearance asserts
// alloc.Pending()[7]. The marker is /Metadata because that dictionary declares
// only /Type, /Subtype, /BBox, /Filter and /Length.
func TestStripUnknownFromStreamObject_KeepsAnEarlierFixersPendingStreamWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, appearanceOpsPDF(t, false))

	const apObj = 7
	apRef := ast.PdfIndirectRef{ObjectNum: apObj}

	stream, err := document.ResolveStream(ctx, doc, apRef)
	if err != nil {
		t.Fatalf("fixture appearance stream %d did not resolve: %v", apObj, err)
	}
	if _, present := stream.Dict["Metadata"]; present {
		t.Fatalf("precondition: the fixture must NOT declare /Metadata, else the marker proves nothing")
	}
	onDisk, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the fixture appearance body: %v", err)
	}

	// Stand in for graphics, transparency or nested-resources: carry the body
	// across untouched and add one key, the shape those fixers use.
	peer := make(ast.PdfDict, len(stream.Dict)+1)
	maps.Copy(peer, stream.Dict)
	peer["Metadata"] = ast.PdfIndirectRef{ObjectNum: 99}
	if !alloc.AllocAt(apObj, ast.NewLazyStream(peer, stream.Bytes)) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", apObj)
	}

	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}

	got, ok := alloc.Pending()[apObj].(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d is no longer a pending stream: %T", apObj, alloc.Pending()[apObj])
	}
	body, err := got.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the rewritten appearance: %v", err)
	}

	// Self-diagnosing, keyed on the SEEDED object: the seed carried the
	// original body, undefined operator and all, so "zz" being gone proves
	// object 7 itself was rewritten by the repair rather than read back.
	if bytes.Equal(body, onDisk) || bytes.Contains(body, []byte("zz")) {
		t.Fatalf("object %d still holds the unrepaired body: the repair never rewrote "+
			"it, so this test proves nothing about clobbering; body = %q", apObj, body)
	}
	if !bytes.Contains(body, []byte("(Caption)")) {
		t.Fatalf("object %d lost its text, so the repair did not behave as the suite "+
			"expects and this test proves nothing; body = %q", apObj, body)
	}

	if _, kept := got.Dict["Metadata"]; !kept {
		t.Errorf("/Metadata is gone: stripUnknownFromStreamObject rebuilt the stream "+
			"dictionary from the on-disk copy and discarded the earlier fixer's "+
			"write; dict = %v", got.Dict)
	}
}
