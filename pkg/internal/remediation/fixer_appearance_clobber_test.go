package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixAppearanceStream_KeepsAPeerFixersPendingStreamWrite pins that the
// appearance-stream repair keeps a peer fixer's pending write to the same
// appearance object.
//
// [fixAppearanceStream] rebuilds the appearance stream dictionary with
// maps.Copy(work, stream.Dict) and writes it back at the appearance object's
// own number, but the stream it copies is the one its CALLER passed in, and
// [fixAnnotationTransparency] resolves that with document.ResolveObject: once
// for /N, /R and /D, and again for each state in an appearance subdictionary.
// So the stale read feeding this write lives in the caller, at two sites, and
// an earlier fixer's edit to the appearance object is discarded.
//
// Peer and ordering are measured: repairAppearanceStream writes appearance
// streams at need-appearances, registry 104, ahead of transparency at 107.
//
// The test drives the real entry point rather than calling the writer directly,
// because calling fixAppearanceStream with a stream this test resolved itself
// would supply an alloc-aware value the production path never has, and would
// pass while the defect stands.
//
// sharedAppearancePDF is the fixture that can express the defect: object 20 is
// an INDIRECT /N stream carrying both /Group and an inline /ExtGState, so the
// repair both deletes the group and normalises /ca, and AllocAt fires on object
// 20 itself. A fixture whose appearance is direct, or whose object carries
// neither construct, is never rewritten and would pass while proving nothing.
//
// The marker is /Metadata because the fixture does not declare one: a key
// already present on disk would survive inside the stale copy too.
func TestFixAppearanceStream_KeepsAPeerFixersPendingStreamWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, sharedAppearancePDF())

	const apObj = 20
	apRef := ast.PdfIndirectRef{ObjectNum: apObj}

	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, apRef, apRef)
	if !ok || stream.Dict == nil {
		t.Fatalf("fixture appearance stream %d did not resolve", apObj)
	}
	if _, present := stream.Dict["Group"]; !present {
		t.Fatalf("precondition: object %d must carry /Group, or its removal proves nothing", apObj)
	}
	if _, present := stream.Dict["Metadata"]; present {
		t.Fatalf("precondition: the fixture must NOT declare /Metadata, else the marker proves nothing")
	}

	// Stand in for repairAppearanceStream: carry the body across untouched and
	// add one key, the shape the stream-writing fixers use.
	peer := make(ast.PdfDict, len(stream.Dict)+1)
	maps.Copy(peer, stream.Dict)
	peer["Metadata"] = ast.PdfIndirectRef{ObjectNum: 99}
	if !alloc.AllocAt(apObj, ast.NewLazyStream(peer, stream.Bytes)) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", apObj)
	}

	annots := ast.PdfArray{
		ast.PdfIndirectRef{ObjectNum: 15},
		ast.PdfIndirectRef{ObjectNum: 16},
	}
	fixAnnotationTransparency(ctx, doc, alloc, annots, make(map[int]bool))

	got, ok := pendingStream(t, alloc, apObj)
	if !ok {
		t.Fatalf("object %d is no longer pending", apObj)
	}

	// Self-diagnosing, keyed on the SEEDED object: the seed carried /Group, so
	// its absence proves object 20 itself was rewritten. Without this the test
	// could pass by reading back its own seed untouched.
	if _, still := got.Dict["Group"]; still {
		t.Fatalf("/Group survived on object %d, so the repair never rewrote it and this "+
			"test proves nothing about clobbering; dict = %v", apObj, got.Dict)
	}

	if _, kept := got.Dict["Metadata"]; !kept {
		t.Errorf("/Metadata is gone: the appearance repair rebuilt the stream dictionary "+
			"from the copy its caller read out of the document and discarded the earlier "+
			"fixer's write; dict = %v", got.Dict)
	}
}
