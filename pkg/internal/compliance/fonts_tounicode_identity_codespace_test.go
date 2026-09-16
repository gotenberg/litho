package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestType0PerGlyphToUnicode_IdentityCodeIsTwoBytes pins which codespace splits
// the codes a composite font shows when its /ToUnicode declares another.
//
// An Identity CMap makes every character code two bytes (ISO 32000-1, 9.7.5.2)
// and the /ToUnicode CMap describes those same codes (9.10.3), so a one-byte
// codespace written there does not shorten them. Reading the shown string
// through the /ToUnicode codespace split "\x00\x24" into two one-byte codes and
// reported the leading zero byte as unmapped. veraPDF keys its entries by code
// value (CMap.getUnicode(int)) and never consults that codespace, so it maps
// 0x0024 through a one-byte bfrange covering 9 to 64 and reports nothing.
//
// Measured on pdfjs bug898853.pdf, an Identity-H FrutigerLTStd-Light whose
// /ToUnicode declares <00> <ff> and carries one-byte bfranges: veraPDF passes
// it, litho reported ISO 14289-2, 8.4.5.8 t1.
func TestType0PerGlyphToUnicode_IdentityCodeIsTwoBytes(t *testing.T) {
	t.Parallel()

	const tuCMap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"/CMapName /Adobe-Identity-UCS def /CMapType 2 def " +
		"1 begincodespacerange <00> <ff> endcodespacerange " +
		"1 beginbfrange <09> <40> <0028> endbfrange endcmap " +
		"CMapName currentdict /CMap defineresource pop end end"

	for _, tt := range []struct {
		name  string
		shown ast.PdfString
		fires bool
	}{
		{"a two-byte code the one-byte range covers by value", "\x00\x24", false},
		{"a two-byte code below the range", "\x00\x08", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H "+
					"/DescendantFonts [3 0 R] /ToUnicode 4 0 R >>",
				"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo "+
					"<< /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 5 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tuCMap), tuCMap),
				"<< /Type /FontDescriptor /FontName /Test /Flags 32 /ItalicAngle 0 /Ascent 700 "+
					"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{
				visible:    true,
				charCodes:  map[byte]bool{tt.shown[0]: true, tt.shown[1]: true},
				rawStrings: []ast.PdfString{tt.shown},
			}
			got := checkType0PerGlyphToUnicode(ctx, doc, fontDict, "F1", 1, "PDF/UA-2", usage)
			if fired := len(got) > 0; fired != tt.fires {
				t.Errorf("8.4.5.8 t1 fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
