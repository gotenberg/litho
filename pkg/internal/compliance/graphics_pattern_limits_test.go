package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestPatternContentStreamRealLimit pins ISO 19005-1, 6.1.12 t2 on a real a
// tiling pattern writes. The pattern paints its cell with a content stream of
// its own (ISO 32000-1, 8.7.3.1), so an operand there is judged exactly as one
// written on the page: the rule object is CosReal, and veraPDF reports
// pdfjs 22060_A1_01_Plans.pdf six times over patterns /R21 and /R24 while that
// file's 136 KB page stream stays inside the limit.
//
// Only a pattern a scn or SCN selects is judged. veraPDF passes a fixture whose
// unselected pattern carries the same out-of-range real, so the walk is gated
// on selection rather than on declaration.
func TestPatternContentStreamRealLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const outOfRange = "0 -39455.5 18602.6 19727.8 re W n"
	const inRange = "0 -100.5 200.6 300.8 re W n"

	for _, tt := range []struct {
		name    string
		content string
		cell    string
		want    int
	}{
		{"a pattern the page selects", "/Pattern cs /P0 scn 0 0 100 100 re f", outOfRange, 1},
		{"a pattern the page never selects", "0 0 100 100 re S", outOfRange, 0},
		{"a selected pattern inside the limit", "/Pattern cs /P0 scn 0 0 100 100 re f", inRange, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Pattern << /P0 5 0 R >> >> >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.content), tt.content),
				fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"+
					" /Resources << >> /Length %d >>\nstream\n%s\nendstream", len(tt.cell), tt.cell),
			)
			viols, err := ValidatePDFA(ctx, doc, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_1_6_1_12_t02); got != tt.want {
				t.Errorf("6.1.12 t2 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
