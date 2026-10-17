package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolveFont_WinAnsiUnusedCodesReadAsBullet pins that a simple font
// encoded with WinAnsiEncoding reads every code the encoding leaves unused
// above octal 40 as U+2022 BULLET (ISO 32000-1, Annex D.2), whether the font
// names the encoding or bases its /Differences on it. The other predefined
// encodings keep those codes undefined, and a /Differences entry still wins.
//
// # Corpus evidence
//
//   - govdocs thread_200_200213.pdf: Helvetica with /WinAnsiEncoding shows its
//     list bullets as code 0x7F, which PDF.js reads as "•" and litho dropped.
func TestResolveFont_WinAnsiUnusedCodesReadAsBullet(t *testing.T) {
	ctx := context.Background()
	unused := []byte{0x7F, 0x81, 0x8D, 0x8F, 0x90, 0x9D}

	build := func(encoding ast.PdfObject) ast.PdfDict {
		return ast.PdfDict{
			"Type":     ast.PdfName("Font"),
			"Subtype":  ast.PdfName("Type1"),
			"BaseFont": ast.PdfName("Helvetica"),
			"Encoding": encoding,
		}
	}
	decode := func(t *testing.T, font *Font, code byte) string {
		t.Helper()
		got, _, skip := decodeNextChar(font, []byte{code})
		if skip {
			return ""
		}
		return got
	}

	tests := []struct {
		name     string
		encoding ast.PdfObject
		bullet   bool
	}{
		{name: "named WinAnsiEncoding", encoding: ast.PdfName("WinAnsiEncoding"), bullet: true},
		{name: "Differences over WinAnsiEncoding", encoding: ast.PdfDict{
			"Type":         ast.PdfName("Encoding"),
			"BaseEncoding": ast.PdfName("WinAnsiEncoding"),
			"Differences":  ast.PdfArray{ast.PdfInt(65), ast.PdfName("B")},
		}, bullet: true},
		{name: "MacRomanEncoding leaves them unassigned", encoding: ast.PdfName("MacRomanEncoding")},
		{name: "StandardEncoding leaves them unassigned", encoding: ast.PdfName("StandardEncoding")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &document.Document{Logger: document.NopLogger()}
			font, err := ResolveFont(ctx, doc, build(tt.encoding))
			if err != nil {
				t.Fatalf("ResolveFont: %v", err)
			}
			for _, code := range unused {
				if got := decode(t, font, code); (got == "•") != tt.bullet {
					t.Errorf("code 0x%02X = %q, want bullet %v", code, got, tt.bullet)
				}
			}
		})
	}

	t.Run("Differences entry wins over the bullet", func(t *testing.T) {
		doc := &document.Document{Logger: document.NopLogger()}
		font, err := ResolveFont(ctx, doc, build(ast.PdfDict{
			"Type":         ast.PdfName("Encoding"),
			"BaseEncoding": ast.PdfName("WinAnsiEncoding"),
			"Differences":  ast.PdfArray{ast.PdfInt(0x7F), ast.PdfName("A")},
		}))
		if err != nil {
			t.Fatalf("ResolveFont: %v", err)
		}
		if got := decode(t, font, 0x7F); got != "A" {
			t.Errorf("code 0x7F = %q, want %q", got, "A")
		}
	})

	// Annex D.2 names WinAnsiEncoding octal 255 "hyphen", where CP1252 has
	// a soft hyphen extraction drops (pdfjs issue9650.pdf ends a line on it).
	t.Run("octal 255 reads as a hyphen", func(t *testing.T) {
		doc := &document.Document{Logger: document.NopLogger()}
		font, err := ResolveFont(ctx, doc, build(ast.PdfName("WinAnsiEncoding")))
		if err != nil {
			t.Fatalf("ResolveFont: %v", err)
		}
		if got := decode(t, font, 0o255); got != "-" {
			t.Errorf("code 0xAD = %q, want %q", got, "-")
		}
	})

	t.Run("codes at or below octal 40 stay undefined", func(t *testing.T) {
		doc := &document.Document{Logger: document.NopLogger()}
		font, err := ResolveFont(ctx, doc, build(ast.PdfName("WinAnsiEncoding")))
		if err != nil {
			t.Fatalf("ResolveFont: %v", err)
		}
		if got := decode(t, font, 0x1F); got == "•" {
			t.Errorf("code 0x1F = %q, want no bullet", got)
		}
	})
}
