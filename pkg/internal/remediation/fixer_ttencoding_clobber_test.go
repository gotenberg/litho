package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// A fixer that rewrites an object another fixer also rewrites must read that
// object through the allocator. [RemediatePDFA] drives every registry entry
// against ONE allocator and ONE document and never flushes between them, so a
// fixer that rebuilds a dictionary from the on-disk copy discards the pending
// edits of every fixer that ran before it.
//
// tt-encoding replaces the whole font dictionary, and it runs after the font
// fixers that write the same object: cid-gidmap, font-keys, tounicode-ranges,
// fonts, and font-widths. Those read alloc-aware (fixer_cidgidmap.go:55,
// fixer_fontwidth.go:53); this pins that tt-encoding does too.
//
// Worth pinning because nothing else catches it. The fix-to-fixpoint loop in
// pkg/pdf reopens the output and re-runs up to maxRemediationRounds, so a
// discarded repair is simply reapplied on the next round and never reaches the
// output. The only symptom was the same fix line reported twice, and
// dedupeFixes (pkg/pdf/writer.go) now collapses that too.
func TestFixTrueTypeEncoding_KeepsAnEarlierFixersPendingWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, trueTypePDF(32))

	// The fixture declares /Widths [600 600] on object 4.
	const fontObj = 4
	fontDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: fontObj})
	if fontDict == nil {
		t.Fatalf("fixture font object %d did not resolve", fontObj)
	}

	// Stand in for any earlier font fixer leaving a repair pending, the shape
	// fixSimpleFont writes at fixer_fonts.go:254: copy the dict, correct one
	// key, AllocAt the same object number.
	peer := make(ast.PdfDict, len(fontDict))
	maps.Copy(peer, fontDict)
	peer["Widths"] = ast.PdfArray{ast.PdfReal(500), ast.PdfReal(500)}
	if !alloc.AllocAt(fontObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", fontObj)
	}

	if _, err := fixTrueTypeEncoding(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixTrueTypeEncoding: %v", err)
	}

	got := pendingDict(t, alloc, fontObj)

	// The fixer's own repair must still land: this is not a test that it does
	// nothing, it is a test that it composes.
	if enc := got["Encoding"]; enc != ast.PdfName("WinAnsiEncoding") {
		t.Errorf("/Encoding = %v, want WinAnsiEncoding", enc)
	}

	widths, ok := got["Widths"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Widths = %T, want an array", got["Widths"])
	}
	if len(widths) != 2 || widths[0] != ast.PdfReal(500) || widths[1] != ast.PdfReal(500) {
		t.Errorf("/Widths = %v, want [500 500]: tt-encoding rebuilt the font dictionary "+
			"from the on-disk copy, discarding the earlier fixer's correction", widths)
	}
}
