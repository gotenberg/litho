package remediation

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// glyphPosition is where a glyph starts in unscaled text space.
type glyphPosition struct{ x, y float64 }

// glyphPositions runs the text operators of data as ISO 32000-1, 9.3 and 9.4
// define them and returns where each glyph above 0x20 a show operation paints
// starts, keyed by its code: every code is shown once in the streams it reads.
// A glyph moves the position by its width, advance for a control code and 0
// for any other, times the font size, plus the character spacing, and the word
// spacing for code 32, all times the horizontal scaling (9.4.4); a number in a
// TJ array moves it by minus a thousandth of it times the font size and the
// horizontal scaling (9.4.3, Table 109). The streams compared share every glyph
// above 0x20, so giving those no width leaves where each starts comparable.
func glyphPositions(t *testing.T, data []byte, advance map[byte]float64) map[byte]glyphPosition {
	t.Helper()

	ops, err := content.ParseStreamData(context.Background(), data, content.ForRepair())
	if err != nil {
		t.Fatalf("parsing the content stream: %v", err)
	}
	type textState struct{ size, scale, charSpacing, wordSpacing, leading float64 }
	st := textState{scale: 1}
	var saved []textState
	var line, pos glyphPosition
	positions := make(map[byte]glyphPosition)
	showString := func(s ast.PdfString) {
		for _, b := range []byte(s) {
			if b > 0x20 {
				positions[b] = pos
			}
			w := advance[b]*st.size + st.charSpacing
			if b == ' ' {
				w += st.wordSpacing
			}
			pos.x += w * st.scale
		}
	}
	show := func(operand ast.PdfObject) {
		switch v := operand.(type) {
		case ast.PdfString:
			showString(v)
		case ast.PdfArray:
			for _, elem := range v {
				if s, ok := elem.(ast.PdfString); ok {
					showString(s)
					continue
				}
				pos.x -= document.Float(elem) / 1000 * st.size * st.scale
			}
		}
	}
	nextLine := func() {
		line.y -= st.leading
		pos = line
	}
	for _, op := range ops {
		num := func(i int) float64 { return document.Float(op.Operands[i]) }
		switch op.Operator {
		case "q":
			saved = append(saved, st)
		case "Q":
			st, saved = saved[len(saved)-1], saved[:len(saved)-1]
		case "BT":
			line, pos = glyphPosition{}, glyphPosition{}
		case "Td":
			line = glyphPosition{line.x + num(0), line.y + num(1)}
			pos = line
		case "TL":
			st.leading = num(0)
		case "Tf":
			st.size = num(1)
		case "Tz":
			st.scale = num(0) / 100
		case "Tc":
			st.charSpacing = num(0)
		case "Tw":
			st.wordSpacing = num(0)
		case "T*":
			nextLine()
		case "Tj", "TJ":
			show(op.Operands[0])
		case "'":
			nextLine()
			show(op.Operands[0])
		case "\"":
			st.wordSpacing, st.charSpacing = num(0), num(1)
			nextLine()
			show(op.Operands[2])
		}
	}
	return positions
}

// TestFixTextControl_KeepsTheRoomOfAStrippedGlyph pins that every glyph the
// control-byte strip keeps starts where the source drew it.
//
// A glyph moves the text position by its width times the font size plus the
// character spacing (ISO 32000-1, 9.4.4), and a code the encoding gives no
// glyph still has a width: its /Widths entry, or the font descriptor's
// /MissingWidth. Removing the code drew every later glyph on the line that
// much further back. The fixture font gives its control codes the 600 units
// of the .notdef glyph they show, as a font whose widths agree with its
// program does.
//
// # Corpus evidence
//
//   - litho repairleft_4.pdf: "2 1 (\x01) \" (AB) Tj" drew AB 8.2 units
//     further left once the strip removed the code, the room the .notdef
//     glyph took with its width of 600 at 12 points and a spacing of 1.
func TestFixTextControl_KeepsTheRoomOfAStrippedGlyph(t *testing.T) {
	t.Parallel()

	advance := map[byte]float64{1: 0.6, 2: 0.6, 3: 0.6, 4: 0.6, 5: 0.6, 6: 0.6, 7: 0.6}
	src := []byte("BT /F1 12 Tf 90 Tz 0.5 Tc 14 TL 72 700 Td (A\x01B) Tj [(C) -250 (\x02\x03D)] TJ (E) Tj " +
		"(\x04) ' (F) Tj 1 2 (\x05G) \" (H) Tj q 0 Tc (I\x06J) Tj Q (K\x07L) Tj ET")
	fonts := map[string]textFontInfo{"F1": {advance: advance}}
	out, removed, err := repairContentOps(context.Background(), src, func() opRepair {
		return (&controlByteStripper{fonts: fonts}).repair
	})
	if err != nil {
		t.Fatalf("repairContentOps: %v", err)
	}
	if removed != 7 {
		t.Fatalf("removed %d control byte(s), want 7; output = %q", removed, out)
	}
	for _, b := range out {
		if b < 0x20 && b != '\n' {
			t.Fatalf("the output keeps control byte %#x; output = %q", b, out)
		}
	}

	want := glyphPositions(t, src, advance)
	got := glyphPositions(t, out, advance)
	for _, glyph := range "ABCDEFGHIJKL" {
		w, g := want[byte(glyph)], got[byte(glyph)]
		if math.Abs(g.x-w.x) > 1e-3 || math.Abs(g.y-w.y) > 1e-3 {
			t.Errorf("%c starts at %v, want %v, where the source draws it; output = %q", glyph, g, w, out)
		}
	}
}

// TestFixTextControl_LeavesARoomlessGlyphsOperationAlone pins that a removed
// glyph that took no room on the line changes nothing but the string: no TJ
// replaces the operation.
func TestFixTextControl_LeavesARoomlessGlyphsOperationAlone(t *testing.T) {
	t.Parallel()

	src := []byte("BT /F1 12 Tf 72 700 Td (A\x01B) Tj ET")
	fonts := map[string]textFontInfo{"F1": {}}
	out, _, err := repairContentOps(context.Background(), src, func() opRepair {
		return (&controlByteStripper{fonts: fonts}).repair
	})
	if err != nil {
		t.Fatalf("repairContentOps: %v", err)
	}
	if want := "BT\n/F1 12 Tf\n72 700 Td\n(AB) Tj\nET\n"; string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}
