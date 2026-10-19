package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolveFontDifferencesNameReplacesTheBaseName pins ISO 32000-1, 9.6.6.1:
// a /Differences entry replaces the base encoding's name at its code, so a
// name no glyph list maps leaves the code with no character rather than the
// base encoding's. Code 32 still reads as a word space (9.3.3), and a code the
// array does not name keeps the base encoding's character, as do the two names
// [applyDifferencesName] reads leniently: .notdef, and a numeric name that
// spells its own code ("c128", the Euro sign under WinAnsiEncoding).
func TestResolveFontDifferencesNameReplacesTheBaseName(t *testing.T) {
	t.Parallel()
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("Helvetica"),
		"Encoding": ast.PdfDict{
			"Type":         ast.PdfName("Encoding"),
			"BaseEncoding": ast.PdfName("WinAnsiEncoding"),
			"Differences": ast.PdfArray{
				ast.PdfInt(32), ast.PdfName("T20"),
				ast.PdfInt(65), ast.PdfName("T41"),
				ast.PdfInt(67), ast.PdfName(".notdef"),
				ast.PdfInt(128), ast.PdfName("c128"),
				ast.PdfInt(129), ast.PdfName("c033"),
			},
		},
	}
	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, fontDict)
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	for _, code := range []byte{32, 65} {
		if r, ok := font.Encoding[code]; ok {
			t.Errorf("Encoding[0x%02X] = %q, want no character: /Differences replaced the base name", code, r)
		}
	}
	cases := []struct {
		code byte
		want string
	}{
		{32, " "},
		{65, ""},
		{66, "B"},
		{67, "C"},
		{128, "€"},
		{129, "!"},
	}
	for _, c := range cases {
		if got, _, _ := decodeNextChar(font, []byte{c.code}); got != c.want {
			t.Errorf("decodeNextChar(0x%02X) = %q, want %q", c.code, got, c.want)
		}
	}
}

// TestResolveFontDifferencesNameOverridesTheProgramEncoding pins that a
// /Differences name also replaces what an embedded Type 1 program's built-in
// encoding names at the code: ISO 32000-1, 9.6.6.1 lays the array over the
// base encoding, the built-in one included, so filling the code from the
// program afterwards would undo the replacement.
func TestResolveFontDifferencesNameOverridesTheProgramEncoding(t *testing.T) {
	t.Parallel()
	fontDict := numericNamedType1Font(map[int]string{65: "A", 66: "B"}, ast.PdfDict{
		"Type":        ast.PdfName("Encoding"),
		"Differences": ast.PdfArray{ast.PdfInt(65), ast.PdfName("T41")},
	})
	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, fontDict)
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if r, ok := font.Encoding[65]; ok {
		t.Errorf("Encoding[0x41] = %q, want no character: /Differences named T41", r)
	}
	if r := font.Encoding[66]; r != 'B' {
		t.Errorf("Encoding[0x42] = %q, want 'B' from the program's built-in encoding", r)
	}
}

// TestResolveFontType3DifferencesNameKeepsTheCode pins the Type 3 leniency of
// [applyDifferencesName]: a Type 3 font's glyph names are only the keys of its
// /CharProcs (ISO 32000-1, 9.6.5), so a name no glyph list maps, such as the
// "a65" a bitmap font gives code 65, keeps the default encoding's reading.
func TestResolveFontType3DifferencesNameKeepsTheCode(t *testing.T) {
	t.Parallel()
	fontDict := ast.PdfDict{
		"Type":       ast.PdfName("Font"),
		"Subtype":    ast.PdfName("Type3"),
		"FontMatrix": ast.PdfArray{ast.PdfReal(0.001), ast.PdfInt(0), ast.PdfInt(0), ast.PdfReal(0.001), ast.PdfInt(0), ast.PdfInt(0)},
		"CharProcs":  ast.PdfDict{},
		"Encoding": ast.PdfDict{
			"Type":        ast.PdfName("Encoding"),
			"Differences": ast.PdfArray{ast.PdfInt(65), ast.PdfName("a65"), ast.PdfName("eacute")},
		},
	}
	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, fontDict)
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got, _, _ := decodeNextChar(font, []byte{65}); got != "A" {
		t.Errorf("decodeNextChar(0x41) = %q, want %q", got, "A")
	}
	if got, _, _ := decodeNextChar(font, []byte{66}); got != "é" {
		t.Errorf("decodeNextChar(0x42) = %q, want %q from the name eacute", got, "é")
	}
}
