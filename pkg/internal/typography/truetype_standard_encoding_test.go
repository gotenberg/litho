package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestNonEmbeddedNonsymbolicTrueTypeReadsStandardEncoding pins the reading
// of a TrueType font that embeds no program, sets the Nonsymbolic flag and
// states no /Encoding, the shape of pdfjs issue7580.pdf: StandardEncoding,
// where 0xF1 is ae and 0xF9 oslash. Without the flag, with the Symbolic flag
// too, with a stated /Encoding, or with an embedded program, the WinAnsi
// default stays.
func TestNonEmbeddedNonsymbolicTrueTypeReadsStandardEncoding(t *testing.T) {
	t.Parallel()

	build := func(flags int, embedded bool, encoding ast.PdfObject) ast.PdfDict {
		fd := ast.PdfDict{"Type": ast.PdfName("FontDescriptor"), "FontName": ast.PdfName("ArialMT"), "Flags": ast.PdfInt(flags)}
		if embedded {
			fd["FontFile2"] = ast.PdfIndirectRef{ObjectNum: 99}
		}
		font := ast.PdfDict{
			"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("TrueType"), "BaseFont": ast.PdfName("ArialMT"),
			"FirstChar": ast.PdfInt(32), "LastChar": ast.PdfInt(32), "Widths": ast.PdfArray{ast.PdfInt(278)},
			"FontDescriptor": fd,
		}
		if encoding != nil {
			font["Encoding"] = encoding
		}
		return font
	}
	doc := &document.Document{Logger: document.NopLogger()}
	for _, tc := range []struct {
		desc string
		font ast.PdfDict
		want string
	}{
		{"nonsymbolic, no program, no /Encoding", build(32, false, nil), "æø"},
		{"no Nonsymbolic flag", build(0, false, nil), "ñù"},
		{"Symbolic and Nonsymbolic", build(36, false, nil), "ñù"},
		{"stated WinAnsiEncoding", build(32, false, ast.PdfName("WinAnsiEncoding")), "ñù"},
		{"embedded program", build(32, true, nil), "ñù"},
	} {
		font, err := ResolveFont(context.Background(), doc, tc.font)
		if err != nil {
			t.Fatalf("%s: ResolveFont: %v", tc.desc, err)
		}
		if got := string([]rune{font.Encoding[0xF1], font.Encoding[0xF9]}); got != tc.want {
			t.Errorf("%s: 0xF1 0xF9 read %q, want %q", tc.desc, got, tc.want)
		}
	}
}
