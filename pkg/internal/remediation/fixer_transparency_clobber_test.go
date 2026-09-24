package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// pageExtGStatePDF puts the offending /ExtGState straight on the page's
// /Resources, object 4, so a peer fixer's pending write lands on the same
// object the transparency walk rewrites.
func pageExtGStatePDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /ExtGState << /GS0 4 0 R >> >> >>",
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 >>",
	)
}

// TestFixTransparency_KeepsAPeerFixersPendingExtGStateWrite pins that the
// transparency repair reads an /ExtGState through the allocator.
//
// graphics (registry 106) and transparency (registry 107) are CONSECUTIVE
// registry entries that write the same /ExtGState objects. fixExtGStates reads
// each entry through resolveFromAllocOrDoc; fixExtGStateDict used to read it
// with document.ResolveDict and no alloc-aware helper at all, so running second
// it rebuilt the dictionary from the on-disk copy and dropped whatever graphics
// just wrote there.
//
// The pending write stands in for that peer. Provoking graphics for real needs
// a second violation on the same object (an /HT or /TR entry it repairs);
// seeding the allocator reproduces the state it leaves behind without coupling
// this test to another fixer's matcher.
//
// The fixpoint loop in pkg/pdf hides this in the output, exactly as it hid
// 9bbdcd41: a discarded repair is simply reapplied on the next round, so only a
// unit test can see it.
func TestFixTransparency_KeepsAPeerFixersPendingExtGStateWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, pageExtGStatePDF())

	const gsObj = 4
	gs := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: gsObj})
	if gs == nil {
		t.Fatalf("fixture ExtGState object %d did not resolve", gsObj)
	}

	// Stand in for graphics (106): copy the dict, add one key, AllocAt the
	// same object number. /HT is what fixExtGStates normalises there.
	peer := make(ast.PdfDict, len(gs)+1)
	maps.Copy(peer, gs)
	peer["HT"] = ast.PdfName("Default")
	if !alloc.AllocAt(gsObj, peer) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", gsObj)
	}

	if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixTransparency: %v", err)
	}

	got, ok := alloc.Pending()[gsObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d is %T, want a pending dictionary", gsObj, alloc.Pending()[gsObj])
	}

	// The fixer's own job must still be done: this is a composition test, not
	// an assertion that it leaves the ExtGState alone.
	if v, isReal := got["ca"].(ast.PdfReal); !isReal || float64(v) != 1.0 {
		t.Errorf("/ca = %v, want 1.0: the transparency repair did not run", got["ca"])
	}

	if _, kept := got["HT"]; !kept {
		t.Errorf("/HT is gone: fixExtGStateDict rebuilt the ExtGState from the on-disk "+
			"copy and discarded the earlier fixer's write; ExtGState = %v", got)
	}
}
