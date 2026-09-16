package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestType0PerGlyphToUnicode_RenderingMode3IsNotExempt pins that text rendering
// mode 3 does not excuse a code from carrying a Unicode value.
//
// veraPDF spells ISO 14289-2, 8.4.5.8 t1 on object Glyph as `toUnicode != null`
// with no renderingMode term, while ISO 14289-2, 8.4.5.5.1 t1 and t2 both begin
// `renderingMode == 3 ||`. Measured on an Identity-H font showing <017F> against
// a /ToUnicode covering <0000> to <00FF>: veraPDF fails 8.4.5.8 t1 under both
// 0 Tr and 3 Tr, and passes 8.4.5.5.1 under 3 Tr. litho carried the embedding
// rule's exemption into the ToUnicode rule and stayed silent on the invisible
// case.
func TestType0PerGlyphToUnicode_RenderingMode3IsNotExempt(t *testing.T) {
	t.Parallel()

	const tuCMap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"/CMapName /Test def /CMapType 2 def " +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange " +
		"1 beginbfrange <0000> <00FF> <0041> endbfrange endcmap " +
		"CMapName currentdict /CMap defineresource pop end end"

	for _, tt := range []struct {
		name    string
		shown   ast.PdfString
		visible bool
		fires   bool
	}{
		{"a code past the range shown visibly", "\x01\x7F", true, true},
		{"a code past the range shown in mode 3", "\x01\x7F", false, true},
		{"a covered code shown visibly", "\x00\x01", true, false},
		{"a covered code shown in mode 3", "\x00\x01", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H "+
					"/DescendantFonts [3 0 R] /ToUnicode 4 0 R >>",
				"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /DW 1000 >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tuCMap), tuCMap),
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{
				visible:    tt.visible,
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
