package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// simpleFontShowingACodePDF builds a page that SHOWS one character code with a
// simple font carrying an /Encoding and no /ToUnicode, which is what
// [fixSimpleFontToUnicode] repairs.
//
// No font program is needed. The program route is wrapped in nil guards and the
// mapping is built from the /Encoding alone: "The program route needs a cmap
// table, which a Type 1 or bare CFF program does not have. Its absence is not a
// reason to emit nothing" (fixer_fonts.go). That keeps this fixture to plain
// objects, unlike the hand-built sfnt in fixer_simplewidths_test.go.
//
// Object 4 is the font, so a peer fixer's pending write lands there.
func simpleFontShowingACodePDF() []byte {
	const content = "BT /F1 12 Tf 20 700 Td (A) Tj ET\n"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R "+
			"/Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica "+
			"/Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 65 /Widths [722] >>",
		"<< /Length 33 >>\nstream\n"+content+"endstream",
	)
}

// TestFixFontIssues_KeepsAPeerFixersPendingFontWrite pins that the /ToUnicode
// repair COMPOSES with an earlier fixer's write to the same font dictionary.
// This test passes today and is here to keep it that way.
//
// [fixFontIssues] reads the font dictionary once from the document
// (fixer_fonts.go) and hands that value to three writers, which looks like the
// stale-read-then-AllocAt shape that produced several real defects elsewhere in
// this package. It is not one here: [fixSimpleFontToUnicode] rebuilds its write
// from alloc.Pending() at the write site and ignores the parameter, so the
// earlier write survives. Nothing enforces that from outside the function, and
// the registry gives it plenty to survive: fonts runs at 144, after cid-gidmap
// (110), font-keys (111), tounicode-ranges (114) and nested-resources (131),
// every one of which rewrites font dictionaries.
//
// The pending write below stands in for any of them, because provoking a real
// peer needs a second violation on the same object.
//
// NOTE the limit of this test: it exercises the /ToUnicode path only.
// [fixSimpleFont] builds its write from the stale parameter instead, and
// reaching that write needs an embedded font program, so it is neither proven
// nor cleared by what follows.
func TestFixFontIssues_KeepsAPeerFixersPendingFontWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, simpleFontShowingACodePDF())

	const fontObj = 4
	fontDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: fontObj})
	if fontDict == nil {
		t.Fatalf("fixture font object %d did not resolve", fontObj)
	}

	// Stand in for an earlier font fixer: copy the dict, add one key, AllocAt
	// the same object number. /CIDToGIDMap is what cid-gidmap writes.
	peer := make(ast.PdfDict, len(fontDict)+1)
	maps.Copy(peer, fontDict)
	peer["CIDToGIDMap"] = ast.PdfName("Identity")
	if !alloc.AllocAt(fontObj, peer) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", fontObj)
	}

	if _, err := fixFontIssues(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixFontIssues: %v", err)
	}

	got := pendingDict(t, alloc, fontObj)

	// Self-diagnosing: if the repair never ran, say so rather than reporting a
	// clobber that never happened. A dead fixture must not masquerade as a bug.
	if _, ok := got["ToUnicode"]; !ok {
		t.Fatalf("no /ToUnicode was attached, so the repair did not run and this test "+
			"proves nothing about clobbering; font = %v", got)
	}

	if _, kept := got["CIDToGIDMap"]; !kept {
		t.Errorf("/CIDToGIDMap is gone: fixFontIssues read the font dictionary from the "+
			"on-disk copy and discarded the earlier fixer's write; font = %v", got)
	}
}
