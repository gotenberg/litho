package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixNestedResources_KeepsAPeerFixersPendingStreamWrite pins the stream
// half of the same contract [fixType3FontResources] breaks with dictionaries:
// [fixFormXObjectResources] rewrites a form XObject STREAM, so it must read
// that stream through the allocator too.
//
// Form XObject streams are among the most written objects in the package.
// Thirteen sites call AllocAt with a stream at an object number, in aggressive,
// images, outputintent, needappearances, unknownops, graphics and transparency,
// so an earlier fixer having already rewritten this object is the normal case
// rather than a corner one.
//
// The `refCount != 1` guard in the fixer does not help here: it declines a
// SHARED XObject, which says nothing about whether a peer FIXER wrote it.
//
// Reuses nestedFormPDF, whose object 4 is a singly-referenced Form XObject that
// TestFixNestedResources_GivesAFormXObjectItsOwnResources already proves the
// repair fires on, so a failure here is the clobber and not a dead fixture.
func TestFixNestedResources_KeepsAPeerFixersPendingStreamWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, nestedFormPDF())

	const xobjObj = 4
	ref := ast.PdfIndirectRef{ObjectNum: xobjObj}
	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, ref)
	if !ok || stream.Dict == nil {
		t.Fatalf("fixture form XObject %d did not resolve", xobjObj)
	}

	// Stand in for any earlier stream-writing fixer: carry the body across
	// untouched and add one key, the shape those fixers use.
	peer := make(ast.PdfDict, len(stream.Dict)+1)
	maps.Copy(peer, stream.Dict)
	peer["Metadata"] = ast.PdfIndirectRef{ObjectNum: 99}
	if !alloc.AllocAt(xobjObj, ast.NewLazyStream(peer, stream.Bytes)) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", xobjObj)
	}

	if _, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}

	got, ok := pendingStream(t, alloc, xobjObj)
	if !ok {
		t.Fatalf("object %d is no longer pending", xobjObj)
	}

	// The fixer's own job must still be done.
	if _, has := got.Dict["Resources"]; !has {
		t.Errorf("the form XObject gained no /Resources, so the repair did not run: %v", got.Dict)
	}

	if _, kept := got.Dict["Metadata"]; !kept {
		t.Errorf("/Metadata is gone: fixFormXObjectResources rebuilt the stream dictionary "+
			"from the on-disk copy and discarded the earlier fixer's write; dict = %v", got.Dict)
	}
}
