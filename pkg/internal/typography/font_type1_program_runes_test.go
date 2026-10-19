package typography

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolveFont_Type1BuiltInHoleReadsTheGlyphLists pins that a code a Type 1
// font's stated /Encoding leaves undefined reads the glyph name its program's
// built-in encoding gives it through the glyph lists every built-in encoding
// reads through ([builtinGlyphRune]): "infinity" is the Adobe Glyph List's
// U+221E (ISO 32000-1, 9.10.2), and a Dingbats name in a font that is not
// ZapfDingbats names nothing.
func TestResolveFont_Type1BuiltInHoleReadsTheGlyphLists(t *testing.T) {
	t.Parallel()

	names := map[int]string{0x01: "infinity", 0x02: "a3"}
	font, err := ResolveFont(context.Background(),
		&document.Document{Logger: document.NopLogger()}, numericNamedType1Font(names, ast.PdfName("WinAnsiEncoding")))
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got := font.Encoding[0x01]; got != '∞' {
		t.Errorf("Encoding[0x01] = %q, want '∞'", got)
	}
	if got, ok := font.Encoding[0x02]; ok && got == '✄' {
		t.Errorf("Encoding[0x02] = %q, the Dingbats reading of a font that is not ZapfDingbats", got)
	}
}

// TestResolveFont_Type1ProgramGlyphRunes pins which codes of a Type 1 font
// [Font.ProgramGlyphRunes] records: those the program's built-in encoding
// names with a glyph-list name, when the font states no /Encoding (ISO
// 32000-1, 9.6.6.1 and 9.10.2). A numeric name and a name no glyph list
// carries are left out, though text extraction reads the first, and a font
// whose /Differences renames codes over the built-in encoding records nothing.
func TestResolveFont_Type1ProgramGlyphRunes(t *testing.T) {
	t.Parallel()

	names := map[int]string{0x00: "minus", 0x31: "infinity", 0x32: "H11021", 0x62: "C98"}
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
				&document.Document{Logger: document.NopLogger()}, numericNamedType1Font(names, c.encoding))
			if err != nil {
				t.Fatalf("ResolveFont: %v", err)
			}
			if !maps.Equal(font.ProgramGlyphRunes, c.want) {
				t.Errorf("ProgramGlyphRunes = %v, want %v", font.ProgramGlyphRunes, c.want)
			}
		})
	}
}
