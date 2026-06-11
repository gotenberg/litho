package remediation

import (
	"strings"
	"testing"
)

// TestCmapHasForbiddenDestination guards the destination-only scan: the
// previous whole-stream substring check matched the "<0000> <FFFF>"
// codespacerange declaration, condemning nearly every valid ToUnicode CMap
// to replacement by a regenerated guess.
func TestCmapHasForbiddenDestination(t *testing.T) {
	t.Parallel()
	valid := []byte("1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n2 beginbfchar\n<1F> <001F>\n<0000> <0041>\nendbfchar\n")
	if cmapHasForbiddenDestination(valid) {
		t.Error("codespacerange and source codes must not trip the forbidden-value scan")
	}
	badChar := []byte("1 beginbfchar\n<20> <0000>\nendbfchar\n")
	if !cmapHasForbiddenDestination(badChar) {
		t.Error("bfchar destination U+0000 must be flagged")
	}
	badRange := []byte("1 beginbfrange\n<20> <22> [<0041> <FEFF> <0043>]\nendbfrange\n")
	if !cmapHasForbiddenDestination(badRange) {
		t.Error("bfrange array destination U+FEFF must be flagged")
	}
	rangeOK := []byte("1 beginbfrange\n<0000> <00FF> <0041>\nendbfrange\n")
	if cmapHasForbiddenDestination(rangeOK) {
		t.Error("bfrange source bounds must not trip the scan")
	}
}

// TestGenerateToUnicodeCMap_CodeWidth pins the codespacerange and source
// code width to the font's addressing: one byte for simple fonts, two for
// CID fonts. A two-byte codespace over one-byte page strings misaligns
// every conforming reader's lookup.
func TestGenerateToUnicodeCMap_CodeWidth(t *testing.T) {
	t.Parallel()
	one := string(generateToUnicodeCMap(map[uint16]rune{0x41: 'A'}, 1))
	if !strings.Contains(one, "<00> <FF>") || !strings.Contains(one, "<41> <0041>") {
		t.Errorf("one-byte CMap malformed:\n%s", one)
	}
	two := string(generateToUnicodeCMap(map[uint16]rune{0x0041: 'A'}, 2))
	if !strings.Contains(two, "<0000> <FFFF>") || !strings.Contains(two, "<0041> <0041>") {
		t.Errorf("two-byte CMap malformed:\n%s", two)
	}
}
