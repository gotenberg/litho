package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckType3FontTransparency_SoftMaskedImage pins that a glyph procedure
// painting a soft-masked image makes the page transparent. A Type3 procedure
// draws XObjects from the font's own /Resources, and an image /SMask is
// transparency wherever it is painted (ISO 32000-1 §11.6.5); veraPDF counts it
// toward the page's containsTransparency for ISO 19005-2 §6.2.10 t2.
//
// # Corpus evidence
//
//   - pdfjs issue10717.pdf: its /SMask images sit in the Type3 font
//     resources, and veraPDF counts them toward containsTransparency.
func TestCheckType3FontTransparency_SoftMaskedImage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	image := func(smask string) string {
		return "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 " +
			"/ColorSpace /DeviceGray " + smask + " /Length 0 >>\nstream\nendstream"
	}

	build := func(t *testing.T, imgObj string) bool {
		t.Helper()
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
				" /Resources << /Font << /T3 5 0 R >> >> >>",
			streamObj("BT /T3 12 Tf (a) Tj ET"),
			"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 750 750] /FontMatrix [0.001 0 0 0.001 0 0]"+
				" /CharProcs 6 0 R /Encoding << /Differences [97 /g] >> /FirstChar 97 /LastChar 97"+
				" /Widths [1000] /Resources << /XObject << /Im 7 0 R >> >> >>",
			"<< /g 8 0 R >>",
			imgObj,
			streamObj("1000 0 d0 /Im Do"),
			image(""), // the soft-mask target
		)
		page := collectPages(ctx, doc, cat)[0]
		return checkType3FontTransparency(ctx, doc, &pageCtxsForPages(ctx, doc, page)[0], resolveResources(ctx, doc, page))
	}

	if !build(t, image("/SMask 9 0 R")) {
		t.Error("a glyph procedure painting an /SMask image is transparency")
	}
	if build(t, image("")) {
		t.Error("a glyph procedure painting a plain image uses no transparency")
	}
}

// TestPageHasBlendingCS_NullCS pins that a /Group /CS resolving to null counts
// as no blending colour space. Per ISO 32000-1 §7.3.9 a reference to a
// non-existent object is the null object, and per §7.3.7 a null-valued entry is
// equivalent to an absent one, so a group pointing /CS at a free object still
// owes ISO 19005-2 §6.2.10 t2 an answer.
//
// # Corpus evidence
//
//   - pdfjs issue11045.pdf: points its group /CS at a free object, and
//     veraPDF fails it.
func TestPageHasBlendingCS_NullCS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	has := func(t *testing.T, group string) bool {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Group "+group+" >>",
		)
		page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
		if err != nil {
			t.Fatalf("resolve page: %v", err)
		}
		return pageHasBlendingCS(ctx, doc, page)
	}

	if has(t, "<< /S /Transparency /CS 99 0 R >>") {
		t.Error("a /CS pointing at a free object is null, so no blending colour space is set")
	}
	if !has(t, "<< /S /Transparency /CS /DeviceRGB >>") {
		t.Error("a named /CS is a blending colour space")
	}
	if has(t, "<< /S /Transparency >>") {
		t.Error("a group with no /CS has no blending colour space")
	}
}
