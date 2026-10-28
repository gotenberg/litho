package cmap

import "testing"

// TestByteLength guards the code-width sizing against the truncation bug, where
// a two-byte code whose low byte fell inside a one-byte codespace (e.g. 0xE644,
// low byte 0x44) was mis-sized as one byte.
func TestByteLength(t *testing.T) {
	t.Parallel()
	// Mixed-width Shift-JIS-style codespaces.
	cs := []CodeSpaceRange{
		{Low: []byte{0x00}, High: []byte{0x80}},
		{Low: []byte{0x81, 0x40}, High: []byte{0x9F, 0xFC}},
		{Low: []byte{0xA0}, High: []byte{0xDF}},
		{Low: []byte{0xE0, 0x40}, High: []byte{0xFC, 0xFC}},
	}
	cases := []struct {
		code int64
		want int
	}{
		{0x44, 1},   // ASCII 'D'
		{0xA1, 1},   // half-width katakana, single byte
		{0x8140, 2}, // CJK
		{0xE644, 2}, // CJK whose low byte 0x44 used to mis-size as 1 byte
		{0xFC4B, 2}, // CJK in the upper two-byte range
	}
	for _, c := range cases {
		if got := byteLength(c.code, cs); got != c.want {
			t.Errorf("byteLength(%#x) = %d, want %d", c.code, got, c.want)
		}
	}
}
