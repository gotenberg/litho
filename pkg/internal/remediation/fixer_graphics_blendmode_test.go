package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestRepairGraphicsState_KeepsAStandardBlendMode pins that the graphics
// state repair replaces only a blend mode ISO 32000-1, 11.3.5 does not
// define, the one thing ISO 19005-2 and 19005-3, 6.2.10 t1 and ISO 19005-4,
// 6.2.9 t1 forbid. The repair runs for any graphics rule a file breaks, and
// it set every mode but Normal and Compatible to Normal, so a page lost its
// blending to a rule about its rendering intent or its blending colour
// space.
//
// # Corpus evidence
//
//   - probe, pdf: a page of groups that blend, remediated to PDF/A-4, came
//     back with every blend mode Normal and 3,000 pixels changed, where the
//     validator had reported no blend mode.
func TestRepairGraphicsState_KeepsAStandardBlendMode(t *testing.T) {
	t.Parallel()

	doc, _ := newTestDoc(t, minimalCatalogPDF(""))
	for _, tc := range []struct {
		name string
		gs   ast.PdfDict
		want ast.PdfName
	}{
		// The invalid rendering intent makes the repair rewrite the state,
		// which is when a standard mode was lost.
		{"a standard mode beside another repair", ast.PdfDict{"BM": ast.PdfName("Multiply"), "RI": ast.PdfName("Vivid")}, "Multiply"},
		{"Compatible beside another repair", ast.PdfDict{"BM": ast.PdfName("Compatible"), "RI": ast.PdfName("Vivid")}, "Compatible"},
		{"a mode no table lists", ast.PdfDict{"BM": ast.PdfName("Glow")}, "Normal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repaired, notes := repairGraphicsState(t.Context(), doc, "GS0", tc.gs)
			if repaired == nil {
				t.Fatalf("the state was not repaired, notes=%v", notes)
			}
			if got := repaired["BM"]; got != tc.want {
				t.Errorf("/BM = %v, want %v (notes=%v)", got, tc.want, notes)
			}
		})
	}
}
