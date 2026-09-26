package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// showOffsets runs the text operators of data and returns the horizontal
// position, in unscaled text space, at which each non-empty string a show
// operation paints starts, keyed by that string.
//
// It follows ISO 32000-1, 9.4.3: Td, TD, T*, Tm, ' and " start a line, and a
// number in a TJ array moves the position by minus a thousandth of it times
// the font size and the horizontal scaling. A painted glyph advances the
// position by its width, which the streams compared here share: they differ
// only in the strings the control-byte strip empties, whose codes the font
// gives no glyph and no width, so every glyph is taken as advancing by
// nothing.
func showOffsets(t *testing.T, data []byte) map[string]float64 {
	t.Helper()

	ops, err := content.ParseStreamData(context.Background(), data, content.ForRepair())
	if err != nil {
		t.Fatalf("parsing the content stream: %v", err)
	}
	offsets := make(map[string]float64)
	var lineX, x, size float64
	scale := 1.0
	show := func(operand ast.PdfObject) {
		switch v := operand.(type) {
		case ast.PdfString:
			if len(v) > 0 {
				offsets[string(v)] = x
			}
		case ast.PdfArray:
			for _, elem := range v {
				if s, ok := elem.(ast.PdfString); ok {
					if len(s) > 0 {
						offsets[string(s)] = x
					}
					continue
				}
				x -= document.Float(elem) / 1000 * size * scale
			}
		}
	}
	for _, op := range ops {
		num := func(i int) float64 { return document.Float(op.Operands[i]) }
		switch op.Operator {
		case "BT":
			lineX, x = 0, 0
		case "Td", "TD":
			lineX += num(0)
			x = lineX
		case "Tm":
			lineX = num(4)
			x = lineX
		case "T*":
			x = lineX
		case "Tf":
			size = num(1)
		case "Tz":
			scale = num(0) / 100
		case "Tj", "TJ":
			show(op.Operands[0])
		case "'":
			x = lineX
			show(op.Operands[0])
		case "\"":
			x = lineX
			show(op.Operands[2])
		}
	}
	return offsets
}

// TestFixTextControl_KeepsTheSpacingOfAnEmptiedTJ pins that emptying every
// string of a TJ array keeps the position moves its numbers make.
//
// A number in a TJ array moves the text position by that many thousandths of
// a unit of text space (ISO 32000-1, 9.4.3, Table 109), which is how producers
// write kerning and word gaps. The repair dropped a TJ whose strings it
// emptied whole, numbers included, so every string after it on the line
// started that much further left.
func TestFixTextControl_KeepsTheSpacingOfAnEmptiedTJ(t *testing.T) {
	t.Parallel()

	src := []byte("BT /F1 12 Tf 90 Tz 72 700 Td (A) Tj [(\x01) -2000 (\x02\x03) -500] TJ (B) Tj [(\x04)] TJ (C) Tj [(\x05) 0] TJ (D) Tj ET")
	fonts := map[string]textFontInfo{"F1": {}}
	out, removed, err := repairContentOps(context.Background(), src, func() opRepair {
		return (&controlByteStripper{fonts: fonts}).repair
	})
	if err != nil {
		t.Fatalf("repairContentOps: %v", err)
	}
	if removed != 5 {
		t.Fatalf("removed %d control byte(s), want 5; output = %q", removed, out)
	}

	want := showOffsets(t, src)
	got := showOffsets(t, out)
	for _, text := range []string{"A", "B", "C", "D"} {
		if got[text] != want[text] {
			t.Errorf("%q starts at %g, want %g, where the source shows it; output = %q",
				text, got[text], want[text], out)
		}
	}
	for text := range got {
		if !strings.Contains("ABCD", text) {
			t.Errorf("the output still shows %q; output = %q", text, out)
		}
	}
	// A TJ that moves nothing once emptied is dropped, not written back.
	if n := strings.Count(string(out), "TJ"); n != 1 {
		t.Errorf("the output holds %d TJ operator(s), want 1, the one whose numbers move; output = %q", n, out)
	}
}
