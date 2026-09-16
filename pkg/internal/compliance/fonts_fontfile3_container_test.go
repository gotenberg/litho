package compliance

import (
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFontFile3ContainerMatchesFont pins which container a /FontFile3 program
// must hold to count as embedded (ISO 19005-2, 6.2.11.4.1 t1).
//
// ISO 32000-1, Table 126 gives /FontFile3 two containers: a bare CFF for
// "Type1C" and "CIDFontType0C", an sfnt for "OpenType". veraPDF picks the
// parser from the font rather than the stream, so the pairing that counts
// differs per font subtype: see [fontFile3ParsesForFont].
//
// Every row was measured against veraPDF 1.30.2 by moving the same program
// bytes between declarations. The /TrueType rows matter most: pdfjs
// issue4668.pdf declares "Type1C" over a TrueType sfnt and veraPDF reports it
// embedded, so that pairing shall stay silent.
func TestFontFile3ContainerMatchesFont(t *testing.T) {
	t.Parallel()

	sfnt, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read sfnt fixture: %v", err)
	}
	cff, err := os.ReadFile("testdata/lato_bold_subset.cff")
	if err != nil {
		t.Fatalf("read CFF fixture: %v", err)
	}

	simple := func(fontSub, declared string, program []byte) []byte {
		const content = "BT /F1 12 Tf 10 50 Td (A) Tj ET"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /Font /Subtype /"+fontSub+" /BaseFont /ABCDEF+Test /FirstChar 65 /LastChar 65"+
				" /Widths [500] /Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>",
			"<< /Type /FontDescriptor /FontName /ABCDEF+Test /Flags 4 /FontBBox [0 0 1000 1000] /ItalicAngle 0"+
				" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile3 7 0 R >>",
			fmt.Sprintf("<< /Subtype /%s /Length %d >>\nstream\n%s\nendstream", declared, len(program), program),
		)
	}

	composite := func(declared string, program []byte) []byte {
		const content = "BT /F1 12 Tf 10 50 Td <0001> Tj ET"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Test /Encoding /Identity-H /DescendantFonts [6 0 R] >>",
			"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /ABCDEF+Test /DW 1000"+
				" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 7 0 R >>",
			"<< /Type /FontDescriptor /FontName /ABCDEF+Test /Flags 4 /FontBBox [0 0 1000 1000] /ItalicAngle 0"+
				" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile3 8 0 R >>",
			fmt.Sprintf("<< /Subtype /%s /Length %d >>\nstream\n%s\nendstream", declared, len(program), program),
		)
	}

	for _, tc := range []struct {
		name       string
		data       []byte
		unembedded bool
	}{
		{"a Type1 font reads Type1C as CFF", simple("Type1", "Type1C", cff), false},
		{"a Type1 font cannot read an sfnt as Type1C", simple("Type1", "Type1C", sfnt), true},
		{"a Type1 font reads OpenType as an sfnt", simple("Type1", "OpenType", sfnt), false},
		{"a Type1 font cannot read a CFF as OpenType", simple("Type1", "OpenType", cff), true},
		// pdfjs issue4668.pdf: a TrueType font parses /FontFile3 as an sfnt
		// whatever the subtype names, so this pairing stays embedded.
		{"a TrueType font reads an sfnt named Type1C", simple("TrueType", "Type1C", sfnt), false},
		{"a TrueType font cannot read a CFF", simple("TrueType", "Type1C", cff), true},
		{"a CIDFont reads CIDFontType0C as CFF", composite("CIDFontType0C", cff), false},
		{"a CIDFont cannot read an sfnt as CIDFontType0C", composite("CIDFontType0C", sfnt), true},
		{"a CIDFont reads OpenType as an sfnt", composite("OpenType", sfnt), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRuleProfile(t, tc.data, models.PDFA2b, "6.2.11.4.1", 1); got != tc.unembedded {
				t.Errorf("6.2.11.4.1 t1 violation = %v, want %v", got, tc.unembedded)
			}
		})
	}
}
