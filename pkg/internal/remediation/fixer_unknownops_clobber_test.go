package remediation

import (
	"bytes"
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestStripUnknownInResources_KeepsAnEarlierFixersPendingStreamWrite pins that
// the resources path reads each XObject stream through the allocator.
//
// [stripUnknownInResources] walks resources["XObject"]. It used to read each
// stream with document.ResolveStream and write the stripped result back at
// that stream's own object number (fixer_unknownops.go), and its sibling
// [stripUnknownFromStreamObject] did the same. Neither consulted an
// alloc-aware helper, so an earlier fixer's edit to the same XObject stream was
// discarded.
//
// Peer and ordering are measured STRUCTURALLY, not by token: graphics writes
// XObject streams in fixXObjects (two sites), transparency writes them in
// fixXObjectTransparency, and nested-resources writes them in
// fixFormXObjectResources. Their registry entries are fixer.go:106, :107 and
// :131, all ahead of unknown-ops at :142.
//
// The fixture is already live and the package proves it: compressedFormPDF
// stores the form INDIRECTLY as /Fm0 5 0 R, and the existing
// TestFixUnknownOpsDecodesFormXObjects asserts alloc.Pending()[5], so AllocAt
// demonstrably fires on object 5.
//
// The marker is /Metadata because that dictionary declares only /Type,
// /Subtype, /BBox, /Filter and /Length: a key already present on disk would
// survive inside the stale copy too.
func TestStripUnknownInResources_KeepsAnEarlierFixersPendingStreamWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, compressedFormPDF(t))

	const formObj = 5
	formRef := ast.PdfIndirectRef{ObjectNum: formObj}

	stream, err := document.ResolveStream(ctx, doc, formRef)
	if err != nil {
		t.Fatalf("fixture form XObject %d did not resolve: %v", formObj, err)
	}
	if _, present := stream.Dict["Metadata"]; present {
		t.Fatalf("precondition: the fixture must NOT declare /Metadata, else the marker proves nothing")
	}
	onDisk, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the fixture form body: %v", err)
	}

	// Stand in for graphics, transparency or nested-resources: carry the body
	// across untouched and add one key, the shape those fixers use.
	peer := make(ast.PdfDict, len(stream.Dict)+1)
	maps.Copy(peer, stream.Dict)
	peer["Metadata"] = ast.PdfIndirectRef{ObjectNum: 99}
	if !alloc.AllocAt(formObj, ast.NewLazyStream(peer, stream.Bytes)) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", formObj)
	}

	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}

	got, ok := alloc.Pending()[formObj].(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d is no longer a pending stream: %T", formObj, alloc.Pending()[formObj])
	}
	body, err := got.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the rewritten form: %v", err)
	}

	// Self-diagnosing, keyed on the SEEDED object: the seed carried the
	// original body, undefined operator and all, so "zz" being gone proves
	// object 5 itself was rewritten by the repair. Without this the test could
	// pass by reading back its own seed untouched.
	if bytes.Equal(body, onDisk) || bytes.Contains(body, []byte("zz")) {
		t.Fatalf("object %d still holds the unrepaired body: the repair never rewrote "+
			"it, so this test proves nothing about clobbering; body = %q", formObj, body)
	}

	if _, kept := got.Dict["Metadata"]; !kept {
		t.Errorf("/Metadata is gone: the unknown-ops repair rebuilt the stream "+
			"dictionary from the on-disk copy and discarded the earlier fixer's "+
			"write; dict = %v", got.Dict)
	}
}
