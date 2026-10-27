package cmap

import (
	"context"
	"slices"
	"testing"
)

// zeroWidthCMapSrc declares a codespace range and a bfchar entry with the
// empty hex string "<>". Both are syntactically well-formed PostScript, so the
// CMap parses; neither describes a character code.
const zeroWidthCMapSrc = "begincmap\n" +
	"1 begincodespacerange\n<> <>\nendcodespacerange\n" +
	"1 beginbfchar\n<> <0041>\nendbfchar\n" +
	"endcmap\n"

// TestParse_ZeroWidthCodespaceIsRejected pins that an empty "<>" codespace
// never reaches [CMap.CodeSpaceRanges]. Per ISO 32000-1, 9.7.6.2, a codespace
// range is one to four bytes wide; a stored zero-width entry makes
// [CMap.codeLengths] report 0 and every lookup consume nothing.
func TestParse_ZeroWidthCodespaceIsRejected(t *testing.T) {
	t.Parallel()

	cm, err := Parse(context.Background(), []byte(zeroWidthCMapSrc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cm == nil {
		t.Fatal("Parse returned a nil CMap")
	}
	for i, cs := range cm.CodeSpaceRanges {
		if len(cs.Low) == 0 {
			t.Errorf("CodeSpaceRanges[%d] has a zero-width Low, want every range >= 1 byte", i)
		}
	}
	if slices.Contains(cm.codeLengths(), 0) {
		t.Errorf("codeLengths() = %v, want no zero entry", cm.codeLengths())
	}
}

// TestMap_ZeroWidthCodespaceConsumesAtLeastOneByte builds the CMap directly
// rather than through [Parse], so it pins the guard inside
// [CMap.mapWithCodeSpaces] on its own: a caller walking a string by the width
// Map reports must always make progress, whatever a malformed CMap declares.
func TestMap_ZeroWidthCodespaceConsumesAtLeastOneByte(t *testing.T) {
	t.Parallel()

	cm := &CMap{
		CodeSpaceRanges: []CodeSpaceRange{{Low: []byte{}, High: []byte{}}},
		Mapping:         map[string]string{"": "A"},
	}

	n, val, ok := cm.Map([]byte("AB"))
	if ok && n == 0 {
		t.Fatalf("Map = (n=%d, %q, ok=%v), want no match that consumes zero bytes", n, val, ok)
	}
}

// TestMap_ParsedZeroWidthCMapConsumesAtLeastOneByte is the end-to-end pairing:
// a document-supplied CMap carrying only "<>" entries must not stall a caller.
func TestMap_ParsedZeroWidthCMapConsumesAtLeastOneByte(t *testing.T) {
	t.Parallel()

	cm, err := Parse(context.Background(), []byte(zeroWidthCMapSrc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	n, val, ok := cm.Map([]byte("AB"))
	if ok && n == 0 {
		t.Fatalf("Map = (n=%d, %q, ok=%v), want no match that consumes zero bytes", n, val, ok)
	}
}

// TestMap_WellFormedCodespaceStillMaps is the control. The guards above reject
// a zero width, so a normal one-byte codespace must still resolve: a test that
// only proves the malformed case is refused would also pass if Map stopped
// mapping anything at all.
func TestMap_WellFormedCodespaceStillMaps(t *testing.T) {
	t.Parallel()

	src := "begincmap\n" +
		"1 begincodespacerange\n<00> <FF>\nendcodespacerange\n" +
		"1 beginbfchar\n<41> <0041>\nendbfchar\n" +
		"endcmap\n"

	cm, err := Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cm.CodeSpaceRanges) != 1 {
		t.Fatalf("CodeSpaceRanges = %d, want 1", len(cm.CodeSpaceRanges))
	}

	n, val, ok := cm.Map([]byte("AB"))
	if !ok || n != 1 || val != "A" {
		t.Fatalf("Map = (n=%d, %q, ok=%v), want (1, \"A\", true)", n, val, ok)
	}
}
