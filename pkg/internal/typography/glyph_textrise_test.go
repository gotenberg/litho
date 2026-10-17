package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// TestProcessText_TextRiseShiftsOrigin pins that Ts moves the glyph origin
// off the baseline (ISO 32000-1, 9.3.7): the text rendering matrix of 9.4.4
// places the origin at (0, Trise) in text space, so a superscript shown
// with 4 Ts under a text matrix scaled by 2 sits 8 units above the
// baseline, and 0 Ts brings the next glyph back.
func TestProcessText_TextRiseShiftsOrigin(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	font := &Font{
		Subtype:   "Type1",
		Encoding:  map[byte]rune{'x': 'x', '2': '2'},
		FirstChar: 'x',
		Widths:    []float64{500},
	}
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	show := func(rise float64, s string) []models.TextElement {
		t.Helper()
		op := content.Operation{Operator: "Ts", Operands: []ast.PdfObject{ast.PdfReal(rise)}}
		if err := machine.ExecuteOne(ctx, op); err != nil {
			t.Fatalf("Ts: %v", err)
		}
		var elements []models.TextElement
		if err := processText(ctx, ast.PdfString(s), font, machine, &elements); err != nil {
			t.Fatalf("processText: %v", err)
		}
		return elements
	}
	state := machine.Current()
	state.Text.Tm = [6]float64{2, 0, 0, 2, 100, 700}
	state.Text.Tfs = 10

	base := show(0, "x")
	sup := show(4, "2")
	back := show(0, "x")
	if len(base) != 1 || len(sup) != 1 || len(back) != 1 {
		t.Fatalf("got %d, %d, %d elements, want one each", len(base), len(sup), len(back))
	}
	if want := 708.0; math.Abs(sup[0].Y-want) > 1e-9 {
		t.Errorf("superscript at y=%v, want %v (rise scaled by the text matrix)", sup[0].Y, want)
	}
	if math.Abs(back[0].Y-base[0].Y) > 1e-9 {
		t.Errorf("glyph after 0 Ts at y=%v, want the baseline %v", back[0].Y, base[0].Y)
	}
}
