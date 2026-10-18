package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// TestResolveFont_SymbolTreatmentFollowsTheStandardName pins that only the
// standard Symbol font (ISO 32000-1, 9.6.2.2) reads through the Symbol
// encoding and glyph table: a name merely holding "Symbol" leaves an embedded
// program, or the Adobe Glyph List, to say what its codes are.
func TestResolveFont_SymbolTreatmentFollowsTheStandardName(t *testing.T) {
	unicodeOnly := []fontbuild.Glyph{{Name: "a", Unicode: 'a', Advance: 500}}
	for _, c := range []struct {
		name string
		font ast.PdfDict
		code byte
		want string
	}{
		{
			// No (3,0) subtable: the code reads through the program's (3,1)
			// subtable, where the Symbol encoding read 0x61 as "α".
			name: "embedded font whose name holds Symbol",
			font: symbolicTrueTypeFont("ABCDEF+WPIconicSymbolsA", unicodeOnly),
			code: 0x61,
			want: "a",
		},
		{
			// The Adobe Glyph List names mu U+00B5; the Symbol glyph table,
			// which the name used to select, names it U+03BC.
			name: "unembedded nonsymbolic font whose name holds Symbol",
			font: differencesFont("ItcSymbol-Book", 32, "mu"),
			code: 0x41,
			want: "µ",
		},
		{
			name: "Symbol keeps its glyph table",
			font: differencesFont("Symbol", 4, "mu"),
			code: 0x41,
			want: "μ",
		},
		{
			name: "Symbol with a style suffix keeps its encoding",
			font: ast.PdfDict{
				"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("TrueType"),
				"BaseFont": ast.PdfName("Symbol,Bold"),
			},
			code: 0x61,
			want: "α",
		},
		{
			// With no program to read, a symbolic font named for symbols
			// reads as PDF.js reads it, through the Symbol encoding.
			name: "unembedded symbolic font whose name holds Symbol",
			font: ast.PdfDict{
				"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("TrueType"),
				"BaseFont": ast.PdfName("WPIconicSymbolsA"),
				"FontDescriptor": ast.PdfDict{
					"Type": ast.PdfName("FontDescriptor"), "FontName": ast.PdfName("WPIconicSymbolsA"),
					"Flags": ast.PdfInt(4),
				},
			},
			code: 0x61,
			want: "α",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, c.font)
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

// differencesFont builds an unembedded Type 1 font dictionary whose
// /Differences array names glyph at code 0x41.
func differencesFont(baseFont string, flags int, glyph string) ast.PdfDict {
	return ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName(baseFont),
		"FontDescriptor": ast.PdfDict{
			"Type":     ast.PdfName("FontDescriptor"),
			"FontName": ast.PdfName(baseFont),
			"Flags":    ast.PdfInt(flags),
		},
		"Encoding": ast.PdfDict{
			"Type":         ast.PdfName("Encoding"),
			"BaseEncoding": ast.PdfName("WinAnsiEncoding"),
			"Differences":  ast.PdfArray{ast.PdfInt(0x41), ast.PdfName(glyph)},
		},
	}
}
