package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestToUnicode_CodeTheCMapLeavesOut pins that a /ToUnicode CMap vouches only
// for the codes it maps (ISO 14289-1 §7.21.7 t1).
//
// A shown code the CMap leaves out falls back to the glyph name the encoding
// gives it, as veraPDF's PDSimpleFont.toUnicode does, and a name with no
// Unicode value leaves the glyph without one. govdocs thread_300_300068.pdf
// shows code 2 of an MTSY font whose /Differences name it circlecopyrt, a TeX
// spelling no glyph list carries, while its /ToUnicode maps codes 1 and 3 only;
// veraPDF reports the glyph and litho accepted the font for having a CMap.
func TestToUnicode_CodeTheCMapLeavesOut(t *testing.T) {
	t.Parallel()

	cmapFor := func(entries string, n int) string {
		return "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
			"1 begincodespacerange <01> <03> endcodespacerange " +
			fmt.Sprintf("%d beginbfchar %s endbfchar ", n, entries) +
			"endcmap CMapName currentdict /CMap defineresource pop end end"
	}
	partial := cmapFor("<01> <0020> <03> <003D>", 2)
	full := cmapFor("<01> <0020> <02> <00A9> <03> <003D>", 3)

	tests := []struct {
		name  string
		cmap  string
		code  byte
		fires bool
	}{
		{"a code the CMap leaves out, named without Unicode", partial, 2, true},
		{"a code the CMap maps", partial, 1, false},
		{"a CMap mapping every shown code", full, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /Type1 /BaseFont /MTSY /Encoding << /Differences [1 /space /circlecopyrt /equal] >> "+
					"/FontDescriptor 3 0 R /ToUnicode 4 0 R >>",
				"<< /Type /FontDescriptor /FontName /MTSY /Flags 4 /ItalicAngle 0 /Ascent 700 /Descent -200 "+
					"/CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.cmap), tt.cmap),
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{tt.code: true}}
			got := checkToUnicode(ctx, doc, fontDict, "F1", 1, "PDF/UA-1", usage)
			if fired := hasRule(got, toUnicodeRule("PDF/UA-1")); fired != tt.fires {
				t.Errorf("fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
