package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestStripControlBytesFromOps_CompositeFont pins that a string shown with a
// composite font keeps its bytes.
//
// A Type 0 font addresses glyphs with multi-byte codes (ISO 32000-1, 9.7.6),
// so under Identity-H every CID below U+2000 carries a 0x00 high byte.
// Stripping those bytes deletes half of each code and slides the remainder into
// garbage.
//
// # Corpus evidence
//
//   - pdfjs issue2856.pdf: "A different method" extracted as "㼼8E8AG@8" and the
//     file lost a third of its text.
func TestStripControlBytesFromOps_CompositeFont(t *testing.T) {
	// <0041 0020 0042>: three 2-byte CIDs, each with a 0x00 high byte.
	cids := ast.PdfString([]byte{0x00, 0x41, 0x00, 0x20, 0x00, 0x42})

	composite := []content.Operation{
		{Operator: "BT"},
		{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName("F1"), ast.PdfInt(12)}},
		{Operator: "Tj", Operands: []ast.PdfObject{cids}},
		{Operator: "ET"},
	}
	removed, modified := stripControlBytesFromOps(composite, map[string]textFontInfo{"F1": {composite: true}})
	if modified || removed != 0 {
		t.Errorf("composite font: stripped %d byte(s), want none", removed)
	}
	if got := composite[2].Operands[0].(ast.PdfString); string(got) != string(cids) {
		t.Errorf("composite font operand = %q, want %q", got, cids)
	}

	// A simple font keeps the existing behaviour: control bytes go.
	simple := []content.Operation{
		{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName("F2"), ast.PdfInt(12)}},
		{Operator: "Tj", Operands: []ast.PdfObject{ast.PdfString([]byte{0x41, 0x01, 0x42})}},
	}
	removed, modified = stripControlBytesFromOps(simple, map[string]textFontInfo{"F1": {composite: true}})
	if !modified || removed != 1 {
		t.Errorf("simple font: stripped %d byte(s), want 1", removed)
	}
	if got := simple[1].Operands[0].(ast.PdfString); string(got) != "AB" {
		t.Errorf("simple font operand = %q, want %q", got, "AB")
	}

	// The guard follows /Tf: the same page may switch back to a simple font.
	mixed := []content.Operation{
		{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName("F1"), ast.PdfInt(12)}},
		{Operator: "Tj", Operands: []ast.PdfObject{cids}},
		{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName("F2"), ast.PdfInt(12)}},
		{Operator: "Tj", Operands: []ast.PdfObject{ast.PdfString([]byte{0x43, 0x02})}},
	}
	removed, _ = stripControlBytesFromOps(mixed, map[string]textFontInfo{"F1": {composite: true}})
	if removed != 1 {
		t.Errorf("mixed page: stripped %d byte(s), want 1 (only the simple font's)", removed)
	}
	if got := mixed[1].Operands[0].(ast.PdfString); string(got) != string(cids) {
		t.Errorf("mixed page: composite operand was modified: %q", got)
	}
}

// TestStripControlBytesFromOps_DefinedCodes pins that a code the font's
// /Encoding gives a glyph for is never stripped.
//
// The fixer exists to remove .notdef references, and a code named by
// /Differences is by definition not one. A Symbol font mapping
// "1 /minute /lessequal /pi" lost its ≤ from a 148 KB document, and a
// Times-Roman mapping "1 /hyphen 2 /space" lost its hyphens.
func TestStripControlBytesFromOps_DefinedCodes(t *testing.T) {
	// Codes 0x01 (lessequal) and 0x02 (pi) are defined; 0x03 is not.
	ops := []content.Operation{
		{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName("F1"), ast.PdfInt(12)}},
		{Operator: "Tj", Operands: []ast.PdfObject{ast.PdfString([]byte{0x41, 0x01, 0x02, 0x03, 0x42})}},
	}
	fonts := map[string]textFontInfo{
		"F1": {defined: map[byte]bool{0x01: true, 0x02: true}},
	}

	removed, modified := stripControlBytesFromOps(ops, fonts)
	if !modified || removed != 1 {
		t.Fatalf("stripped %d byte(s), want 1 (only the undefined code)", removed)
	}
	got := []byte(ops[1].Operands[0].(ast.PdfString))
	want := []byte{0x41, 0x01, 0x02, 0x42}
	if string(got) != string(want) {
		t.Errorf("operand = % x, want % x", got, want)
	}
}
