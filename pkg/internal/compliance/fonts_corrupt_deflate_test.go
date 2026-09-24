package compliance

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFontProgramCorruptDeflate pins that a font program whose deflate data is
// corrupt counts as unembedded (ISO 19005-2, 6.2.11.4.1 t1), and then escapes the
// /CIDToGIDMap rule that needs an embedded program (6.2.11.3.2 t1), while one
// whose deflate stream is merely cut short still embeds its glyphs. veraPDF
// cannot read the corrupt program. Every case was measured against veraPDF.
//
// # Corpus evidence
//
//   - pdfjs issue11651.pdf: ships a font program with corrupt deflate data.
func TestFontProgramCorruptDeflate(t *testing.T) {
	t.Parallel()

	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	if _, err := zw.Write(ttf); err != nil {
		t.Fatalf("deflate: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("deflate: %v", err)
	}
	intact := z.Bytes()
	corrupt := bytes.Clone(intact)
	corrupt[len(corrupt)/2] ^= 0x55
	truncated := intact[:len(intact)-40]

	doc := func(program []byte, cidMap string) []byte {
		const content = "BT /F1 12 Tf 10 50 Td <0001> Tj ET"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Test /Encoding /Identity-H /DescendantFonts [6 0 R] >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Test"+
				" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 7 0 R"+cidMap+" >>",
			"<< /Type /FontDescriptor /FontName /ABCDEF+Test /Flags 4 /FontBBox [0 0 1000 1000] /ItalicAngle 0"+
				" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile2 8 0 R >>",
			fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", len(program), program),
		)
	}
	const identity = " /CIDToGIDMap /Identity"

	for _, tc := range []struct {
		name        string
		data        []byte
		unembedded  bool
		missingCMap bool
	}{
		{"an intact program", doc(intact, identity), false, false},
		{"a program cut short", doc(truncated, identity), false, false},
		{"a corrupt program", doc(corrupt, identity), true, false},
		{"a corrupt program with no CIDToGIDMap", doc(corrupt, ""), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRuleProfile(t, tc.data, models.PDFA2b, "6.2.11.4.1", 1); got != tc.unembedded {
				t.Errorf("6.2.11.4.1 t1 violation = %v, want %v", got, tc.unembedded)
			}
			if got := firesRuleProfile(t, tc.data, models.PDFA2b, "6.2.11.3.2", 1); got != tc.missingCMap {
				t.Errorf("6.2.11.3.2 t1 violation = %v, want %v", got, tc.missingCMap)
			}
		})
	}
}
