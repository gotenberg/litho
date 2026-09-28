package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// judgedTrueType returns a TrueType font dictionary over WinAnsiEncoding
// declaring widths for "A" and "B", holding its descriptor and program
// directly, and the program drawing "A" at advanceA.
func judgedTrueType(advanceA uint16) ast.PdfDict {
	glyphs := []fontbuild.Glyph{
		{Name: "A", Unicode: 'A', Advance: advanceA},
		{Name: "B", Unicode: 'B', Advance: 667},
	}
	program := fontbuild.TrueType{PostScriptName: "TestSans", Family: "Test Sans", Subfamily: "Regular", Glyphs: glyphs}.Build()
	return ast.PdfDict{
		"Type":      ast.PdfName("Font"),
		"Subtype":   ast.PdfName("TrueType"),
		"BaseFont":  ast.PdfName("TestSans"),
		"Encoding":  ast.PdfName("WinAnsiEncoding"),
		"FirstChar": ast.PdfInt(65),
		"LastChar":  ast.PdfInt(66),
		"Widths":    ast.PdfArray{ast.PdfInt(722), ast.PdfInt(667)},
		"FontDescriptor": ast.PdfDict{
			"Type":        ast.PdfName("FontDescriptor"),
			"FontName":    ast.PdfName("TestSans"),
			"Flags":       ast.PdfInt(32),
			"FontBBox":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1000), ast.PdfInt(700)},
			"ItalicAngle": ast.PdfInt(0),
			"Ascent":      ast.PdfInt(800),
			"Descent":     ast.PdfInt(-200),
			"CapHeight":   ast.PdfInt(700),
			"StemV":       ast.PdfInt(80),
			"FontFile2":   ast.NewStream(ast.PdfDict{"Length1": ast.PdfInt(len(program))}, program),
		},
	}
}

func TestJudgeFontShowing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
	shown := []ast.PdfString{"AB"}
	const profile = "PDF/A-2b"

	unembedded := ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("Helvetica")}
	if got := JudgeFontShowing(ctx, doc, unembedded, shown, profile); !hasRule(got, FontEmbeddingRule(profile)) {
		t.Errorf("unembedded Helvetica: want the embedding rule, got %v", got)
	}

	for _, tc := range []struct {
		name  string
		font  ast.PdfDict
		width bool
	}{
		{"conforming", judgedTrueType(722), false},
		{"wrong advance", judgedTrueType(600), true},
	} {
		got := JudgeFontShowing(ctx, doc, tc.font, shown, profile)
		if hasRule(got, FontEmbeddingRule(profile)) {
			t.Errorf("%s: the embedded program reads as unembedded: %v", tc.name, got)
		}
		if hasRule(got, GlyphPresenceRule(profile)) {
			t.Errorf("%s: presence rule fired: %v", tc.name, got)
		}
		if hasRule(got, GlyphWidthRule(profile)) != tc.width {
			t.Errorf("%s: width rule = %v, want %v (%v)", tc.name, !tc.width, tc.width, got)
		}
	}
}

func TestUnembeddedFontGlyphNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
	for _, tc := range []struct {
		name string
		font ast.PdfDict
		code byte
		want string
	}{
		{"standard base", ast.PdfDict{"BaseFont": ast.PdfName("Helvetica")}, 0x27, "quoteright"},
		{"winansi", ast.PdfDict{"BaseFont": ast.PdfName("Helvetica"), "Encoding": ast.PdfName("WinAnsiEncoding")}, 0x27, "quotesingle"},
		{"differences over standard", ast.PdfDict{"BaseFont": ast.PdfName("Helvetica"), "Encoding": ast.PdfDict{
			"Differences": ast.PdfArray{ast.PdfInt(0x41), ast.PdfName("Aring")},
		}}, 0x41, "Aring"},
		{"symbol", ast.PdfDict{"BaseFont": ast.PdfName("Symbol")}, 0x61, "alpha"},
		{"dingbats", ast.PdfDict{"BaseFont": ast.PdfName("ZapfDingbats")}, 0x21, "a1"},
	} {
		if got := UnembeddedFontGlyphNames(ctx, doc, tc.font)[tc.code]; got != tc.want {
			t.Errorf("%s: code %#x names %q, want %q", tc.name, tc.code, got, tc.want)
		}
	}
	symbolic := ast.PdfDict{"BaseFont": ast.PdfName("Wingdings"), "FontDescriptor": ast.PdfDict{"Flags": ast.PdfInt(4)}}
	if got := UnembeddedFontGlyphNames(ctx, doc, symbolic); got != nil {
		t.Errorf("symbolic font: want unknown names, got %v", got)
	}
}
