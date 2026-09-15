package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestType3StencilGlyphColour pins the device colour rules (ISO 19005-1,
// 6.2.3.3 t1; ISO 19005-2, 6.2.4.3 t2) on colour operators inside a Type3
// glyph description. One that opens with d1 is a stencil mask whose colour
// operators shall be ignored (ISO 32000-1, 9.6.5, Table 113), so its rg or RG
// uses no DeviceRGB; one that opens with d0 paints in its own colours, and its
// rg does. Every case was measured against veraPDF.
func TestType3StencilGlyphColour(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /X def " +
		"1 begincodespacerange <00> <FF> endcodespacerange 1 beginbfchar <41> <0041> endbfchar endcmap end end"

	tests := []struct {
		name  string
		glyph string
		rgb   bool
	}{
		{"a d1 glyph setting rg", "1000 0 0 0 1000 1000 d1\n1 0 0 rg\n0 0 1000 1000 re f", false},
		{"a d1 glyph setting RG", "1000 0 0 0 1000 1000 d1\n1 0 0 RG\n0 0 1000 1000 re S", false},
		{"a d1 glyph without colour", "1000 0 0 0 1000 1000 d1\n0 0 1000 1000 re f", false},
		{"a d0 glyph setting rg", "1000 0 d0\n1 0 0 rg\n0 0 1000 1000 re f", true},
	}
	for _, tt := range tests {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{{models.PDFA1b, Rule_19005_1_6_2_3_3_t01}, {models.PDFA2b, Rule_19005_2_6_2_4_3_t02}} {
			t.Run(fmt.Sprintf("%s, %s", tt.name, p.profile), func(t *testing.T) {
				t.Parallel()
				body := "BT /F1 12 Tf 10 10 Td (A) Tj ET"
				doc, _ := objsDocPDF(t,
					"<< /Type /Catalog /Pages 2 0 R >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
					fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
					"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]"+
						" /CharProcs << /a 6 0 R >> /Encoding << /Type /Encoding /Differences [65 /a] >>"+
						" /FirstChar 65 /LastChar 65 /Widths [1000] /ToUnicode 7 0 R >>",
					fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.glyph), tt.glyph),
					fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
				)
				viols, err := ValidatePDFA(ctx, doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule) > 0; got != tt.rgb {
					t.Errorf("%s t%d fired=%v, want %v", p.rule.Clause, p.rule.TestNumber, got, tt.rgb)
				}
			})
		}
	}
}
