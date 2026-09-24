package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestFixType0Font_KeepsAPeerFixersPendingCIDFontWrite pins that the Type 0
// font repair keeps a peer fixer's pending write to the descendant CIDFont.
//
// The stale read and the write that consumes it live in DIFFERENT functions
// here, which is why neither looks guilty on its own:
//
//	fixType0Font   :282  cidFont := document.ResolveDict(...)   stale
//	               :323  maps.Copy(newCID, cidFont)
//	               :349  persistCIDFont(alloc, ..., newCID, cidRef, ...)
//	persistCIDFont :424  alloc.AllocAt(cidRef.ObjectNum, newCID)
//
// persistCIDFont takes newCID as a parameter and contains no stale read;
// fixType0Font contains no write of that object. A per-function audit clears
// both, and the pair is still a clobber.
//
// The ordering makes it reachable rather than theoretical: cid-gidmap (registry
// 110) scans for Subtype == CIDFontType2, this very object, reads it
// alloc-aware and writes /CIDToGIDMap; fonts (144) then rebuilds it from the
// on-disk copy. cid-sysinfo (147) writes it too but reads alloc-aware and runs
// later, so it is defended.
//
// The marker is /DW2 because the fixture does NOT declare one. A key already
// present on disk would survive in the stale copy as well, and the test would
// pass for the wrong reason.
func TestFixType0Font_KeepsAPeerFixersPendingCIDFontWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// A wrong /DW, so the width repair actually fires and reaches persistCIDFont.
	doc, alloc := newTestDoc(t, cidFontDWPDF(t, "1000"))

	const (
		type0Obj = 4 // the Type 0 parent
		cidObj   = 5 // the descendant CIDFontType2, the object persistCIDFont writes
	)

	fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: type0Obj})
	if err != nil {
		t.Fatalf("resolving the Type0 font: %v", err)
	}
	cidFont := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: cidObj})
	if cidFont == nil {
		t.Fatalf("fixture descendant object %d did not resolve", cidObj)
	}
	if _, present := cidFont["DW2"]; present {
		t.Fatalf("precondition: the fixture must NOT declare /DW2, else the marker proves nothing")
	}

	// Stand in for cid-gidmap (110): copy the descendant, add one key, AllocAt
	// the same object number.
	peer := make(ast.PdfDict, len(cidFont)+1)
	maps.Copy(peer, cidFont)
	peer["DW2"] = ast.PdfArray{ast.PdfInt(880), ast.PdfInt(-1000)}
	if !alloc.AllocAt(cidObj, peer) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", cidObj)
	}

	fixes := fixType0Font(ctx, doc, alloc, fontDict,
		ast.PdfIndirectRef{ObjectNum: type0Obj}, true, false, false)

	got, ok := alloc.Pending()[cidObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d is %T, want a pending dictionary; fixes=%v",
			cidObj, alloc.Pending()[cidObj], fixes)
	}

	// Self-diagnosing: if the width repair never ran, this proves nothing about
	// clobbering and must say so rather than masquerade as a finding.
	if len(fixes) == 0 {
		t.Fatalf("the Type0 repair reported nothing, so the fixture never reached "+
			"persistCIDFont; descendant = %v", got)
	}

	if _, kept := got["DW2"]; !kept {
		t.Errorf("/DW2 is gone: fixType0Font built newCID from the on-disk copy and "+
			"persistCIDFont wrote it over the earlier fixer's edit; descendant = %v", got)
	}
}
