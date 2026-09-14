package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestToUnicodeZapfDingbatsPart1 pins that a ZapfDingbats glyph earns no
// name-based exemption from the PDF/A-1 /ToUnicode requirement, and that parts 2
// and later keep theirs.
//
// Its glyphs are named a1 to a191. ISO 32000-1 §9.10.2 derives a Unicode value
// from "the Adobe standard Latin character set or the set of named characters in
// the Symbol font" and names no third set, so those names carry none. The Adobe
// Glyph List does map them, which is why [glyphNameHasUnicode] answers true and
// every name survey in [checkToUnicode] passed such a font.
//
// The part split is measured, not assumed: veraPDF runs the same test
// (`toUnicode != null` on a Glyph) in both profiles, yet on govdocs
// thread_100_100601.pdf it reports the /a73 glyph at PDF/A-1a and passes it at
// PDF/A-2u. Extending the carve-out past part 1 reported four files veraPDF
// passes.
func TestToUnicodeZapfDingbatsPart1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(t *testing.T, baseFont string) (*document.Document, ast.PdfDict) {
		t.Helper()

		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /"+baseFont+
				" /FirstChar 1 /LastChar 1 /Widths [788] /Encoding 3 0 R /FontDescriptor 4 0 R >>",
			"<< /Type /Encoding /Differences [1 /a73] >>",
			// Flags bit 3 (value 4) marks the font symbolic.
			"<< /Type /FontDescriptor /FontName /"+baseFont+" /Flags 4 >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		return doc, fontDict
	}
	usage := func() *fontUsageInfo {
		return &fontUsageInfo{visible: true, charCodes: map[byte]bool{1: true}}
	}

	cases := []struct {
		name     string
		baseFont string
		profile  string
		fires    bool
	}{
		{"an embedded subset under part 1", "MHHIBN+ZapfDingbats", "PDF/A-1a", true},
		{"the plain spelling under part 1", "ZapfDingbats", "PDF/A-1a", true},
		{"part 2 keeps the exemption", "MHHIBN+ZapfDingbats", "PDF/A-2u", false},
		{"part 3 keeps the exemption", "MHHIBN+ZapfDingbats", "PDF/A-3u", false},
		{"another symbolic font is untouched", "MHHIBN+Symbol", "PDF/A-1a", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, fontDict := build(t, tc.baseFont)
			got := checkToUnicode(ctx, doc, fontDict, "F1", 1, tc.profile, usage())
			if fired := len(got) != 0; fired != tc.fires {
				t.Errorf("%s %s: fired = %v (%v), want %v",
					tc.baseFont, tc.profile, fired, got, tc.fires)
			}
		})
	}
}
