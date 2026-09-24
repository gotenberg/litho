package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestFixType3FontTransparency_KeepsAPeerFixersPendingFontWrite pins that the
// Type 3 font repair reads the font dictionary through the allocator.
//
// [fixType3FontTransparency] used to read the Type 3 font dictionary with
// document.ResolveDict (fixer_transparency.go), copy it with
// maps.Copy(work, fd) and write that copy back at the font's own object
// number. It consulted no alloc-aware helper, so an earlier fixer's edit to the
// same font object was discarded.
//
// Peer and ordering are measured, not assumed: patchType3FontColorSpace
// (fixer_outputintent.go) writes Type 3 font dictionaries and IS alloc-aware,
// and it runs at registry entries output-intent 68 and output-intent-cleanup
// 70, both far ahead of transparency at 107. So transparency read that object
// stale and dropped the colour-space repair.
//
// THE FIXTURE MUST BE THE INLINE ONE. type3FontPDF stores /GS0 as an indirect
// reference, so fixExtGStateDict writes THAT object and the font dict is never
// rewritten at all: seeding object 10 there would read back the seed itself and
// pass while proving nothing. inlineType3PDF declares /Resources, /ExtGState
// and /GS0 all inline on object 10, the only shape in which AllocAt fires on
// the font object.
//
// The marker is /Name because the fixture does not declare one: a key already
// present on disk would survive inside the stale copy too.
//
// # Corpus evidence
//
//   - All corpora: the carriers declare /Resources, /ExtGState and /GS0 all
//     inline on the font, the shape inlineType3PDF builds.
func TestFixType3FontTransparency_KeepsAPeerFixersPendingFontWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineType3PDF("/ca 0.5"))

	const fontObj = 10

	// Walk to the inline /GS0 that the repair has to normalise. Used for both
	// the precondition and the guard, so the two cannot drift apart.
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

	fontDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: fontObj})
	if fontDict == nil {
		t.Fatalf("fixture Type 3 font object %d did not resolve", fontObj)
	}
	if _, present := fontDict["Name"]; present {
		t.Fatalf("precondition: the fixture must NOT declare /Name, else the marker proves nothing")
	}
	if gs := gs0(fontDict); gs == nil || document.Float(gs["ca"]) == 1.0 {
		t.Fatalf("precondition: object %d must carry a non-conforming inline /GS0, "+
			"or the repair never rewrites it; font = %v", fontObj, fontDict)
	}

	// Stand in for patchType3FontColorSpace: carry the dict across and add one
	// key, the shape every dict-writing fixer uses.
	peer := make(ast.PdfDict, len(fontDict)+1)
	maps.Copy(peer, fontDict)
	peer["Name"] = ast.PdfName("F1")
	if !alloc.AllocAt(fontObj, peer) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", fontObj)
	}

	pageRes := ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: fontObj}}}
	fixType3FontTransparency(ctx, doc, alloc, pageRes, make(map[int]bool), 0)

	got := pendingDict(t, alloc, fontObj)

	// Self-diagnosing, keyed on the SEEDED object: the seed carried /ca 0.5, so
	// seeing 1.0 proves object 10 itself was rewritten by the repair. Without
	// this the test could pass by reading back its own seed untouched.
	gs := gs0(got)
	if gs == nil || document.Float(gs["ca"]) != 1.0 {
		t.Fatalf("object %d still carries /ca %v: the repair never rewrote it, so this "+
			"test proves nothing about clobbering; font = %v",
			fontObj, document.Float(gs["ca"]), got)
	}

	if _, kept := got["Name"]; !kept {
		t.Errorf("/Name is gone: fixType3FontTransparency rebuilt the font dictionary "+
			"from the on-disk copy and discarded the earlier fixer's write; font = %v", got)
	}
}
