package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestToUnicode_UnnamedCodeWithoutProgram pins the one case where a shown code
// its /ToUnicode CMap omits, and its encoding names nothing, still carries no
// Unicode: a font that embeds no program. veraPDF reads the program's own glyph
// names when there is a program, which litho does not establish, so an embedded
// font keeps its exemption. With no program there is nothing left to read.
//
// The real file is pdfjs bug864847.pdf: an unembedded symbolic Arial-ItalicMT
// with no /Encoding whose /ToUnicode maps only <20>, shown with codes 72 and
// 101. veraPDF reports nine glyphs under ISO 19005-2 §6.2.11.7.2 t1, and the
// same font under §6.3.8 t1 and ISO 14289-1 §7.21.7 t1.
func TestToUnicode_UnnamedCodeWithoutProgram(t *testing.T) {
	t.Parallel()

	// Maps <20> only, so any other shown code falls outside it.
	const spaceOnly = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"1 begincodespacerange <00> <ff> endcodespacerange " +
		"1 beginbfrange <20> <20> <0020> endbfrange " +
		"endcmap CMapName currentdict /CMap defineresource pop end end"

	tests := []struct {
		name     string
		subtype  string
		fontFile string
		code     byte
		fires    bool
	}{
		{"unembedded, code outside the CMap", "TrueType", "", 101, true},
		{"unembedded, code the CMap maps", "TrueType", "", 0x20, false},
		{"embedded, code outside the CMap", "TrueType", " /FontFile2 5 0 R", 101, false},
		{"unembedded Type 1, code outside the CMap", "Type1", "", 101, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			// Flags 4 marks the font symbolic, so no standard-encoding or AGL
			// exemption applies and the CMap is the only Unicode source.
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /"+tt.subtype+" /BaseFont /Arial-ItalicMT"+
					" /FontDescriptor 3 0 R /ToUnicode 4 0 R >>",
				"<< /Type /FontDescriptor /FontName /Arial-ItalicMT /Flags 4 /ItalicAngle 0"+
					" /Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000]"+
					tt.fontFile+" >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(spaceOnly), spaceOnly),
				"<< /Length 4 /Length1 4 >>\nstream\n\x00\x01\x00\x00\nendstream",
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{tt.code: true}}

			got := checkToUnicode(ctx, doc, fontDict, "F1", 1, "PDF/A-2u", usage)
			if fired := len(got) > 0; fired != tt.fires {
				t.Errorf("ToUnicode violation fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
