package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestStripControlBytesFromOps_RestoresTheFontOnQ pins that a font selected
// inside q…Q does not outlive the Q.
//
// Per ISO 32000-1, 8.4.2 and Table 104 the text font is part of the graphics
// state, so Q restores it. stripControlBytesFromOps tracks currentFont from Tf
// alone and has no q or Q case, so a font selected inside a saved state stayed
// current afterwards and fonts[currentFont] was consulted for the wrong
// resource name.
//
// That matters more here than in a collector that only labels things: this is a
// safetyContent fixer that REWRITES the content stream. The composite guard
// exists because dropping bytes from multi-byte codes deletes half of each code
// and slides the rest, which turned "A different method" into mojibake and cost
// issue2856.pdf a third of its text. Consulting it for the wrong font either
// strips a composite string, corrupting it, or skips a simple one that should
// have been cleaned.
//
// The ordering below is the discriminating one. F1 is composite and selected
// outside the q; F2 is simple and selected inside it. After the Q the font is
// F1 again, so the CIDs shown there must survive untouched.
func TestStripControlBytesFromOps_RestoresTheFontOnQ(t *testing.T) {
	// <0041 0020 0042>: three 2-byte CIDs, each carrying a 0x00 high byte,
	// exactly the bytes the control-byte strip would eat.
	cids := ast.PdfString([]byte{0x00, 0x41, 0x00, 0x20, 0x00, 0x42})

	tf := func(name string) content.Operation {
		return content.Operation{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName(name), ast.PdfInt(12)}}
	}
	fonts := map[string]textFontInfo{
		"F1": {composite: true},
		"F2": {},
	}

	ops := []content.Operation{
		tf("F1"),
		{Operator: "q"},
		tf("F2"),
		{Operator: "Q"},
		{Operator: "Tj", Operands: []ast.PdfObject{cids}},
	}

	removed, modified := stripControlBytesFromOps(ops, fonts)
	if removed != 0 || modified {
		t.Errorf("stripped %d byte(s) after the Q, want none: the simple font selected "+
			"inside the q…Q outlived the Q, so the composite guard was consulted for F2", removed)
	}
	if got := ops[4].Operands[0].(ast.PdfString); string(got) != string(cids) {
		t.Errorf("composite operand = %q, want %q: its 0x00 high bytes were eaten", got, cids)
	}
}

// TestStripControlBytesFromOps_QDoesNotResurrectAnOuterSimpleFont is the
// control that keeps the fix honest in the other direction.
//
// Restoring on Q must not make the fixer skip work it should do: with a simple
// font outside the q and a composite one inside, the text after the Q belongs
// to the simple font and its control byte must still go.
func TestStripControlBytesFromOps_QDoesNotResurrectAnOuterSimpleFont(t *testing.T) {
	tf := func(name string) content.Operation {
		return content.Operation{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName(name), ast.PdfInt(12)}}
	}
	fonts := map[string]textFontInfo{
		"F1": {composite: true},
		"F2": {},
	}

	ops := []content.Operation{
		tf("F2"),
		{Operator: "q"},
		tf("F1"),
		{Operator: "Q"},
		{Operator: "Tj", Operands: []ast.PdfObject{ast.PdfString([]byte{0x41, 0x01, 0x42})}},
	}

	removed, modified := stripControlBytesFromOps(ops, fonts)
	if removed != 1 || !modified {
		t.Errorf("stripped %d byte(s), want 1: the simple font outside the q…Q must be "+
			"restored by the Q, so its control byte is still removed", removed)
	}
	if got := ops[4].Operands[0].(ast.PdfString); string(got) != "AB" {
		t.Errorf("simple operand = %q, want %q", got, "AB")
	}
}
