package glyph

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func FuzzParseCIDToGIDMap(f *testing.F) {
	f.Add([]byte{0, 0, 0, 3, 0, 1})
	f.Add([]byte{0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		got := parseCIDToGIDMap(data)
		if len(got) > maxCIDs || len(got) > len(data)/2 {
			t.Fatalf("%d entries from %d bytes", len(got), len(data))
		}
	})
}

// FuzzGlyph loads a font whose embedded program is the fuzzed bytes, under
// each font file key, and resolves every single-byte code and a run of
// two-byte ones; it must never panic.
func FuzzGlyph(f *testing.F) {
	tt := fontbuild.TrueType{PostScriptName: "T", Family: "T", Subfamily: "Regular", Glyphs: glyphs}.Build()
	f.Add(tt, uint8(1), false)
	f.Add(tt[:len(tt)/2], uint8(1), true)
	cffProg := fontbuild.CFF{FontName: "T", Glyphs: glyphs, Subrs: true}.Build()
	f.Add(cffProg, uint8(2), false)
	f.Add(cffProg[:len(cffProg)/2], uint8(2), true)
	t1, _ := fontbuild.Type1{FontName: "T", Glyphs: glyphs}.Build()
	f.Add(t1, uint8(0), false)
	f.Add(t1[:len(t1)/2], uint8(0), false)
	f.Fuzz(func(t *testing.T, program []byte, key uint8, composite bool) {
		fileKey := fontFileKeys[int(key)%len(fontFileKeys)]
		var dict ast.PdfDict
		if composite {
			dict = cidTrueType(nil, ast.NewStream(ast.PdfDict{}, program))
			cid := dict["DescendantFonts"].(ast.PdfArray)[0].(ast.PdfDict) //nolint:forcetypeassert // built just above.
			cid["FontDescriptor"] = descriptor(4, fileKey, program, ast.PdfDict{})
		} else {
			dict = simpleFont("TrueType", descriptor(32, fileKey, program, ast.PdfDict{}), ast.PdfName("WinAnsiEncoding"), 500)
		}
		font, err := Load(context.Background(), testDoc(), dict, Options{})
		if err != nil {
			return
		}
		data := make([]byte, 0, 512)
		for c := range 256 {
			data = append(data, byte(c))
		}
		for _, code := range font.Codes(data) {
			if _, err := font.Glyph(context.Background(), code); err != nil {
				t.Fatalf("Glyph: %v", err)
			}
		}
	})
}
