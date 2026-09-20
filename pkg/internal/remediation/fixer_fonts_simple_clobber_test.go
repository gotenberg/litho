package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// [fixSimpleFont] builds its write from the fontDict its CALLER read out of the
// document (fixer_fonts.go), not from the allocator:
//
//	newFont := make(ast.PdfDict, len(fontDict))
//	maps.Copy(newFont, fontDict)
//	newFont["Widths"] = newWidths
//	alloc.AllocAt(fontRef.ObjectNum, newFont)
//
// It never consults alloc.Pending(), so an earlier fixer's edit to the same
// font object is dropped. fonts runs at registry 144, after cid-gidmap (110),
// font-keys (111), tounicode-ranges (114) and nested-resources (131), all of
// which write font dictionaries.
//
// This path needs a real embedded font program before it writes anything, which
// is why the first fonts clobber test could not reach it: without a cmap,
// compliance.ExpectedSimpleFontWidths returns ok=false and the width repair
// returns early. simpleWidthsPDF supplies one (probeTrueTypeWithCmap), so the
// repair fires here.
//
// The marker is /Name because the fixture does not declare one. A key already
// present on disk would survive inside the stale copy and the test would pass
// for the wrong reason.
//
// [fixSimpleFontToUnicode] runs straight after this for the same font and DOES
// re-read alloc.Pending(), so it carries forward whatever fixSimpleFont
// produced. A missing marker at the end is therefore fixSimpleFont's doing.
func TestFixSimpleFont_KeepsAPeerFixersPendingFontWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// Declared 999 against a program advance of 500: a mismatch far wider than
	// the 1.0 tolerance, so the width repair actually runs.
	doc, alloc := newTestDoc(t, simpleWidthsPDF(999, 500))

	const fontObj = 4
	fontDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: fontObj})
	if fontDict == nil {
		t.Fatalf("fixture font object %d did not resolve", fontObj)
	}
	if _, present := fontDict["Name"]; present {
		t.Fatalf("precondition: the fixture must NOT declare /Name, else the marker proves nothing")
	}

	// Stand in for any earlier font fixer: copy the dict, add one key, AllocAt
	// the same object number.
	peer := make(ast.PdfDict, len(fontDict)+1)
	maps.Copy(peer, fontDict)
	peer["Name"] = ast.PdfName("F1")
	if !alloc.AllocAt(fontObj, peer) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", fontObj)
	}

	violation := compliance.Violation{
		Rule:    compliance.Rule{Clause: "6.2.11.5", TestNumber: 1},
		Context: "font F1 on page 1: char 65 width mismatch (PDF=999.0, font=500.0)",
	}
	if _, err := fixFontIssues(ctx, alloc, doc, Options{},
		[]compliance.Violation{violation}, "PDF/A-2b"); err != nil {
		t.Fatalf("fixFontIssues: %v", err)
	}

	got := pendingDict(t, alloc, fontObj)

	// Self-diagnosing: if the width repair never ran, this proves nothing about
	// clobbering and must say so rather than masquerade as a finding.
	w, ok := got["Widths"].(ast.PdfArray)
	if !ok || len(w) != 1 {
		t.Fatalf("/Widths is %v, want a one-element array; the fixture never reached "+
			"the width repair", got["Widths"])
	}
	if document.Float(w[0]) > 998.0 {
		t.Fatalf("/Widths = %v, still the declared 999: the repair did not run, so this "+
			"test proves nothing about clobbering", document.Float(w[0]))
	}

	if _, kept := got["Name"]; !kept {
		t.Errorf("/Name is gone: fixSimpleFont rebuilt the font dictionary from the "+
			"copy its caller read out of the document and discarded the earlier "+
			"fixer's write; font = %v", got)
	}
}
