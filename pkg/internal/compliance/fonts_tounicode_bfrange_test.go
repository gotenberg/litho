package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestType0PerGlyphToUnicode_BfRangeCrossingFirstByte pins that a code only a
// bfrange crossing a first-byte boundary reaches has no Unicode value (ISO
// 14289-2, 8.4.5.8 t1). Adobe Technical Note #5411, 1.4.1 rules such a line
// invalid, and veraPDF keeps the start code's first-byte block, so
// <01ED> <0206> maps nothing. pdfjs issue5751.pdf carries exactly this line,
// and veraPDF fails the five glyphs it shows from it while litho passed them.
// The control writes the same codes as two conforming ranges.
func TestType0PerGlyphToUnicode_BfRangeCrossingFirstByte(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ranges   string
		wantFire bool
	}{
		{
			name:     "range crossing a first-byte boundary",
			ranges:   "1 beginbfrange\n<01ED> <0206> <0061>\nendbfrange\n",
			wantFire: true,
		},
		{
			name:     "same codes split at the boundary",
			ranges:   "2 beginbfrange\n<01ED> <01FF> <0061>\n<0200> <0206> <0074>\nendbfrange\n",
			wantFire: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			cmapData := "/CIDInit /ProcSet findresource begin\n" +
				"12 dict begin\nbegincmap\n/CMapName /Test def\n/CMapType 2 def\n" +
				"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
				tt.ranges +
				"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"

			// Object 2 is the Type0 font, 3 its descendant, 4 the /ToUnicode.
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /Type0 /BaseFont /MyriadArabic-Regular /Encoding /Identity-H /ToUnicode 4 0 R /DescendantFonts [ 3 0 R ] >>",
				"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /MyriadArabic-Regular /DW 1000 >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmapData), cmapData),
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}

			// Code 0x01EF is "c" in issue5751, inside the first-byte block.
			usage := &fontUsageInfo{visible: true, rawStrings: []ast.PdfString{"\x01\xEF"}}

			got := checkType0PerGlyphToUnicode(ctx, doc, fontDict, "F1", 1, "PDF/UA-2", usage)
			if fired := len(got) != 0; fired != tt.wantFire {
				t.Errorf("fired = %v (%v), want %v", fired, got, tt.wantFire)
			}
		})
	}
}
