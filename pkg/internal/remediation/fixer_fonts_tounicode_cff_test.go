package remediation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// symbolicType1CPDF builds a page whose object 4 is a symbolic Type 1 font
// stating no /Encoding, embedding a /FontFile3 (Type1C) program whose built-in
// encoding names each code in names.
func symbolicType1CPDF(names map[byte]string) []byte {
	var glyphs []fontbuild.Glyph
	for c := range 256 {
		if n, ok := names[byte(c)]; ok {
			glyphs = append(glyphs, fontbuild.Glyph{Name: n, Advance: 500})
		}
	}
	program := fontbuild.CFF{FontName: "LithoMath", Glyphs: glyphs, Encoding: names}.Build()
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /LithoMath /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /LithoMath /Flags 4 /FontFile3 6 0 R >>",
		fmt.Sprintf("<< /Subtype /Type1C /Length %d >>\nstream\n%s\nendstream", len(program), program),
	})
}

// TestFixSimpleFontToUnicode_SymbolicType1CReadsItsBuiltInEncoding pins that a
// symbolic Type1C font stating no /Encoding gets the /ToUnicode its program's
// built-in encoding grounds, as a Type 1 program's does.
//
// ISO 32000-1, 9.6.6.1 draws such a font's codes through the built-in
// encoding of its program, whatever the program's format, and a name the Adobe
// Glyph List carries is the character (9.10.2). The CMSY8 subset of verapdf
// 6-3-8-t01-fail-a is a Type1C program naming its codes this way.
func TestFixSimpleFontToUnicode_SymbolicType1CReadsItsBuiltInEncoding(t *testing.T) {
	t.Parallel()

	data := symbolicType1CPDF(map[byte]string{0x00: "minus", 0x31: "infinity"})
	cmap, fixes := generatedToUnicode(t, data, codes(0x00, 0x31))
	if cmap == "" {
		t.Fatalf("no /ToUnicode was generated, fixes=%v", fixes)
	}
	for _, want := range []string{"<00> <2212>", "<31> <221E>"} {
		if !strings.Contains(cmap, want) {
			t.Errorf("the CMap lacks %q, the character the program names the glyph:\n%s", want, cmap)
		}
	}
}

// TestFixSimpleFontToUnicode_SymbolicType1CRefusesAnUngroundedCode is the
// control for codes the built-in encoding does not ground: a name no glyph
// list carries, a numeric name, and a code the encoding leaves unnamed. Each
// would leave a shown code unmapped, so no map is written.
func TestFixSimpleFontToUnicode_SymbolicType1CRefusesAnUngroundedCode(t *testing.T) {
	t.Parallel()

	names := map[byte]string{0x31: "infinity", 0x32: "H11021", 0x33: "C51"}
	for _, tc := range []struct {
		name string
		code byte
	}{
		{"a name no glyph list carries", 0x32},
		{"a numeric name", 0x33},
		{"a code the encoding leaves unnamed", 0x34},
	} {
		if cmap, _ := generatedToUnicode(t, symbolicType1CPDF(names), codes(0x31, tc.code)); cmap != "" {
			t.Errorf("%s: a /ToUnicode was attached:\n%s", tc.name, cmap)
		}
	}
}
