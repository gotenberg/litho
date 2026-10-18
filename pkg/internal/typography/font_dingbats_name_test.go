package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// TestResolveFont_DingbatsTreatmentFollowsTheStandardName pins that only the
// standard ZapfDingbats font (ISO 32000-1, 9.6.2.2) reads through the
// ZapfDingbats encoding and glyph table once it embeds a program: a name
// merely holding "Dingbats" leaves the program to say what its codes are.
func TestResolveFont_DingbatsTreatmentFollowsTheStandardName(t *testing.T) {
	unicodeOnly := []fontbuild.Glyph{{Name: "a", Unicode: 'a', Advance: 500}}
	unembedded := func(baseFont string, flags int) ast.PdfDict {
		return ast.PdfDict{
			"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"),
			"BaseFont": ast.PdfName(baseFont),
			"FontDescriptor": ast.PdfDict{
				"Type": ast.PdfName("FontDescriptor"), "FontName": ast.PdfName(baseFont),
				"Flags": ast.PdfInt(flags),
			},
		}
	}
	for _, c := range []struct {
		name string
		font ast.PdfDict
		code byte
		want string
	}{
		{
			// The ZapfDingbats encoding read 0x61 as a60, "❁".
			name: "embedded font whose name holds Dingbats",
			font: symbolicTrueTypeFont("AAAAAC+ZapfDingbatsITC", unicodeOnly),
			code: 0x61,
			want: "a",
		},
		{
			name: "embedded ZapfDingbats keeps its glyph table",
			font: symbolicTrueTypeFont("MHHIBN+ZapfDingbats", unicodeOnly),
			code: 0x34,
			want: "✔",
		},
		{
			// With no program to read, a symbolic font named for dingbats
			// reads as PDF.js reads it, through the ZapfDingbats encoding.
			name: "unembedded symbolic font whose name holds Dingbats",
			font: unembedded("ITCZapfDingbats", 4),
			code: 0x34,
			want: "✔",
		},
		{
			// PDF.js gives an unembedded nonsymbolic font the
			// StandardEncoding default unless its name is one of the symbol
			// fonts it lists.
			name: "unembedded nonsymbolic font whose name holds Dingbats",
			font: unembedded("ITCZapfDingbats", 32),
			code: 0x34,
			want: "4",
		},
		{
			name: "unembedded nonsymbolic Dingbats",
			font: unembedded("Dingbats", 32),
			code: 0x34,
			want: "✔",
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
