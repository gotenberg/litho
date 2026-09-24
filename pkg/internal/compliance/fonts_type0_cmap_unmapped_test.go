package compliance

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestType0EmbeddedCMapUnmappedCode pins ISO 19005-2, 6.2.11.8 t1 on a Type 0
// font whose embedded encoding CMap maps no CID for the code shown: the code
// selects CID 0 (ISO 32000-1, 9.7.6.3), the .notdef glyph, and a bfchar
// section, which maps to Unicode (9.10.3), does not change that. Every case
// was measured against veraPDF.
//
// # Corpus evidence
//
//   - pdfjs bug920426.pdf: writes its encoding CMap with bfchar alone.
func TestType0EmbeddedCMapUnmappedCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	const head = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CIDSystemInfo << /Registry (Adobe)" +
		" /Ordering (Identity) /Supplement 0 >> def /CMapName /T def /CMapType 1 def\n1 begincodespacerange <0000> <FFFF> endcodespacerange\n"
	const tail = "endcmap CMapName currentdict /CMap defineresource pop end end"

	for _, tt := range []struct {
		name string
		body string
		want int
	}{
		{"a CMap written with bfchar alone", "1 beginbfchar <0041> <0041> endbfchar\n", 1},
		{"a CMap mapping the code with cidchar", "1 begincidchar <0041> 1 endcidchar\n", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			const body = "BT /F1 12 Tf 10 50 Td <0041> Tj ET"
			cm := head + tt.body + tail
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
				"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding 9 0 R /DescendantFonts [6 0 R] >>",
				"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity)"+
					" /Supplement 0 >> /CIDToGIDMap /Identity /FontDescriptor 7 0 R >>",
				"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 /Ascent 700 /Descent -200 /CapHeight 700"+
					" /StemV 80 /FontBBox [0 0 1000 1000] /FontFile2 8 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ttf), ttf),
				fmt.Sprintf("<< /Type /CMap /CMapName /T /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >>"+
					" /Length %d >>\nstream\n%s\nendstream", len(cm), cm),
			)
			viols, err := ValidatePDFA(ctx, doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_11_8_t01); got != tt.want {
				t.Errorf("6.2.11.8 t1 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
