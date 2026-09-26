package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// textLineState is the part of the text state a show operation starts from
// that stripping a control byte must leave alone: where its line starts, and
// the word and character spacing (ISO 32000-1, 9.3 and Table 105).
type textLineState struct {
	x, y, wordSpacing, charSpacing float64
}

// String formats the state for a failure message.
func (s textLineState) String() string {
	return fmt.Sprintf("line (%g, %g) Tw %g Tc %g", s.x, s.y, s.wordSpacing, s.charSpacing)
}

// showStates runs the text positioning and text state operators of data, as
// ISO 32000-1, 9.3 and 9.4, Tables 105, 108 and 109 define them, and returns
// the state each non-empty string a show operation paints starts from, keyed
// by that string. It follows the translation of the text line matrix alone:
// the streams it reads never scale it.
func showStates(t *testing.T, data []byte) map[string]textLineState {
	t.Helper()

	ops, err := content.ParseStreamData(context.Background(), data, content.ForRepair())
	if err != nil {
		t.Fatalf("parsing the content stream: %v", err)
	}
	states := make(map[string]textLineState)
	var s textLineState
	var leading float64
	nextLine := func() { s.y -= leading }
	show := func(operand ast.PdfObject) {
		if str, ok := operand.(ast.PdfString); ok && len(str) > 0 {
			states[string(str)] = s
		}
	}
	for _, op := range ops {
		num := func(i int) float64 { return document.Float(op.Operands[i]) }
		switch op.Operator {
		case "BT":
			s.x, s.y = 0, 0
		case "Td":
			s.x, s.y = s.x+num(0), s.y+num(1)
		case "TD":
			leading = -num(1)
			s.x, s.y = s.x+num(0), s.y+num(1)
		case "Tm":
			s.x, s.y = num(4), num(5)
		case "TL":
			leading = num(0)
		case "Tw":
			s.wordSpacing = num(0)
		case "Tc":
			s.charSpacing = num(0)
		case "T*":
			nextLine()
		case "Tj":
			show(op.Operands[0])
		case "'":
			nextLine()
			show(op.Operands[0])
		case "\"":
			s.wordSpacing, s.charSpacing = num(0), num(1)
			nextLine()
			show(op.Operands[2])
		}
	}
	return states
}

// TestFixTextControl_KeepsTheLineStateOfAnEmptiedQuote pins that emptying the
// string of a ' or " operator keeps the rest of what the operator does.
//
// ' moves to the next line before it shows its string, and aw ac string "
// also sets the word spacing to aw and the character spacing to ac (ISO
// 32000-1, 9.4.3, Table 109). The repair dropped an operation whose string it
// emptied, so the line move and both spacings went with it, and every line
// after it drew one line higher, spaced as the lines before.
func TestFixTextControl_KeepsTheLineStateOfAnEmptiedQuote(t *testing.T) {
	t.Parallel()

	src := []byte("BT /F1 12 Tf 14 TL 72 700 Td (A) Tj 3 1 (\x01) \" (B) Tj (\x02) ' (C) Tj ET")
	fonts := map[string]textFontInfo{"F1": {}}
	out, removed, err := repairContentOps(context.Background(), src, func() opRepair {
		return (&controlByteStripper{fonts: fonts}).repair
	})
	if err != nil {
		t.Fatalf("repairContentOps: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed %d control byte(s), want 2; output = %q", removed, out)
	}

	want := showStates(t, src)
	got := showStates(t, out)
	for _, text := range []string{"A", "B", "C"} {
		if got[text] != want[text] {
			t.Errorf("%q starts from %v, want %v, the state the source gives it; output = %q",
				text, got[text], want[text], out)
		}
	}
	for text := range got {
		if text != "A" && text != "B" && text != "C" {
			t.Errorf("the output still shows %q; output = %q", text, out)
		}
	}
}
