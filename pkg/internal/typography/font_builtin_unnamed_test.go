package typography

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// vectorType1Font builds a Type 1 font dictionary with the given /Flags whose
// /FontFile program writes an explicit built-in encoding vector naming each
// code in names, filled with .notdef first when fill is set, and whose
// /Encoding is encoding (omitted when nil).
func vectorType1Font(names map[int]string, fill bool, flags int, encoding ast.PdfObject) ast.PdfDict {
	var b strings.Builder
	b.WriteString("%!FontType1-1.0: TEST 1\n/Encoding 256 array\n")
	if fill {
		b.WriteString("0 1 255 {1 index exch /.notdef put} for\n")
	}
	for code := range 256 {
		if name, ok := names[code]; ok {
			fmt.Fprintf(&b, "dup %d /%s put\n", code, name)
		}
	}
	b.WriteString("readonly def\ncurrentfile eexec\n")
	font := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("TEST"),
		"FontDescriptor": ast.PdfDict{
			"Type":     ast.PdfName("FontDescriptor"),
			"FontName": ast.PdfName("TEST"),
			"Flags":    ast.PdfInt(flags),
			"FontFile": ast.NewStream(ast.PdfDict{}, []byte(b.String())),
		},
	}
	if encoding != nil {
		font["Encoding"] = encoding
	}
	return font
}

// TestResolveFont_BuiltInEncodingLeavesCodesUnnamed pins that a code the
// built-in encoding of an embedded Type 1 or Type1C program leaves unnamed, or
// names .notdef, has no character when the font's encoding builds on it: a font
// that states no /Encoding reads its codes through that encoding (ISO 32000-1,
// 9.6.6.1, Table 111), as does a /Differences array with no /BaseEncoding
// (Table 114), and 9.10.2 finds no Unicode value for a code with no glyph name.
// litho read such a code through the default StandardEncoding or
// WinAnsiEncoding, so the litho corpus fixture type1notdef_2u read code 43
// under N1 and N2 as "C".
func TestResolveFont_BuiltInEncodingLeavesCodesUnnamed(t *testing.T) {
	t.Parallel()

	const (
		symbolic    = 4
		nonsymbolic = 32
	)
	names := map[int]string{0x41: "A", 0x42: "B"}
	differences := ast.PdfDict{
		"Type":        ast.PdfName("Encoding"),
		"Differences": ast.PdfArray{ast.PdfInt(0x44), ast.PdfName("D")},
	}
	cffNames := map[byte]string{0x41: "A", 0x42: "B"}
	for _, c := range []struct {
		name  string
		font  ast.PdfDict
		named map[byte]rune
		none  []byte
	}{
		{"Type 1 filled, nonsymbolic", vectorType1Font(names, true, nonsymbolic, nil), map[byte]rune{0x41: 'A', 0x42: 'B'}, []byte{0x43, 0x20, 0x61}},
		{"Type 1 unfilled, nonsymbolic", vectorType1Font(names, false, nonsymbolic, nil), map[byte]rune{0x41: 'A'}, []byte{0x43, 0x20}},
		{"Type 1 filled, symbolic", vectorType1Font(names, true, symbolic, nil), map[byte]rune{0x41: 'A'}, []byte{0x43, 0x80}},
		{"Type 1 under Differences", vectorType1Font(names, true, nonsymbolic, differences), map[byte]rune{0x41: 'A', 0x44: 'D'}, []byte{0x43}},
		{"Type1C custom", symbolicType1CFont(cffNames, nil), map[byte]rune{0x41: 'A'}, []byte{0x43, 0x80}},
		{"Type1C custom under Differences", symbolicType1CFont(cffNames, differences), map[byte]rune{0x41: 'A', 0x44: 'D'}, []byte{0x43}},
		{"Type1C standard", symbolicType1CFont(nil, nil), map[byte]rune{0x41: 'A'}, []byte{0x80, 0x7F}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, c.font)
			if err != nil {
				t.Fatalf("ResolveFont: %v", err)
			}
			for code, want := range c.named {
				if got := font.Encoding[code]; got != want {
					t.Errorf("Encoding[%#x] = %q, want %q", code, got, want)
				}
			}
			for _, code := range c.none {
				if got, ok := font.Encoding[code]; ok {
					t.Errorf("Encoding[%#x] = %q, want no character: the built-in encoding names no glyph there", code, got)
				}
			}
		})
	}
}

// TestResolveFont_BuiltInEncodingNamedKeepsDefault pins that a Type 1 program
// whose /Encoding is the StandardEncoding keyword reads every code
// StandardEncoding names, the reading the fixture's N3 and N4 rely on.
func TestResolveFont_BuiltInEncodingNamedKeepsDefault(t *testing.T) {
	t.Parallel()

	program := "%!FontType1-1.0: TEST 1\n/Encoding StandardEncoding def\ncurrentfile eexec\n"
	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("TEST"),
		"FontDescriptor": ast.PdfDict{
			"Type":     ast.PdfName("FontDescriptor"),
			"FontName": ast.PdfName("TEST"),
			"Flags":    ast.PdfInt(32),
			"FontFile": ast.NewStream(ast.PdfDict{}, []byte(program)),
		},
	})
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got := font.Encoding['C']; got != 'C' {
		t.Errorf("Encoding[0x43] = %q, want 'C'", got)
	}
	if got, ok := font.Encoding[0x80]; ok {
		t.Errorf("Encoding[0x80] = %q, want none: StandardEncoding names nothing there", got)
	}
}
