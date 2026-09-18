package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// expertCFF assembles a CFF that declares a predefined encoding and nothing
// else: a header, a one-entry Name INDEX and a Top DICT INDEX. It carries no
// charset and no charstrings, so [typography.ExtractCFFEncodingNames] reports
// nothing for it, which is the subset case this test is about.
//
// encoding is the Top DICT Encoding operand: 0 selects the Standard encoding,
// 1 the Expert encoding, and any other value is a custom encoding the font
// builds itself.
func expertCFF(encoding int) string {
	var buf bytes.Buffer
	buf.Write([]byte{1, 0, 4, 2})                        // header
	buf.Write([]byte{0x00, 0x01, 0x01, 0x01, 0x02, 'X'}) // Name INDEX
	dict := []byte{byte(encoding + 139), 16}             // operand, Encoding
	buf.Write([]byte{0x00, 0x01, 0x01, 0x01, byte(1 + len(dict))})
	buf.Write(dict)
	return buf.String()
}

// TestExpertCFFPUACodes pins that a code a predefined CFF encoding assigns is
// judged for its Unicode value even when the subset omits that glyph.
//
// ISO 19005-2, 6.2.11.7.3 asks whether a shown code carries a Unicode value.
// For a simple font the answer comes from the encoding's glyph name, and a CFF
// declaring the Expert encoding (ISO 32000-1, 9.6.6 with the CFF Top DICT
// Encoding operand) assigns code 0x61 the name Hsmall whatever glyphs it
// embeds. litho read that encoding by intersecting the Expert table with the
// font's charset, which is right for drawing and wrong here: pdfjs
// issue8586.pdf embeds a subset holding Gsmall and Ismall but not Hsmall, and
// 0x61 is the only code its page shows, so the PUA value went unreported while
// veraPDF 1.30.2 failed the rule.
//
// The controls carry the weight. A named /Encoding and a /BaseEncoding each
// replace the program's own encoding outright (ISO 32000-1, Table 114), and
// every veraPDF-corpus fixture that embeds an Expert CFF names one of them, so
// those must keep resolving through the named table and stay silent.
func TestExpertCFFPUACodes(t *testing.T) {
	t.Parallel()

	const body = "BT /F1 12 Tf 10 50 Td (a) Tj ET"

	// fires reports whether §6.2.11.7.3 fires for a font carrying the given
	// /Encoding entry and CFF encoding operand. The page shows code 0x61.
	fires := func(t *testing.T, encodingEntry string, cffEncoding int) bool {
		t.Helper()
		program := expertCFF(cffEncoding)
		data := cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+TimesExpert "+
				"/FirstChar 97 /LastChar 97 /Widths [500] /FontDescriptor 6 0 R "+
				encodingEntry+" >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			"<< /Type /FontDescriptor /FontName /ABCDEF+TimesExpert /Flags 4 "+
				"/ItalicAngle 0 /Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 "+
				"/FontBBox [0 0 1000 1000] /FontFile3 7 0 R >>",
			fmt.Sprintf("<< /Subtype /Type1C /Length %d >>\nstream\n%s\nendstream",
				len(program), program),
		)
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2a)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "6.2.11.7.3" {
				return true
			}
		}
		return false
	}

	tests := []struct {
		name          string
		encodingEntry string
		cffEncoding   int
		want          bool
	}{
		{
			// The issue8586 shape: the dictionary states no base, so the
			// program's own encoding governs and names 0x61 Hsmall.
			name:          "an Expert CFF names a code its subset omits",
			encodingEntry: "/Encoding << /Type /Encoding /Differences [] >>",
			cffEncoding:   1,
			want:          true,
		},
		{
			// No /Encoding at all: the built-in encoding governs just the same.
			name:          "an Expert CFF with no /Encoding entry",
			encodingEntry: "",
			cffEncoding:   1,
			want:          true,
		},
		{
			// The shape of every veraPDF-corpus Expert fixture.
			name:          "a named /Encoding replaces the program's encoding",
			encodingEntry: "/Encoding /WinAnsiEncoding",
			cffEncoding:   1,
			want:          false,
		},
		{
			name:          "a /BaseEncoding replaces the program's encoding",
			encodingEntry: "/Encoding << /BaseEncoding /WinAnsiEncoding /Differences [] >>",
			cffEncoding:   1,
			want:          false,
		},
		{
			// Standard assigns 0x61 the name "a", which carries no PUA value.
			name:          "a Standard CFF encoding names no PUA glyph",
			encodingEntry: "/Encoding << /Type /Encoding /Differences [] >>",
			cffEncoding:   0,
			want:          false,
		},
		{
			// A custom encoding offset is the font's own table, which
			// [ExtractEncoding] reads; nothing is filled in.
			name:          "a custom CFF encoding is left alone",
			encodingEntry: "/Encoding << /Type /Encoding /Differences [] >>",
			cffEncoding:   100,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, tt.encodingEntry, tt.cffEncoding); got != tt.want {
				t.Errorf("§6.2.11.7.3 fired = %v, want %v", got, tt.want)
			}
		})
	}
}
