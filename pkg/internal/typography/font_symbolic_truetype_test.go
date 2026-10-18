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

// TestResolveFont_SymbolicTrueTypeWithACopiedMacSubtable pins that a symbolic
// TrueType font with no /Encoding and no (3,0) subtable reads its codes as the
// glyphs they select only when its (1,0) subtable copies the (3,1) subtable
// over 0x80-0xFF, and keeps the Mac OS Roman reading of a genuine (1,0)
// subtable.
func TestResolveFont_SymbolicTrueTypeWithACopiedMacSubtable(t *testing.T) {
	copied := []fontbuild.Glyph{
		{Name: "glyph00001", Unicode: 0xE4, Mac: 0xE4, Advance: 500},
		{Name: "glyph00002", Unicode: 0x80, Mac: 0x80, Advance: 500},
	}
	for _, c := range []struct {
		name   string
		glyphs []fontbuild.Glyph
		code   byte
		want   string
	}{
		{
			// Mac OS Roman reads 0xE4 as "‰"; the glyph is the one (3,1) maps
			// U+00E4 to.
			name:   "copied subtable reads the Unicode subtable",
			glyphs: copied,
			code:   0xE4,
			want:   "ä",
		},
		{
			// U+0080 is a control character: the code keeps the WinAnsi
			// reading, not the Mac OS Roman "Ä".
			name:   "copied subtable keeps the default for a control character",
			glyphs: copied,
			code:   0x80,
			want:   "€",
		},
		{
			name: "genuine subtable keeps Mac OS Roman",
			glyphs: []fontbuild.Glyph{
				{Name: "glyph00001", Unicode: 0x2030, Mac: 0xE4, Advance: 500},
				{Name: "glyph00002", Unicode: 0xC4, Mac: 0x80, Advance: 500},
			},
			code: 0xE4,
			want: "‰",
		},
		{
			// One code that disagrees makes the subtable a Mac mapping.
			name: "one independent code keeps Mac OS Roman",
			glyphs: []fontbuild.Glyph{
				{Name: "glyph00001", Unicode: 0xE4, Mac: 0xE4, Advance: 500},
				{Name: "glyph00002", Unicode: 0xC4, Mac: 0x80, Advance: 500},
			},
			code: 0xE4,
			want: "‰",
		},
		{
			// Below 0x80 the two encodings agree, so a copy there says nothing.
			name:   "copy below 0x80 keeps Mac OS Roman",
			glyphs: []fontbuild.Glyph{{Name: "glyph00001", Unicode: 0x41, Mac: 0x41, Advance: 500}},
			code:   0xE4,
			want:   "‰",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			font, err := ResolveFont(context.Background(),
				&document.Document{Logger: document.NopLogger()}, symbolicTrueTypeFont("ABCDEF+Arial", c.glyphs))
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
