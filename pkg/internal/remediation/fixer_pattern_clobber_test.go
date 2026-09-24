package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// inlinePatternTransparencyPDF declares the offending /ExtGState INLINE on the
// tiling pattern's own /Resources.
//
// patternTransparencyPDF, the fixture the rest of this package uses, hangs
// /GS0 off an indirect reference (5 0 R), so fixExtGStateDict rewrites THAT
// object and the pattern object is never written at all: its existing tests
// assert alloc.Pending()[5], never [4]. A clobber test needs the write to land
// on the object it seeded, so the graphics state has to live inline where the
// pattern's working copy covers it.
//
// Only the PatternType 1 arm is built. A shading pattern carries no /Resources
// and is never rewritten, so it cannot carry this defect; the existing suite
// already pins that branch.
func inlinePatternTransparencyPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Pattern << /P0 4 0 R >> >> >>",
		"<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 "+
			"/BBox [0 0 1 1] /XStep 1 /YStep 1 "+
			"/Resources << /ExtGState << /GS0 << /ca 0.5 /CA 0.5 /BM /Multiply >> >> >> "+
			"/Length 0 >>\nstream\n\nendstream",
	)
}

// TestFixPatternTransparency_KeepsAPeerFixersPendingPatternWrite pins that the
// pattern repair keeps an earlier fixer's pending write to the same pattern.
//
// [fixPatternTransparency] used to resolve each pattern with
// document.ResolveObject (fixer_transparency.go), copy the resolved dictionary
// into work and write that copy back at the pattern's own object number. It
// consulted no alloc-aware helper, so an earlier fixer's edit to the same
// pattern object was discarded.
//
// Peer and ordering are measured, not assumed: recurseColorSpacePatch writes
// pattern objects and runs at output-intent, fixer.go:68, ahead of transparency
// at fixer.go:107.
//
// The marker is /Metadata because the fixture does not declare one: a key
// already present on disk would survive inside the stale copy too.
func TestFixPatternTransparency_KeepsAPeerFixersPendingPatternWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlinePatternTransparencyPDF())

	const patObj = 4
	patRef := ast.PdfIndirectRef{ObjectNum: patObj}

	// Walk to the inline /GS0 the repair must normalise. Shared by the
	// precondition and the guard so the two cannot drift apart.
	gs0 := func(d ast.PdfDict) ast.PdfDict {
		res, ok := d["Resources"].(ast.PdfDict)
		if !ok {
			return nil
		}
		egs, ok := res["ExtGState"].(ast.PdfDict)
		if !ok {
			return nil
		}
		gs, _ := egs["GS0"].(ast.PdfDict)
		return gs
	}

	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, patRef, patRef)
	if !ok || stream.Dict == nil {
		t.Fatalf("fixture tiling pattern %d did not resolve as a stream", patObj)
	}
	if _, present := stream.Dict["Metadata"]; present {
		t.Fatalf("precondition: the fixture must NOT declare /Metadata, else the marker proves nothing")
	}
	if gs := gs0(stream.Dict); gs == nil || document.Float(gs["ca"]) == 1.0 {
		t.Fatalf("precondition: object %d must carry a non-conforming INLINE /GS0, or the "+
			"repair never rewrites it; dict = %v", patObj, stream.Dict)
	}

	// Stand in for recurseColorSpacePatch: carry the body across untouched and
	// add one key, the shape the pattern-writing fixers use.
	peer := make(ast.PdfDict, len(stream.Dict)+1)
	maps.Copy(peer, stream.Dict)
	peer["Metadata"] = ast.PdfIndirectRef{ObjectNum: 99}
	if !alloc.AllocAt(patObj, ast.NewLazyStream(peer, stream.Bytes)) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", patObj)
	}

	pageRes := ast.PdfDict{"Pattern": ast.PdfDict{"P0": patRef}}
	fixPatternTransparency(ctx, doc, alloc, pageRes, make(map[int]bool), 0)

	got, ok := pendingStream(t, alloc, patObj)
	if !ok {
		t.Fatalf("object %d is no longer pending", patObj)
	}

	// Self-diagnosing, keyed on the SEEDED object: the seed carried /ca 0.5, so
	// seeing 1.0 proves object 4 itself was rewritten by the repair. Without
	// this the test could pass by reading back its own seed untouched.
	gs := gs0(got.Dict)
	if gs == nil || document.Float(gs["ca"]) != 1.0 {
		t.Fatalf("object %d still carries /ca %v: the repair never rewrote it, so this "+
			"test proves nothing about clobbering; dict = %v",
			patObj, document.Float(gs["ca"]), got.Dict)
	}

	if _, kept := got.Dict["Metadata"]; !kept {
		t.Errorf("/Metadata is gone: fixPatternTransparency rebuilt the pattern dictionary "+
			"from the on-disk copy and discarded the earlier fixer's write; dict = %v", got.Dict)
	}
}
