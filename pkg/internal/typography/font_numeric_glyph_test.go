package typography

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// numericNamedType1Font builds a symbolic Type 1 font dictionary whose
// embedded program's built-in encoding names each code in names, and whose
// /Encoding is encoding (omitted when nil).
func numericNamedType1Font(names map[int]string, encoding ast.PdfObject) ast.PdfDict {
	var b strings.Builder
	b.WriteString("%!FontType1-1.0: TEST 1\n/Encoding 256 array\n0 1 255 {1 index exch /.notdef put} for\n")
	for code := range 256 {
		if name, ok := names[code]; ok {
			fmt.Fprintf(&b, "dup %d /%s put\n", code, name)
		}
	}
	b.WriteString("readonly def\ncurrentfile eexec\n")
	fd := ast.PdfDict{
		"Type":     ast.PdfName("FontDescriptor"),
		"FontName": ast.PdfName("TEST"),
		"Flags":    ast.PdfInt(4),
		"FontFile": ast.NewStream(ast.PdfDict{}, []byte(b.String())),
	}
	font := ast.PdfDict{
		"Type":           ast.PdfName("Font"),
		"Subtype":        ast.PdfName("Type1"),
		"BaseFont":       ast.PdfName("TEST"),
		"FontDescriptor": fd,
	}
	if encoding != nil {
		font["Encoding"] = encoding
	}
	return font
}

// TestResolveFont_BuiltinNumericGlyphNames pins how a built-in encoding's
// numeric glyph names read. A name whose number, read as a glyph-name
// convention, lands on its own code keeps that reading; one that lands on
// another code reads the wrong convention and leaves the code to the default
// encoding. Times New Roman programs name code c "G<c in decimal>", and the
// hexadecimal reading turned every "a" into U+0097 and every space into "2".
func TestResolveFont_BuiltinNumericGlyphNames(t *testing.T) {
	names := map[int]string{
		0x07: "G07", // hexadecimal convention: lands on its own code
		0x20: "G32", // decimal convention: read as hex, would be "2"
		0x27: "G39", // decimal convention: read as hex, would be "9"
		0x41: "G41", // hexadecimal convention
		0x61: "G97", // decimal convention: read as hex, would be U+0097
		0x62: "C98", // decimal C convention: lands on its own code
	}
	differences := ast.PdfDict{
		"Type":        ast.PdfName("Encoding"),
		"Differences": ast.PdfArray{ast.PdfInt(9), ast.PdfName("G9")},
	}
	want := map[byte]rune{0x07: 0x07, 0x20: ' ', 0x27: '\'', 0x41: 'A', 0x61: 'a', 0x62: 'b'}

	for _, c := range []struct {
		name     string
		encoding ast.PdfObject
	}{
		{"Differences without BaseEncoding", differences},
		{"no Encoding", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			font, err := ResolveFont(context.Background(),
				&document.Document{Logger: document.NopLogger()}, numericNamedType1Font(names, c.encoding))
			if err != nil {
				t.Fatalf("ResolveFont: %v", err)
			}
			for code, r := range want {
				got, ok := font.Encoding[code]
				if !ok || got != r {
					t.Errorf("code 0x%02X = U+%04X (mapped %v), want U+%04X", code, got, ok, r)
				}
			}
		})
	}
}
