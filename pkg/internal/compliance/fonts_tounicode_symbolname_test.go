package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// TestToUnicode_FontNameGrantsNoExemption pins that a font's name plays no part
// in the ToUnicode presence rule. ISO 19005-2, 6.2.11.7.2 asks a Unicode value
// of every shown glyph, which ISO 32000-1, 9.10.2 derives from the glyph name
// the encoding gives the code. A /BaseFont that merely contains "Symbol" says
// nothing about the glyph a code selects, so a code the encoding names nothing
// for has no Unicode value whatever the font is called.
//
// The fonts embed a nonsymbolic Type 1 program litho cannot read and differ
// only in their name. Their /Differences array names code 65 alpha and leaves
// code 128 unnamed, with no /BaseEncoding to fall back on. veraPDF 1.30.2
// reports code 128 of both, under ISO 19005-1 6.3.8 t1, ISO 19005-2 6.2.11.7.2
// t1 and ISO 14289-1 7.21.7 t1 (Glyph toUnicode != null), and passes code 65.
func TestToUnicode_FontNameGrantsNoExemption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		font  string
		code  byte
		fires bool
	}{
		{"Symbol in the name, unnamed code", "MdSymbolA-Regular", 128, true},
		{"no Symbol in the name, unnamed code", "MdGlyphsA-Regular", 128, true},
		{"Symbol in the name, code named alpha", "MdSymbolA-Regular", 65, false},
		{"no Symbol in the name, code named alpha", "MdGlyphsA-Regular", 65, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /Type1 /BaseFont /"+tt.font+
					" /FirstChar 65 /LastChar 65 /Widths [500] /FontDescriptor 3 0 R"+
					" /Encoding << /Type /Encoding /Differences [65 /alpha] >> >>",
				"<< /Type /FontDescriptor /FontName /"+tt.font+" /Flags 32 /ItalicAngle 0"+
					" /Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000]"+
					" /FontFile 4 0 R >>",
				"<< /Length 22 /Length1 22 /Length2 0 /Length3 0 >>\nstream\n%!PS-AdobeFont-1.0: X\n\nendstream",
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{tt.code: true}}

			for _, profile := range []string{"PDF/A-1a", "PDF/A-2u", "PDF/UA-1"} {
				got := checkToUnicode(ctx, doc, fontDict, "F1", 1, profile, usage)
				if fired := len(got) > 0; fired != tt.fires {
					t.Errorf("%s: ToUnicode violation fired = %v, want %v (%v)", profile, fired, tt.fires, got)
				}
			}
		})
	}
}

// TestToUnicode_SymbolCharSetGrantsNoExemption pins that a /CharSet listing
// only Symbol-font names exempts no shown glyph. The rule judges the glyph each
// shown code selects, and ISO 32000-1, Table 122 makes /CharSet meaningful for
// Type 1 fonts alone, so a TrueType font's /CharSet says nothing about its
// glyphs.
//
// The font is the one litho corpus symbolname_2u.pdf plants: a nonsymbolic
// TrueType named MdSymbolA-Regular whose /Differences name code 66 H20850, a
// name with no Unicode value in the Adobe Glyph List, the Symbol font or
// ZapfDingbats. veraPDF 1.30.2 reports code 66 at every level that runs the
// rule, and passes code 65, named A.
func TestToUnicode_SymbolCharSetGrantsNoExemption(t *testing.T) {
	t.Parallel()

	program := fontbuild.TrueType{
		PostScriptName: "MdSymbolA-Regular", Family: "MdSymbolA", Subfamily: "Regular",
		Glyphs: []fontbuild.Glyph{{Name: "A", Unicode: 'A', Advance: 600}, {Name: "H20850", Advance: 600}},
	}.Build()

	for _, tt := range []struct {
		name  string
		font  string
		code  byte
		fires bool
	}{
		{"Symbol in the name, code named H20850", "MdSymbolA-Regular", 66, true},
		{"no Symbol in the name, code named H20850", "MdGlyphsA-Regular", 66, true},
		{"Symbol in the name, code named A", "MdSymbolA-Regular", 65, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /TrueType /BaseFont /"+tt.font+
					" /FirstChar 65 /LastChar 66 /Widths [600 600] /FontDescriptor 3 0 R"+
					" /Encoding << /Type /Encoding /Differences [65 /A /H20850] >> >>",
				"<< /Type /FontDescriptor /FontName /"+tt.font+" /Flags 32 /ItalicAngle 0"+
					" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 -200 550 700]"+
					" /CharSet (/alpha/beta) /FontFile2 4 0 R >>",
				fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(program), len(program), program),
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{tt.code: true}}

			for _, profile := range []string{"PDF/A-1a", "PDF/A-2u", "PDF/UA-1"} {
				got := checkToUnicode(ctx, doc, fontDict, "F1", 1, profile, usage)
				if fired := len(got) > 0; fired != tt.fires {
					t.Errorf("%s: ToUnicode violation fired = %v, want %v (%v)", profile, fired, tt.fires, got)
				}
			}
		})
	}
}
