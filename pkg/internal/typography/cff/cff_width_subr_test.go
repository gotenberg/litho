package cff

import (
	"context"
	"os"
	"testing"
)

// TestGlyphWidths_SubrBeforeStem guards the width scanner's subroutine
// expansion. FrutigerLTStd-Light (a CIDFontType0 CFF extracted from pdfjs
// bug898853) encodes glyph 'C' (GID 36) as `83 -20 callsubr hstem ...`: the
// width delta 83 precedes a callsubr that runs before the first stem operator.
// nominalWidthX is 584, so the true advance is 667; defaultWidthX is 222. A
// scanner that clears the operand stack on callsubr loses the width and
// reports 222, which fired a spurious CIDFont /W width-consistency violation
// (ISO 32000-1 §9.7.4.3; PDF/A 6.2.11.5 / PDF/UA 8.4.5.6) that veraPDF passes.
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
		t.Errorf("GID 36 ('C') width = %d, want 667 (nominalWidthX 584 + delta 83); a subr-clearing scanner returns defaultWidthX 222", got)
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
		t.Errorf("width = %d, want 83", *w)
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
		t.Errorf("width = %d, want nil (even operand count carries no width)", *w)
	}
}
