package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// streamObj renders a content stream object with the /Length its body needs.
func streamObj(body string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
}

// parseOps turns content stream source into operations.
func parseOps(t *testing.T, src string) []content.Operation {
	t.Helper()
	ops, err := content.ParseStreamData(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("parse ops: %v", err)
	}
	return ops
}

// TestCheckType3CharProcFonts_NestedUnembeddedFont pins that a font reached
// only through a Type3 glyph procedure is checked. A Type3 procedure is a
// content stream (ISO 32000-1 §9.6.5) that may select fonts from the Type3
// font's own /Resources, and the page walk stops at the Type3 font itself, so
// an unembedded font used there would otherwise never answer ISO 19005-2
// §6.2.11.4.1, which veraPDF reports.
//
// # Corpus evidence
//
//   - pdfjs ContentStreamCycleType3insideType3.pdf: reaches an unembedded
//     Helvetica through two nested Type3 fonts, and veraPDF reports it.
func TestCheckType3CharProcFonts_NestedUnembeddedFont(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /T3 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 750 750] /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /CharProcs 5 0 R /Encoding << /Differences [97 /rect] >> /FirstChar 97 /LastChar 97"+
			" /Widths [1000] /Resources << /Font << /F1 6 0 R >> >> >>",
		"<< /rect 7 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		streamObj("1000 0 d0 BT /F1 50 Tf (x) Tj ET"),
	)

	page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}
	res, err := document.ResolveDict(ctx, doc, page["Resources"])
	if err != nil {
		t.Fatalf("resolve resources: %v", err)
	}

	// The page shows code 97, whose procedure selects the unembedded Helvetica.
	got := checkType3CharProcFonts(ctx, doc, parseOps(t, "BT /T3 12 Tf (a) Tj ET"), res, showSite{}, nil,
		1, "PDF/A-2b", newFontJudgements(), map[int]bool{}, 0)
	if !hasRule(got, Rule_19005_2_6_2_11_4_1_t01) {
		t.Errorf("unembedded font inside a Type3 procedure must fire §6.2.11.4.1 t1, got %v", got)
	}

	// Every procedure of a font text is shown with is judged, the glyph's the
	// page shows or not: code 98 selects no procedure, and the nested
	// Helvetica is still reached through /rect. Measured with veraPDF 1.30 at
	// PDF/A-1b and PDF/A-2b; see [checkType3CharProcFonts].
	unshown := checkType3CharProcFonts(ctx, doc, parseOps(t, "BT /T3 12 Tf (b) Tj ET"), res, showSite{}, nil,
		1, "PDF/A-2b", newFontJudgements(), map[int]bool{}, 0)
	if !hasRule(unshown, Rule_19005_2_6_2_11_4_1_t01) {
		t.Errorf("an unshown glyph's procedure is judged too; got %v", unshown)
	}

	// A font shown in rendering mode 3 has its procedures judged in that mode,
	// where the Helvetica they show paints nothing, so the embedding rule
	// passes it.
	invisible := checkType3CharProcFonts(ctx, doc, parseOps(t, "BT 3 Tr /T3 12 Tf (a) Tj ET"), res, showSite{}, nil,
		1, "PDF/A-2b", newFontJudgements(), map[int]bool{}, 0)
	if hasRule(invisible, Rule_19005_2_6_2_11_4_1_t01) {
		t.Errorf("a font shown in mode 3 inside a mode-3 procedure is not rendered; got %v", invisible)
	}
}

// TestCheckType3GlyphPresence pins glyph presence for a Type3 font, whose
// glyph programs are its /CharProcs. Per ISO 32000-1 §9.6.5 a code is drawn by
// mapping it through /Encoding to a name and that name through /CharProcs, so
// a code the encoding leaves undefined paints nothing. veraPDF reports that as
// ISO 19005-2 §6.2.11.4.1 t2.
//
// # Corpus evidence
//
//   - pdfjs Type3WordSpacing.pdf: shows a space its font never defines, and
//     veraPDF reports it.
func TestCheckType3GlyphPresence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 750 750] /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /CharProcs 5 0 R /Encoding << /Differences [97 /square /triangle] >>"+
			" /FirstChar 97 /LastChar 98 /Widths [1000 1000] >>",
		"<< /square 6 0 R /triangle 7 0 R >>",
		streamObj("1000 0 d0 0 0 750 750 re f"),
		streamObj("1000 0 d0 0 0 m 375 750 l 750 0 l f"),
	)

	font, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolve font: %v", err)
	}

	presence := func(codes ...byte) []Violation {
		set := make(map[byte]bool, len(codes))
		for _, c := range codes {
			set[c] = true
		}
		return checkSimpleGlyphPresence(ctx, doc, font, "T3", 1, "PDF/A-2b",
			&fontUsageInfo{visible: true, charCodes: set})
	}

	// Both defined codes draw a procedure the font holds.
	if v := presence('a', 'b'); len(v) != 0 {
		t.Errorf("codes 97 and 98 name procedures the font defines, got %v", v)
	}
	// A space is outside /Differences and names no procedure.
	if v := presence('a', ' '); !hasRule(v, Rule_19005_2_6_2_11_4_1_t02) {
		t.Errorf("a space the Type3 font never defines must fire §6.2.11.4.1 t2, got %v", v)
	}
	// Mode 3 paints nothing, so presence is not asked.
	hidden := checkSimpleGlyphPresence(ctx, doc, font, "T3", 1, "PDF/A-2b",
		&fontUsageInfo{visible: false, charCodes: map[byte]bool{' ': true}})
	if len(hidden) != 0 {
		t.Errorf("an invisible Type3 font renders no glyph; got %v", hidden)
	}
}

// TestCheckType3GlyphPresence_UnresolvableStaysQuiet pins the conservative
// side: presence is unanswerable without /CharProcs or an /Encoding, so a font
// missing either is left unchecked rather than reported.
func TestCheckType3GlyphPresence_UnresolvableStaysQuiet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		// No /CharProcs and no /Encoding.
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 750 750] /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /FirstChar 97 /LastChar 97 /Widths [1000] >>",
	)
	font, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolve font: %v", err)
	}

	got := checkSimpleGlyphPresence(ctx, doc, font, "T3", 1, "PDF/A-2b",
		&fontUsageInfo{visible: true, charCodes: map[byte]bool{'a': true}})
	if len(got) != 0 {
		t.Errorf("presence is unanswerable without /CharProcs, got %v", got)
	}
}
