package cff

import (
	"bytes"
	"testing"
)

// minimalCFF assembles the smallest CFF that carries a Top DICT: a header, a
// one-entry Name INDEX and a Top DICT INDEX. When encoding is negative the Top
// DICT states no Encoding operator at all, which per the CFF specification
// leaves the default of 0, the Standard encoding.
//
// [PredefinedEncodingNames] reads the header and the Top DICT and nothing else,
// so a fixture needs neither a charset nor charstrings. That is the whole point
// of the function: it answers what the encoding assigns without consulting the
// glyphs the font happens to embed.
func minimalCFF(encoding int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{1, 0, 4, 2}) // major, minor, hdrSize, offSize

	// Name INDEX: one entry, "X".
	buf.Write([]byte{0x00, 0x01, 0x01, 0x01, 0x02, 'X'})

	var dict []byte
	if encoding >= 0 {
		// Operands in -107..107 encode as a single byte of value+139.
		dict = append(dict, byte(encoding+139), 16) // 16 = Encoding
	}

	// Top DICT INDEX: one entry holding dict.
	buf.Write([]byte{0x00, 0x01, 0x01, 0x01, byte(1 + len(dict))})
	buf.Write(dict)
	return buf.Bytes()
}

// TestPredefinedEncodingNames pins that a CFF declaring a predefined encoding
// reports the whole table, not the part its charset happens to cover.
//
// Code 0x61 is the discriminator: the Expert encoding assigns it Hsmall while
// the Standard encoding assigns it "a". A subset font that embeds neither still
// means one of them by that code, which is what a caller asking for a Unicode
// value needs to know.
func TestPredefinedEncodingNames(t *testing.T) {
	t.Parallel()

	t.Run("Expert reports the full table", func(t *testing.T) {
		t.Parallel()
		got := PredefinedEncodingNames(minimalCFF(1))
		if got[0x61] != "Hsmall" {
			t.Errorf("code 0x61 = %q, want Hsmall", got[0x61])
		}
		if got[0x5A] != "Asmall" {
			t.Errorf("code 0x5A = %q, want Asmall", got[0x5A])
		}
		if got[0x20] != "space" {
			t.Errorf("code 0x20 = %q, want space", got[0x20])
		}
		// The table is sparse, so count rather than assert a fixed length.
		if len(got) < 100 {
			t.Errorf("Expert table returned %d codes, want the full table", len(got))
		}
	})

	t.Run("Standard reports the full table", func(t *testing.T) {
		t.Parallel()
		got := PredefinedEncodingNames(minimalCFF(0))
		if got[0x61] != "a" {
			t.Errorf("code 0x61 = %q, want a", got[0x61])
		}
		if got[0x20] != "space" {
			t.Errorf("code 0x20 = %q, want space", got[0x20])
		}
	})

	t.Run("an absent Encoding operator defaults to Standard", func(t *testing.T) {
		t.Parallel()
		if got := PredefinedEncodingNames(minimalCFF(-1)); got[0x61] != "a" {
			t.Errorf("code 0x61 = %q, want a (default encoding 0)", got[0x61])
		}
	})

	t.Run("a custom encoding offset reports nothing", func(t *testing.T) {
		t.Parallel()
		// Offset 100 is neither predefined table; the font builds its own
		// encoding and ExtractEncoding is the function that reads it.
		if got := PredefinedEncodingNames(minimalCFF(100)); got != nil {
			t.Errorf("custom encoding offset returned %d codes, want nil", len(got))
		}
	})

	t.Run("junk is rejected", func(t *testing.T) {
		t.Parallel()
		for _, data := range [][]byte{nil, {}, {1}, {9, 0, 4, 2}} {
			if got := PredefinedEncodingNames(data); got != nil {
				t.Errorf("PredefinedEncodingNames(%v) = %v, want nil", data, got)
			}
		}
	})
}
