package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestType0ToUnicode_NotACMap pins that a composite font whose /ToUnicode
// stream is not a CMap fails ISO 14289-1 §7.21.7 t1 once it shows a glyph.
//
// The clause asks for a ToUnicode CMap that maps character codes to Unicode
// values, and a stream that does not parse as one maps none.
//
// # Corpus evidence
//
//   - pdfjs issue8061.pdf: ships 345 bytes of binary data as its /ToUnicode.
//     veraPDF reads every glyph the font shows as having no Unicode value,
//     while litho accepted the entry.
func TestType0ToUnicode_NotACMap(t *testing.T) {
	t.Parallel()

	const validCMap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange " +
		"1 beginbfchar <0003> <0041> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end"

	tests := []struct {
		name  string
		cmap  string
		shown bool
		fires bool
	}{
		{"a stream that is not a CMap", "not a cmap at all", true, true},
		{"a CMap that maps the codes", validCMap, true, false},
		{"a broken stream no glyph uses", "not a cmap at all", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H /DescendantFonts [3 0 R] /ToUnicode 4 0 R >>",
				"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 5 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.cmap), tt.cmap),
				"<< /Type /FontDescriptor /FontName /Test /Flags 32 /ItalicAngle 0 /Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{visible: tt.shown, charCodes: map[byte]bool{}}
			if tt.shown {
				usage.charCodes[0] = true
				usage.charCodes[3] = true
				usage.rawStrings = []ast.PdfString{"\x00\x03"}
			}
			got := checkType0Font(ctx, doc, fontDict, "F1", 1, "PDF/UA-1", usage)
			if fired := hasRule(got, toUnicodeRule("PDF/UA-1")); fired != tt.fires {
				t.Errorf("7.21.7 t1 fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
