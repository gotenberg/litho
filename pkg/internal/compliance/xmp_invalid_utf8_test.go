package compliance

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestXMPRepairForParse_InvalidUTF8 covers a packet carrying a byte run that is
// not valid UTF-8. XML 1.0 (Fifth Edition) §2.2 admits only characters, so such
// a run is not well-formed by the letter of the clause, but veraPDF's XMPCore
// relaxes exactly this: measured against veraPDF 1.30.2, a raw 0xFF 0xFE run in
// character data draws neither an invalid-serialization verdict nor an encoding
// one, and the packet's PDF/A identification still reads. Refusing to parse
// cost litho all three.
func TestXMPRepairForParse_InvalidUTF8(t *testing.T) {
	t.Parallel()

	packet := "<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><dc:t xmlns:dc=\"urn:d\">litho \xff\xfe corpus</dc:t></x:xmpmeta>"

	if utf8.ValidString(packet) {
		t.Fatal("the probe must carry invalid UTF-8 for this test to mean anything")
	}
	repaired := xmpRepairForParse([]byte(packet))
	if !utf8.Valid(repaired) {
		t.Errorf("repaired packet is still not valid UTF-8: %q", repaired)
	}
	if !xmpWellFormed([]byte(packet)) {
		t.Error("a packet whose only defect is an invalid byte run must parse")
	}

	// The repair must not disturb the surrounding markup, so a genuine
	// well-formedness defect alongside it is still caught.
	if strings.Contains(string(repaired), "<dc:t") == false {
		t.Errorf("repair damaged the markup: %q", repaired)
	}
	broken := "<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><dc:t xmlns:dc=\"urn:d\">litho \xff\xfe corpus</dc:x></x:xmpmeta>"
	if xmpWellFormed([]byte(broken)) {
		t.Error("a mismatched end tag must still fail, invalid bytes or not")
	}

	// A control character keeps its existing repair, and a clean packet is
	// untouched.
	if !xmpWellFormed([]byte("<r>a\x07b</r>")) {
		t.Error("a C0 control character must still be repaired before parsing")
	}
	clean := []byte("<r>plain</r>")
	if string(xmpRepairForParse(clean)) != string(clean) {
		t.Errorf("a clean packet must pass through unchanged, got %q", xmpRepairForParse(clean))
	}
}
