package pdf

import (
	"encoding/binary"
	"errors"
	"testing"
)

// sfntHeaderClaiming builds the 12-byte TrueType table directory header for a
// font that claims numTables entries without supplying them. Twelve bytes is
// the shortest input that gets past the length check in [NewTTFFont], so these
// reach the parser rather than the short-circuit.
func sfntHeaderClaiming(numTables uint16) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b[0:4], 0x00010000) // sfntVersion 1.0
	binary.BigEndian.PutUint16(b[4:6], numTables)
	return b
}

// TestNewTTFFont_StructurallyInvalidReturnsTheSentinel pins the documented
// contract of [ErrInvalidFont]: "font data is too short or structurally
// invalid".
//
// Only the first half held. The length check returned the sentinel, but every
// parse failure returned either a bare error string ("ttf data too short",
// "head table too short") or the internal truetype sentinel, neither of which
// errors.Is could match against the public value. Measured before the fix: four
// of these five cases reported false.
//
// The cases are the distinct failure shapes the parser actually produces, not
// variations on one: a missing table, a table directory truncated before its
// records, and a record truncated mid-entry.
func TestNewTTFFont_StructurallyInvalidReturnsTheSentinel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{"too short for a header", make([]byte, 11)},
		{"twelve zero bytes", make([]byte, 12)},
		{"header declaring no tables", sfntHeaderClaiming(0)},
		{"header claiming tables it does not carry", sfntHeaderClaiming(5)},
		{"header with a truncated table record", append(sfntHeaderClaiming(1), 'h', 'e', 'a', 'd')},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewTTFFont(tt.data)
			if err == nil {
				t.Fatalf("NewTTFFont(%s) succeeded, want an error", tt.name)
			}
			if !errors.Is(err, ErrInvalidFont) {
				t.Errorf("NewTTFFont(%s): err = %v, want errors.Is(err, ErrInvalidFont)", tt.name, err)
			}
		})
	}
}

// TestNewTTFFont_AcceptsAValidFont is the control for the test above.
//
// Without it, the sentinel assertions would also pass if NewTTFFont had started
// refusing every input, which is the failure mode a "reject harder" change
// introduces.
func TestNewTTFFont_AcceptsAValidFont(t *testing.T) {
	font, err := NewTTFFont(minimalTTF(t))
	if err != nil {
		t.Fatalf("NewTTFFont(minimalTTF): %v, want a usable font: if this now fails, "+
			"the sentinel assertions above no longer discriminate between a malformed "+
			"font and any font", err)
	}
	if len(font.ttf) == 0 {
		t.Error("the returned StampFont carries no font bytes")
	}
}
