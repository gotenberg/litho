package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// type3FontSharingPageResourcesPDF builds a page declaring a Type 3 font whose
// glyph procedure paints with a colour space only the PAGE defines, which is
// the shape [fixType3FontResources] repairs: the font needs a /Resources of its
// own (ISO 32000-1, 9.6.5 scopes it to the CharProcs).
//
// Object 4 is the font, so a peer fixer's pending write lands there too.
func type3FontSharingPageResourcesPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F3 4 0 R >> /ColorSpace << /CS0 [/CalGray 5 0 R] >> >> >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] "+
			"/FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << /a 6 0 R >> "+
			"/Encoding << >> /FirstChar 97 /LastChar 97 /Widths [10] >>",
		"<< /WhitePoint [0.9505 1 1.089] >>",
		"<< /Length 16 >>\nstream\nq /CS0 cs 0.5 sc\nendstream",
	)
}

// The registry drives every fixer against ONE allocator with no flush between
// them, so a fixer that rewrites a whole dictionary must read it through the
// allocator or it discards what the fixers before it wrote to the same object.
//
// nested-resources (registry 131) rewrites the Type 3 FONT dictionary, and it
// runs after cid-gidmap (110), font-keys (111) and tounicode-ranges (114),
// every one of which writes font dictionaries. It also runs after transparency
// (107), which rewrites the same Type 3 font dict in fixType3FontTransparency.
//
// The pending write here stands in for any of them: provoking a real peer would
// need a font program and a second violation, while seeding the allocator
// reproduces exactly the state the peer leaves behind.
func TestFixNestedResources_KeepsAPeerFixersPendingFontWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, type3FontSharingPageResourcesPDF())

	const fontObj = 4
	fontDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: fontObj})
	if fontDict == nil {
		t.Fatalf("fixture font object %d did not resolve", fontObj)
	}

	// Stand in for an earlier font fixer: copy the dict, add one key, AllocAt
	// the same object number. /ToUnicode is what tounicode-ranges attaches.
	peer := make(ast.PdfDict, len(fontDict)+1)
	maps.Copy(peer, fontDict)
	peer["ToUnicode"] = ast.PdfIndirectRef{ObjectNum: 99}
	if !alloc.AllocAt(fontObj, peer) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", fontObj)
	}

	if _, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}

	got := pendingDict(t, alloc, fontObj)

	// The fixer's own job must still be done: this is a composition test, not
	// an assertion that it does nothing.
	if _, ok := got["Resources"]; !ok {
		t.Errorf("the Type 3 font gained no /Resources, so the repair did not run: %v", got)
	}

	if _, kept := got["ToUnicode"]; !kept {
		t.Errorf("/ToUnicode is gone: fixType3FontResources rebuilt the font dictionary "+
			"from the on-disk copy and discarded the earlier fixer's write; font = %v", got)
	}
}
