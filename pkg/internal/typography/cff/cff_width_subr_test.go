package cff

import (
	"context"
	"math"
	"os"
	"testing"
)

// TestGlyphWidths_SubrBeforeStem guards the width scanner's subroutine
// expansion. The FrutigerLTStd-Light fixture (a CIDFontType0 CFF) encodes
// glyph 'C' (GID 36) as `83 -20 callsubr hstem ...`: the width delta 83
// precedes a callsubr that runs before the first stem operator. nominalWidthX
// is 584, so the true advance is 667; defaultWidthX is 222. A scanner that
// clears the operand stack on callsubr loses the width and reports 222, which
// fires a spurious CIDFont /W width-consistency violation (ISO 32000-1
// §9.7.4.3; PDF/A 6.2.11.5 / PDF/UA 8.4.5.6).
//
// # Corpus evidence
//
//   - pdfjs bug898853.pdf: the source of the FrutigerLTStd-Light fixture. The
//     stack-clearing scanner fired the /W violation on it, and veraPDF passes.
func TestGlyphWidths_SubrBeforeStem(t *testing.T) {
	data, err := os.ReadFile("testdata/frutiger_subr_width.cff")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	widths, err := GlyphWidths(context.Background(), data)
	if err != nil {
		t.Fatalf("GlyphWidths: %v", err)
	}
	if got := widths[36]; got != 667 {
		t.Errorf("GID 36 ('C') width = %g, want 667 (nominalWidthX 584 + delta 83); a subr-clearing scanner returns defaultWidthX 222", got)
	}
}

// TestExtractCharstringWidth_CallSubrBeforeStem checks that a width operand
// pushed before a callsubr survives the subroutine expansion.
func TestExtractCharstringWidth_CallSubrBeforeStem(t *testing.T) {
	// Local subr 0 pushes two hint operands then returns.
	subr0 := append(append(encodeCFFInt(10), encodeCFFInt(20)...), 11) // 11 = return
	local := [][]byte{subr0}

	// Bias for a 1-entry local subr INDEX is 107, so index 0 is called as -107.
	// Main: width 83, callsubr(0), hstem. After the subr the stack is
	// [83, 10, 20]; hstem with an odd count means 83 is the width.
	cs := append(encodeCFFInt(83), encodeCFFInt(-107)...)
	cs = append(cs, 10) // callsubr
	cs = append(cs, 1)  // hstem

	w := extractCharstringWidth(cs, local, nil)
	if w == nil {
		t.Fatal("expected a width, got nil (subr expansion dropped the leading operand)")
	}
	if *w != 83 {
		t.Errorf("width = %g, want 83", *w)
	}
}

// TestExtractCharstringWidth_CallGSubrBeforeStem is the global-subroutine
// variant of the callsubr case.
func TestExtractCharstringWidth_CallGSubrBeforeStem(t *testing.T) {
	gsubr0 := append(append(encodeCFFInt(30), encodeCFFInt(40)...), 11)
	global := [][]byte{gsubr0}

	cs := append(encodeCFFInt(90), encodeCFFInt(-107)...)
	cs = append(cs, 29) // callgsubr
	cs = append(cs, 1)  // hstem

	w := extractCharstringWidth(cs, nil, global)
	if w == nil || *w != 90 {
		t.Errorf("width = %v, want 90", w)
	}
}

// TestReadUnitsPerEm_CIDFDArrayFontMatrix pins that a CID-keyed CFF whose
// FontMatrix lives in the FDArray font dict (not the Top DICT) reports its true
// units-per-em. cid_fdarray_matrix.cff has an FD FontMatrix of
// [0.00048828…] = 1/2048; reading only the Top DICT yields the 1000 default
// and fires a spurious CIDFont /W width mismatch (CID 20: /W 629 vs a program
// advance of 1289 = 629 × 2048/1000).
//
// # Corpus evidence
//
//   - pdfjs text_clip_cff_cid.pdf: the source of cid_fdarray_matrix.cff.
func TestReadUnitsPerEm_CIDFDArrayFontMatrix(t *testing.T) {
	data, err := os.ReadFile("testdata/cid_fdarray_matrix.cff")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if got := ReadUnitsPerEm(data); got != 2048 {
		t.Errorf("ReadUnitsPerEm = %d, want 2048 (FDArray FontMatrix); a Top-DICT-only read gives 1000", got)
	}
}

// TestExtractCharstringWidth_HintmaskWidth checks that hintmask, when it is the
// first stack-clearing operator (implicit vstem hints), still surrenders the
// leading width operand. An odd operand count carries a width.
func TestExtractCharstringWidth_HintmaskWidth(t *testing.T) {
	// width 100, then one implicit vstem pair (50 60), then hintmask + mask byte.
	cs := append(encodeCFFInt(100), encodeCFFInt(50)...)
	cs = append(cs, encodeCFFInt(60)...)
	cs = append(cs, 19)   // hintmask
	cs = append(cs, 0xFF) // one mask byte (never read: the scan stops at hintmask)

	w := extractCharstringWidth(cs, nil, nil)
	if w == nil || *w != 100 {
		t.Errorf("width = %v, want 100", w)
	}

	// Even operand count before hintmask: no width.
	cs2 := append(encodeCFFInt(50), encodeCFFInt(60)...)
	cs2 = append(cs2, 19, 0xFF)
	if w := extractCharstringWidth(cs2, nil, nil); w != nil {
		t.Errorf("width = %g, want nil (even operand count carries no width)", *w)
	}
}

// TestExtractCharstringWidth_FixedOperand pins that a 32-bit fixed (16.16)
// width operand keeps its fraction. ISO 19005-2 §6.2.11.5 is judged within one
// unit, so truncating the operand can move a real mismatch onto the tolerance
// boundary and hide it.
//
// # Corpus evidence
//
//   - pdfjs issue3566.pdf: draws a glyph 457.171 wide against a declared 456,
//     a real 1.167-unit mismatch that truncating the operand hides.
func TestExtractCharstringWidth_FixedOperand(t *testing.T) {
	// 457.171 as 16.16: the integer part in the high half, the fraction in the
	// low half. Followed by hstem with an odd operand count, so it is the width.
	const want = 457.171
	fixed := int32(math.Round(want * 65536))
	cs := []byte{255, byte(fixed >> 24), byte(fixed >> 16), byte(fixed >> 8), byte(fixed)}
	cs = append(cs, 1) // hstem

	w := extractCharstringWidth(cs, nil, nil)
	if w == nil {
		t.Fatal("expected a width, got nil")
	}
	if math.Abs(*w-want) > 1.0/65536.0 {
		t.Errorf("width = %g, want %g", *w, want)
	}
}
