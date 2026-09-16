package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestType0ToUnicode_MalformedCMapMapsNothing pins that a /ToUnicode CMap
// carrying a token the CMap syntax cannot produce maps no code, so a composite
// font showing a glyph fails ISO 14289-1 §7.21.7 t1 even where the bfchar list
// covers that code.
//
// ISO 32000-1, 7.3.5 ends a name at a delimiter, so "/Adobe-Identity-UC>"
// leaves a bare ">" that is valid nowhere in a CMap. veraPDF discards such a
// CMap whole and reports every code the page shows. Measured on pdfjs
// issue11651.pdf, whose CMap carries both "/CMapName /Adobe-Identity-UC>" and
// "/Ordering (UC>)" where an S was corrupted to ">", and on fixtures isolating
// each: corrupting /CMapName alone already makes veraPDF fail the file, while
// the same CMap with the name intact passes in both tools.
func TestType0ToUnicode_MalformedCMapMapsNothing(t *testing.T) {
	t.Parallel()

	cmapNamed := func(name string) string {
		return "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
			"/CMapName " + name + " def /CMapType 2 def " +
			"1 begincodespacerange <0000> <FFFF> endcodespacerange " +
			"1 beginbfchar <0003> <0041> endbfchar endcmap " +
			"CMapName currentdict /CMap defineresource pop end end"
	}

	for _, tt := range []struct {
		name  string
		cmap  string
		fires bool
	}{
		{"a well-formed name", cmapNamed("/Adobe-Identity-UCS"), false},
		{"a name a stray delimiter cut short", cmapNamed("/Adobe-Identity-UC>"), true},
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
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.cmap), tt.cmap),
				"<< /Type /FontDescriptor /FontName /Test /Flags 32 /ItalicAngle 0 /Ascent 700 "+
					"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{
				visible:    true,
				charCodes:  map[byte]bool{0: true, 3: true},
				rawStrings: []ast.PdfString{"\x00\x03"},
			}
			got := checkType0Font(ctx, doc, fontDict, "F1", 1, "PDF/UA-1", true, usage)
			if fired := hasRule(got, toUnicodeRule("PDF/UA-1")); fired != tt.fires {
				t.Errorf("7.21.7 t1 fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
