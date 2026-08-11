package typography

import "testing"

// FuzzReadUnitsPerEm feeds arbitrary bytes to the head-table reader and asserts
// crash-freedom.
//
// The bytes are an embedded font program, so they are attacker-controlled: the
// units-per-em is read straight out of the sfnt head table to scale every glyph
// width, and a truncated or lying table is what a damaged font file supplies.
func FuzzReadUnitsPerEm(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("\x00\x01\x00\x00"))                 // sfnt version alone, no tables
	f.Add([]byte("OTTO\x00\x00\x00\x00"))             // CFF-flavoured, truncated
	f.Add([]byte("true\x00\x01\x00\x00head\x00\x00")) // table tag with no payload
	f.Add(make([]byte, 54))                           // a head table's worth of zeroes

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		// A zero result is the documented "unknown" answer; what matters here
		// is that no input reaches a panic or an out-of-range read.
		_ = ReadUnitsPerEm(data)
	})
}
