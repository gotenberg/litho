package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// [fixXObjectTransparency] reads the XObject with document.ResolveObject
// (fixer_transparency.go), rebuilds its dictionary with
// maps.Copy(newDict, stream.Dict) and writes that back at xoRef.ObjectNum. It
// consults no alloc-aware helper, and fixResourceTransparency, the only thing
// it delegates to, is a pure dispatcher holding no read or write of its own.
// So an earlier fixer's edit to that stream is discarded.
//
// Peer and ordering are measured: graphics (registry 106) writes XObject
// streams in fixXObjects and reads them ALLOC-AWARE, and transparency is
// registry 107, the very next entry. Graphics writes correctly, transparency
// overwrites from the stale copy.
//
// THE SEEDED OBJECT IS 21, NOT 20, AND THAT IS THE WHOLE POINT. In
// nestedFormGroupPDF object 20 is the outer form and carries NO /Group; object
// 21 is the nested one that does. An earlier version of this test seeded 20,
// and it PASSED for the wrong reason: nothing was ever written to 20, so the
// assertions read back the seed itself. The repair count was non-zero because
// the recursion had repaired 21, a different object.
//
// Hence the guard below keys on the SEEDED object. The seed copies object 21's
// on-disk dictionary, so it carries /Group; /Group being gone afterwards is
// itself proof that this object was rewritten, which "the fixer returned a
// non-zero count" never was.
//
// The marker is /Metadata because the fixture does not declare one: a key
// already present on disk would survive inside the stale copy too.
func TestFixXObjectTransparency_KeepsAPeerFixersPendingStreamWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, nestedFormGroupPDF())

	const (
		outerObj = 20 // the walk entry; declares no /Group of its own
		innerObj = 21 // carries /Group, and is the object the repair rewrites
	)

	innerRef := ast.PdfIndirectRef{ObjectNum: innerObj}
	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, innerRef, innerRef)
	if !ok || stream.Dict == nil {
		t.Fatalf("fixture form XObject %d did not resolve", innerObj)
	}
	if _, present := stream.Dict["Group"]; !present {
		t.Fatalf("precondition: object %d must carry /Group, or its removal proves nothing", innerObj)
	}
	if _, present := stream.Dict["Metadata"]; present {
		t.Fatalf("precondition: the fixture must NOT declare /Metadata, else the marker proves nothing")
	}

	// Stand in for graphics (106): carry the body across untouched and add one
	// key, the shape the stream-writing fixers use.
	peer := make(ast.PdfDict, len(stream.Dict)+1)
	maps.Copy(peer, stream.Dict)
	peer["Metadata"] = ast.PdfIndirectRef{ObjectNum: 99}
	if !alloc.AllocAt(innerObj, ast.NewLazyStream(peer, stream.Bytes)) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", innerObj)
	}

	pageRes := ast.PdfDict{"XObject": ast.PdfDict{"Fm0": ast.PdfIndirectRef{ObjectNum: outerObj}}}
	fixXObjectTransparency(ctx, doc, alloc, pageRes, make(map[int]bool), 0)

	got, ok := pendingStream(t, alloc, innerObj)
	if !ok {
		t.Fatalf("object %d is no longer pending", innerObj)
	}

	// Self-diagnosing, keyed on the SEEDED object: the seed carried /Group, so
	// its absence proves object 21 itself was rewritten. Without this the test
	// could pass by reading back its own seed untouched.
	if _, still := got.Dict["Group"]; still {
		t.Fatalf("/Group survived on object %d, so the repair never rewrote it and this "+
			"test proves nothing about clobbering; dict = %v", innerObj, got.Dict)
	}

	if _, kept := got.Dict["Metadata"]; !kept {
		t.Errorf("/Metadata is gone: fixXObjectTransparency rebuilt the stream dictionary "+
			"from the on-disk copy and discarded the earlier fixer's write; dict = %v", got.Dict)
	}
}
