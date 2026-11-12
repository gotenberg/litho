package remediation

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// nameKeyedCFFProgram builds a name-keyed CFF program, one whose Top DICT
// carries no ROS operator, of glyphs glyphs that each hold only endchar.
func nameKeyedCFFProgram(glyphs int) []byte {
	header := []byte{1, 0, 4, 4}
	name := []byte{0, 1, 1, 1, 2, 'A'}
	// The Top DICT INDEX is 11 bytes and the String and Global Subr INDEXes
	// are empty, so the CharStrings INDEX follows them at a fixed offset.
	charStringsOff := len(header) + len(name) + 11 + 4
	topDict := []byte{
		0, 1, 1, 1, 7,
		29, 0, 0, 0, byte(charStringsOff), 17, // CharStrings
	}
	empty := []byte{0, 0}
	charStrings := []byte{byte(glyphs >> 8), byte(glyphs), 1}
	for i := range glyphs + 1 {
		charStrings = append(charStrings, byte(i+1))
	}
	for range glyphs {
		charStrings = append(charStrings, 14) // endchar
	}
	return slices.Concat(header, name, topDict, empty, empty, charStrings)
}

// TestProgramCIDs_ReadsANameKeyedCFFByGlyphIndex pins the CIDs the /CIDSet
// repair lists for a CIDFontType0 over a name-keyed CFF program. Such a
// program has no CIDs of its own and the CIDFont reads it by glyph index
// (ISO 32000-1, 9.7.4.2), so its CIDs are the GIDs from 1. Listing none
// left the descriptor without the /CIDSet ISO 19005-1, 6.3.5 t3 requires.
//
// # Corpus evidence
//
//   - poi/docx: document/heading123.docx converts with a subset of Nimbus
//     Roman, a name-keyed CFF, and kept "CIDFont subset missing /CIDSet" at
//     PDF/A-1b; with the set written, veraPDF 1.30 finds the file compliant.
func TestProgramCIDs_ReadsANameKeyedCFFByGlyphIndex(t *testing.T) {
	t.Parallel()

	cidFont := ast.PdfDict{"Subtype": ast.PdfName("CIDFontType0")}
	got := slices.Sorted(maps.Keys(programCIDs(context.Background(), nil, cidFont, nameKeyedCFFProgram(4))))
	if want := []uint16{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("CIDs %v, want %v", got, want)
	}
}
