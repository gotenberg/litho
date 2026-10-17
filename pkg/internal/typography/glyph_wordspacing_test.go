package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/models"
)

// wordSpacingRun shows s in font at 10 pt with Tw 5 and returns the X of
// each element processText emits.
func wordSpacingRun(t *testing.T, font *Font, s string) []float64 {
	t.Helper()
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	state := machine.Current()
	state.Text.Tm = [6]float64{1, 0, 0, 1, 0, 0}
	state.Text.Tfs = 10
	state.Text.Tw = 5

	var elements []models.TextElement
	if err := processText(context.Background(), ast.PdfString(s), font, machine, &elements); err != nil {
		t.Fatalf("processText: %v", err)
	}
	xs := make([]float64, len(elements))
	for i, el := range elements {
		xs[i] = el.X
	}
	return xs
}

// TestProcessText_WordSpacingTwoByteSpace pins that Tw skips a space shown
// by a two-byte code (ISO 32000-1, 9.3.3): the word after an Identity-H
// space starts one space width after it, not one space plus Tw.
func TestProcessText_WordSpacingTwoByteSpace(t *testing.T) {
	t.Parallel()

	src := "begincmap\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		"2 beginbfchar\n<0003> <0020>\n<0024> <0041>\nendbfchar\n" +
		"endcmap\n"
	cm, err := cmap.Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("cmap.Parse: %v", err)
	}
	font := &Font{
		Subtype:      "Type0",
		ToUnicode:    cm,
		CIDWidths:    map[int]float64{0x03: 250, 0x24: 500},
		MissingWidth: 1000,
	}

	xs := wordSpacingRun(t, font, "\x00\x24\x00\x03\x00\x24")
	if len(xs) != 3 {
		t.Fatalf("got %d elements, want 3", len(xs))
	}
	if want := 7.5; math.Abs(xs[2]-want) > 1e-9 {
		t.Errorf("second A at x=%v, want %v (no word spacing on a two-byte code)", xs[2], want)
	}
}

// TestProcessText_WordSpacingSingleByteSpace is the control: the
// single-byte code 32 of a simple font takes Tw.
func TestProcessText_WordSpacingSingleByteSpace(t *testing.T) {
	t.Parallel()

	font := &Font{
		Subtype:   "Type1",
		Encoding:  map[byte]rune{'A': 'A', ' ': ' '},
		FirstChar: 32,
		Widths:    append(append([]float64{250}, make([]float64, 32)...), 500),
	}

	xs := wordSpacingRun(t, font, "A A")
	if len(xs) != 3 {
		t.Fatalf("got %d elements, want 3", len(xs))
	}
	if want := 12.5; math.Abs(xs[2]-want) > 1e-9 {
		t.Errorf("second A at x=%v, want %v (word spacing on code 32)", xs[2], want)
	}
}
