package typography

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// symbolicType1CFont builds a symbolic Type 1 font dictionary embedding a
// /FontFile3 (Type1C) program whose built-in encoding names each code in
// names, and whose /Encoding is encoding (omitted when nil).
func symbolicType1CFont(names map[byte]string, encoding ast.PdfObject) ast.PdfDict {
	var glyphs []fontbuild.Glyph
	for _, code := range sortedCodes(names) {
		glyphs = append(glyphs, fontbuild.Glyph{Name: names[code], Advance: 500})
	}
	program := fontbuild.CFF{FontName: "LithoMath", Glyphs: glyphs, Encoding: names}.Build()
	fd := ast.PdfDict{
		"Type":      ast.PdfName("FontDescriptor"),
		"FontName":  ast.PdfName("LithoMath"),
		"Flags":     ast.PdfInt(4),
		"FontFile3": ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Type1C")}, program),
	}
	font := ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("Type1"),
		"BaseFont":       ast.PdfName("LithoMath"),
		"FontDescriptor": fd,
	}
	if encoding != nil {
		font["Encoding"] = encoding
	}
	return font
}

func sortedCodes(names map[byte]string) []byte {
	codes := make([]byte, 0, len(names))
	for c := range 256 {
		if _, ok := names[byte(c)]; ok {
			codes = append(codes, byte(c))
		}
	}
	return codes
}

// TestResolveFont_Type1CBuiltInEncodingReadsTheGlyphLists pins that the codes
// of a Type1C font read their built-in encoding's glyph names through the
// glyph lists a Type 1 program's names read through ([resolveGlyphRune]).
//
// ISO 32000-1, 9.10.2 takes a code through the glyph name the encoding gives
// it to the Adobe Glyph List, which lists "infinity" and "pi". The compliance
// table the CFF reading used leaves out the list's Greek and math names, so
// the CMSY8 subset of verapdf 6-3-8-t01-fail-a read "∞" as the "1" of the
// default encoding. A Dingbats name in a font that is not ZapfDingbats stays
// out, as it does for a Type 1 program.
func TestResolveFont_Type1CBuiltInEncodingReadsTheGlyphLists(t *testing.T) {
	t.Parallel()

	names := map[byte]string{0x00: "minus", 0x31: "infinity", 0x32: "pi", 0x33: "a3"}
	differences := ast.PdfDict{
		"Type":        ast.PdfName("Encoding"),
		"Differences": ast.PdfArray{ast.PdfInt(0x00), ast.PdfName("minus")},
	}
	for _, c := range []struct {
		name     string
		encoding ast.PdfObject
	}{
		{"no Encoding", nil},
		{"Differences without BaseEncoding", differences},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			font, err := ResolveFont(context.Background(),
				&document.Document{Logger: document.NopLogger()}, symbolicType1CFont(names, c.encoding))
			if err != nil {
				t.Fatalf("ResolveFont: %v", err)
			}
			for code, want := range map[byte]rune{0x00: '−', 0x31: '∞', 0x32: 'π'} {
				if got := font.Encoding[code]; got != want {
					t.Errorf("Encoding[%#x] = %q, want %q", code, got, want)
				}
			}
			if got := font.Encoding[0x33]; got == '✄' {
				t.Errorf("Encoding[0x33] = %q, the Dingbats reading of a font that is not ZapfDingbats", got)
			}
		})
	}
}

// TestResolveFont_Type1CProgramGlyphRunes pins that a Type1C font that states
// no /Encoding records in [Font.ProgramGlyphRunes] the codes its program's
// built-in encoding names with a glyph-list name, as a Type 1 program's do
// (ISO 32000-1, 9.6.6.1 and 9.10.2), and that a font whose /Differences
// renames codes over the built-in encoding records nothing.
func TestResolveFont_Type1CProgramGlyphRunes(t *testing.T) {
	t.Parallel()

	names := map[byte]string{0x00: "minus", 0x31: "infinity", 0x32: "H11021", 0x62: "C98"}
	differences := ast.PdfDict{
		"Type":        ast.PdfName("Encoding"),
		"Differences": ast.PdfArray{ast.PdfInt(0x31), ast.PdfName("one")},
	}
	for _, c := range []struct {
		name     string
		encoding ast.PdfObject
		want     map[byte]rune
	}{
		{"no Encoding", nil, map[byte]rune{0x00: '−', 0x31: '∞'}},
		{"Differences without BaseEncoding", differences, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			font, err := ResolveFont(context.Background(),
				&document.Document{Logger: document.NopLogger()}, symbolicType1CFont(names, c.encoding))
			if err != nil {
				t.Fatalf("ResolveFont: %v", err)
			}
			if !maps.Equal(font.ProgramGlyphRunes, c.want) {
				t.Errorf("ProgramGlyphRunes = %v, want %v", font.ProgramGlyphRunes, c.want)
			}
		})
	}
}
