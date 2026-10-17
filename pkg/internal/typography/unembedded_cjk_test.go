package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolveFont_UnembeddedPredefinedCMap pins that a reference-only Type 0
// font decodes through ISO 32000-1, 9.10.2, steps a-e: the character code maps
// to a CID through the predefined CMap, the CID to Unicode through the
// collection's UCS2 CMap. Neither step reads the font program, so leaving it to
// the host changes nothing. PDF.js extracts the same characters once it is
// given its CMaps (noembed-eucjp.pdf, noembed-identity.pdf).
func TestResolveFont_UnembeddedPredefinedCMap(t *testing.T) {
	ctx := context.Background()

	build := func(encoding, registry, ordering string) ast.PdfDict {
		return ast.PdfDict{
			"Type":     ast.PdfName("Font"),
			"Subtype":  ast.PdfName("Type0"),
			"BaseFont": ast.PdfName("Ryumin-Light"),
			"Encoding": ast.PdfName(encoding),
			"DescendantFonts": ast.PdfArray{ast.PdfDict{
				"Type":     ast.PdfName("Font"),
				"Subtype":  ast.PdfName("CIDFontType0"),
				"BaseFont": ast.PdfName("Ryumin-Light"),
				"W":        ast.PdfArray{ast.PdfInt(1), ast.PdfArray{ast.PdfInt(250)}},
				"CIDSystemInfo": ast.PdfDict{
					"Registry":   ast.PdfString(registry),
					"Ordering":   ast.PdfString(ordering),
					"Supplement": ast.PdfInt(2),
				},
				"FontDescriptor": ast.PdfDict{
					"Type":     ast.PdfName("FontDescriptor"),
					"FontName": ast.PdfName("Ryumin-Light"),
					"Flags":    ast.PdfInt(4),
				},
			}},
		}
	}

	tests := []struct {
		name     string
		encoding string
		registry string
		ordering string
		code     []byte
		want     string
		suppress bool
	}{
		// EUC-JP 0xA4A2 is Adobe-Japan1 CID 843, U+3042.
		{name: "predefined EUC-H", encoding: "EUC-H", registry: "Adobe", ordering: "Japan1", code: []byte{0xA4, 0xA2}, want: "あ"},
		// Under Identity-H the code is the CID: Adobe-Japan1 CID 843 is U+3042.
		{name: "Identity-H Adobe-Japan1", encoding: "Identity-H", registry: "Adobe", ordering: "Japan1", code: []byte{0x03, 0x4B}, want: "あ"},
		// A name outside Table 118 leaves 9.10.2 no route to Unicode.
		{name: "unknown CMap name", encoding: "Bogus-H", registry: "Adobe", ordering: "Japan1", code: []byte{0xA4, 0xA2}, suppress: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &document.Document{Logger: document.NopLogger()}
			font, err := ResolveFont(ctx, doc, build(tt.encoding, tt.registry, tt.ordering))
			if err != nil {
				t.Fatalf("ResolveFont: %v", err)
			}
			if font.SuppressExtraction != tt.suppress {
				t.Fatalf("SuppressExtraction = %v, want %v", font.SuppressExtraction, tt.suppress)
			}
			if tt.suppress {
				return
			}
			got, n, skip := decodeNextChar(font, tt.code)
			if skip || n != len(tt.code) || got != tt.want {
				t.Errorf("decodeNextChar(% X) = %q, %d, skip=%v; want %q, %d, skip=false", tt.code, got, n, skip, tt.want, len(tt.code))
			}
		})
	}
}

// TestResolveFont_UnembeddedChineseFace pins that an unembedded simple font
// named after a Chinese face and stating /WinAnsiEncoding reads its codes
// through GBK-EUC-H, as PDF.js does (issue20489.pdf), while an unlisted
// CJK-named face stays cleared.
func TestResolveFont_UnembeddedChineseFace(t *testing.T) {
	ctx := context.Background()
	build := func(baseFont string) ast.PdfDict {
		return ast.PdfDict{
			"Type":     ast.PdfName("Font"),
			"Subtype":  ast.PdfName("TrueType"),
			"BaseFont": ast.PdfName(baseFont),
			"Encoding": ast.PdfName("WinAnsiEncoding"),
		}
	}
	// GBK 0xD6D0 is U+4E2D.
	code := []byte{0xD6, 0xD0}

	doc := &document.Document{Logger: document.NopLogger()}
	font, err := ResolveFont(ctx, doc, build("\xCB\xCE\xCC\xE5")) // SimSun
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got, n, skip := decodeNextChar(font, code); skip || n != 2 || got != "中" {
		t.Errorf("SimSun: decodeNextChar = %q, %d, skip=%v; want \"中\", 2, skip=false", got, n, skip)
	}

	font, err = ResolveFont(ctx, doc, build("\xC2\xA5\xC2\xA5")) // not a listed face
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if font.ToUnicode != nil || font.Encoding != nil {
		t.Errorf("unlisted CJK name: ToUnicode set = %v, Encoding = %v; want both cleared", font.ToUnicode != nil, font.Encoding)
	}
}

// TestDecodeNextChar_Type0TableBeforeEncoding pins that a two-byte code a
// Type 0 font's CID table names reads as that character, not as two WinAnsi
// bytes, and that a code the table does not name keeps the single-byte read.
func TestDecodeNextChar_Type0TableBeforeEncoding(t *testing.T) {
	font := &Font{
		Subtype:      "Type0",
		Encoding:     map[byte]rune{0x4E: 'N', 0x2D: '-', 0x63: 'c'},
		GIDToUnicode: map[uint16]rune{0x4E2D: '中'},
	}
	if got, n, skip := decodeNextChar(font, []byte{0x4E, 0x2D}); skip || n != 2 || got != "中" {
		t.Errorf("named code: decodeNextChar = %q, %d, skip=%v; want \"中\", 2, skip=false", got, n, skip)
	}
	if got, n, skip := decodeNextChar(font, []byte{0x63, 0x6D}); skip || n != 1 || got != "c" {
		t.Errorf("unnamed code: decodeNextChar = %q, %d, skip=%v; want \"c\", 1, skip=false", got, n, skip)
	}
}
