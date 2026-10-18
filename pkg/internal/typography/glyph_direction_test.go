package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/models"
)

// TestProcessText_AngleFollowsTheAdvance pins that a glyph's angle is the
// direction it advances in on the page: ISO 32000-1, 9.4.4 scales the
// horizontal displacement by Tfs × Th, so a negative font size or a negative
// horizontal scaling, one of the two, turns it a half turn, and both together
// leave it unturned.
//
// # Corpus evidence
//
//   - pdfjs issue14415.pdf: every word set at a negative size, -9 to -15
//     Tf, on a page turned by /Rotate 180, advancing leftward.
//   - pdfcpu pkg_testdata__Paclitaxel.pdf: -12 Tf and -100 Tz under a CTM
//     that flips Y, advancing rightward.
func TestProcessText_AngleFollowsTheAdvance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		tfs, tz  float64
		wantTurn bool
	}{
		{"positive size", 12, 100, false},
		{"negative size", -12, 100, true},
		{"negative scaling", 12, -100, true},
		{"negative size and scaling", -12, -100, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			font := &Font{
				Subtype:   "Type1",
				Encoding:  map[byte]rune{'a': 'a', 'b': 'b'},
				FirstChar: 'a',
				Widths:    []float64{500, 500},
			}
			machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
			state := machine.Current()
			state.Text.Tm = [6]float64{1, 0, 0, 1, 300, 400}
			state.Text.Tfs = tc.tfs
			state.Text.Tz = tc.tz

			var elements []models.TextElement
			if err := processText(ctx, ast.PdfString("ab"), font, machine, &elements); err != nil {
				t.Fatalf("processText: %v", err)
			}
			if len(elements) != 2 {
				t.Fatalf("got %d elements, want 2", len(elements))
			}
			want := 0.0
			if tc.wantTurn {
				want = math.Pi
			}
			for _, el := range elements {
				if math.Abs(math.Abs(el.Angle)-want) > 1e-9 {
					t.Errorf("%q: angle %v, want %v", el.Text, el.Angle, want)
				}
			}
			// The second glyph sits one advance along the angle.
			step := elements[1].X - elements[0].X
			if got := math.Cos(elements[0].Angle) * elements[0].Width; math.Abs(step-got) > 1e-9 {
				t.Errorf("second glyph %v from the first, want %v along the angle", step, got)
			}
		})
	}
}
