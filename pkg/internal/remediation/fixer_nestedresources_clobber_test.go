package remediation

import (
	"compress/zlib"
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
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

// TestFixNestedResources_KeepsAPeerFixersPendingFontWrite pins that the Type 3
// font repair reads the font dictionary through the allocator. The registry
// drives every fixer against ONE allocator with no flush between them, so a
// fixer that rewrites a whole dictionary must read it through the allocator or
// it discards what the fixers before it wrote to the same object.
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

// TestFixNestedResources_KeepsAnEarlierFixersPendingResourcesWrite pins that
// the nested-resources repair copies a form's or a Type 3 font's own
// /Resources, and the /ColorSpace dictionary inside it, from the versions
// earlier fixers queued.
//
// The repair builds the owner a new /Resources inline: its own resources plus
// the entries only the page defines (ISO 19005-2, 6.2.2). It read the owner's
// /Resources and each resource sub-dictionary from the document, so when either
// was indirect and an earlier fixer had rewritten it, the inline copy dropped
// that write and orphaned the pending object. Three shapes, three real peers,
// all ahead of nested-resources (registry 131):
//
//	form resources   output-intent (68) injects /DefaultRGB into the form's
//	                 indirect /Resources, object 5.
//	type 3 resources the same injection reaches a Type 3 font's indirect
//	                 /Resources through patchType3FontColorSpace.
//	form colour      icc-dedup (117) substitutes /CS0 in the form's indirect
//	space            /ColorSpace, object 6.
//
// /CSP is defined only on the page and painted by the owner, so the repair has
// to run, and its presence in the owner's resources proves it landed there.
func TestFixNestedResources_KeepsAnEarlierFixersPendingResourcesWrite(t *testing.T) {
	t.Parallel()

	const calGray = "<< /WhitePoint [0.9505 1 1.089] >>"
	formStream := func(res, content string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources %s /Length %d >>"+
			"\nstream\n%s\nendstream", res, len(content), content)
	}
	cmyk := deflateAt(t, cmykICCProfile, zlib.BestCompression)

	for _, tc := range []struct {
		name     string
		objs     []string
		ownerObj int
		peer     func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document)
		kept     func(cs ast.PdfDict) bool
	}{
		{
			name: "form resources",
			objs: []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
					" /Resources << /ColorSpace << /CSP [/CalGray 6 0 R] >> /XObject << /Fm0 4 0 R >> >> >>",
				formStream("5 0 R", "q /CSP cs 0.5 sc 0 0 5 5 re f Q\n"),
				"<< /ProcSet [/PDF] >>",
				calGray,
			},
			ownerObj: 4,
			peer: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				t.Helper()
				injectTestDefaultRGB(t, alloc, doc)
				if _, ok := alloc.Pending()[5]; !ok {
					t.Fatalf("control: output-intent did not rewrite the form's /Resources, object 5")
				}
			},
			kept: func(cs ast.PdfDict) bool { return cs["DefaultRGB"] != nil },
		},
		{
			name: "type 3 resources",
			objs: []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
					" /Resources << /Font << /F3 4 0 R >> /ColorSpace << /CSP [/CalGray 5 0 R] >> >> >>",
				"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0]" +
					" /CharProcs << /a 6 0 R >> /Encoding << >> /FirstChar 97 /LastChar 97 /Widths [10]" +
					" /Resources 7 0 R >>",
				calGray,
				"<< /Length 16 >>\nstream\nq /CSP cs 0.5 sc\nendstream",
				"<< /ProcSet [/PDF] >>",
			},
			ownerObj: 4,
			peer: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				t.Helper()
				injectTestDefaultRGB(t, alloc, doc)
				if _, ok := alloc.Pending()[7]; !ok {
					t.Fatalf("control: output-intent did not rewrite the font's /Resources, object 7")
				}
			},
			kept: func(cs ast.PdfDict) bool { return cs["DefaultRGB"] != nil },
		},
		{
			name: "form colour space",
			objs: []string{
				"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
					" /Resources << /ColorSpace << /CSP [/CalGray 8 0 R] >> /XObject << /Fm0 5 0 R >> >> >>",
				"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (Coated FOGRA39)" +
					" /DestOutputProfile 7 0 R >>",
				formStream("<< /ColorSpace 6 0 R >>", "q /CS0 cs 0 0 0 1 sc /CSP cs 0.5 sc 0 0 5 5 re f Q\n"),
				"<< /CS0 [/ICCBased 9 0 R] >>",
				iccObj(cmyk, 4),
				calGray,
				iccObj(cmyk, 4),
			},
			ownerObj: 5,
			peer: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
				t.Helper()
				if _, err := fixICCDedup(context.Background(), alloc, doc, Options{}, nil, ""); err != nil {
					t.Fatalf("fixICCDedup: %v", err)
				}
				if _, ok := alloc.Pending()[6]; !ok {
					t.Fatalf("control: icc-dedup did not rewrite the form's /ColorSpace, object 6")
				}
			},
			kept: func(cs ast.PdfDict) bool { name, _ := cs["CS0"].(ast.PdfName); return name == "DeviceCMYK" },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, assemblePDF(tc.objs))

			tc.peer(t, alloc, doc)

			if _, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixNestedResources: %v", err)
			}

			cs := liveDictValue(ctx, alloc, doc, liveOwnerResources(t, alloc, doc, tc.ownerObj)["ColorSpace"])
			if cs["CSP"] == nil {
				t.Fatalf("object %d's /ColorSpace has no /CSP: the repair did not land where the "+
					"output reads it, so this test proves nothing; /ColorSpace = %v", tc.ownerObj, cs)
			}
			if !tc.kept(cs) {
				t.Errorf("object %d's /ColorSpace lost the earlier fixer's write: the repair "+
					"copied the document's version; /ColorSpace = %v", tc.ownerObj, cs)
			}
		})
	}
}
