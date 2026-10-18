package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// symbolicTrueTypeFont builds a symbolic TrueType font dictionary that states
// no /Encoding and embeds a program holding glyphs.
func symbolicTrueTypeFont(baseFont string, glyphs []fontbuild.Glyph) ast.PdfDict {
	program := fontbuild.TrueType{PostScriptName: "TEST", Family: "Test", Subfamily: "Regular", Glyphs: glyphs}.Build()
	fd := ast.PdfDict{
		"Type":      ast.PdfName("FontDescriptor"),
		"FontName":  ast.PdfName(baseFont),
		"Flags":     ast.PdfInt(4),
		"FontFile2": ast.NewStream(ast.PdfDict{}, program),
	}
	return ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("TrueType"),
		"BaseFont":       ast.PdfName(baseFont),
		"FontDescriptor": fd,
	}
}

// TestResolveFont_SymbolicTrueTypeReadsItsGlyphs pins that an embedded
// symbolic TrueType font with no /Encoding reads each code as the glyph its
// (3,0) subtable selects (ISO 32000-1, 9.6.6.4): by the post table name, then
// by the program's Unicode subtable, and otherwise as before.
func TestResolveFont_SymbolicTrueTypeReadsItsGlyphs(t *testing.T) {
	for _, c := range []struct {
		name     string
		baseFont string
		glyphs   []fontbuild.Glyph
		code     byte
		want     string
	}{
		{
			// A name holding "Symbol" no longer selects the Symbol encoding,
			// which read code 0x53 as "Σ".
			name:     "post name over a name-picked built-in encoding",
			baseFont: "ABCDEF+WPTypographicSymbols",
			glyphs:   []fontbuild.Glyph{{Name: "S", Symbol: 0xF053, Advance: 500}},
			code:     0x53,
			want:     "S",
		},
		{
			// An opaque post name defers to the (3,1) subtable, where the
			// WinAnsi default read code 0x41 as "A".
			name:     "Unicode subtable for an opaque post name",
			baseFont: "ABCDEF+Arial",
			glyphs:   []fontbuild.Glyph{{Name: "glyph00001", Symbol: 0xF041, Unicode: 0x03B1, Advance: 500}},
			code:     0x41,
			want:     "α",
		},
		{
			name:     "Symbol subset keeps its reading",
			baseFont: "ABCDEF+Symbol",
			glyphs:   []fontbuild.Glyph{{Name: "bullet", Symbol: 0xF0B7, Advance: 500}},
			code:     0xB7,
			want:     "•",
		},
		{
			name:     "uncharacterised glyph keeps the default",
			baseFont: "ABCDEF+Arial",
			glyphs:   []fontbuild.Glyph{{Name: "glyph00001", Symbol: 0xF042, Advance: 500}},
			code:     0x42,
			want:     "B",
		},
		{
			// A Private Use Area name does not displace a graphic reading.
			name:     "Private Use Area keeps the default",
			baseFont: "ABCDEF+Arial",
			glyphs:   []fontbuild.Glyph{{Name: "uniF043", Symbol: 0xF043, Advance: 500}},
			code:     0x43,
			want:     "C",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			font, err := ResolveFont(context.Background(),
				&document.Document{Logger: document.NopLogger()}, symbolicTrueTypeFont(c.baseFont, c.glyphs))
			if err != nil {
				t.Fatalf("ResolveFont: %v", err)
			}
			got, _, skip := decodeNextChar(font, []byte{c.code})
			if skip || got != c.want {
				t.Errorf("code 0x%02X = %q (skip %v), want %q", c.code, got, skip, c.want)
			}
		})
	}
}
