package filter

import (
	"bytes"
	"encoding/ascii85"
	"encoding/hex"
	"testing"
)

// maxASCIIFuzzInput bounds a fuzz input so one iteration stays fast.
const maxASCIIFuzzInput = 1 << 20

// FuzzDecodeASCIIHex feeds arbitrary bytes to decodeASCIIHex, which must not
// panic, and checks that the hexadecimal encoding of those bytes decodes back
// to them. Bytes returned alongside an error are the digit pairs the data
// opens with.
func FuzzDecodeASCIIHex(f *testing.F) {
	f.Add([]byte("48656C6C6F>"))
	f.Add([]byte("48656C6C6")) // Truncated: no EOD marker, odd digit count.
	f.Add([]byte("4865G6C6F>"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxASCIIFuzzInput {
			return
		}
		if prefix, err := decodeASCIIHex(data); err != nil {
			var digits []byte
			for _, c := range data {
				if !isWhiteSpace(c) {
					digits = append(digits, c)
				}
			}
			n := 2 * len(prefix)
			if n > len(digits) || !bytes.EqualFold(digits[:n], []byte(hex.EncodeToString(prefix))) {
				t.Fatalf("decodeASCIIHex(%q) returned %x alongside %v, which the data does not open with", data, prefix, err)
			}
		}

		enc := append([]byte(hex.EncodeToString(data)), '>')
		got, err := decodeASCIIHex(enc)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("decodeASCIIHex(%q) = %x, %v, want %x", enc, got, err, data)
		}
	})
}

// FuzzDecodeASCII85 feeds arbitrary bytes to decodeASCII85, which must not
// panic. Data it accepts holds only white space and groups within 2^32 - 1,
// where encoding/ascii85 decodes correctly, so the two must agree on it. Bytes
// returned alongside an error hold whole groups only. The base-85 encoding of
// the input must also decode back to it.
func FuzzDecodeASCII85(f *testing.F) {
	f.Add([]byte("87cURD]i,\"Ebo7~>"))
	f.Add([]byte("87cURD]i,\"Eb")) // Truncated: no EOD marker, final partial group.
	f.Add([]byte("s8W-\"~>"))      // A group of 2^32.
	f.Add([]byte("87cURuuuuu~>"))  // A group of 2^32 after a valid one.

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxASCIIFuzzInput {
			return
		}
		got, err := decodeASCII85(data)
		if err != nil && len(got)%4 != 0 {
			t.Fatalf("decodeASCII85(%q) returned %d bytes alongside %v, not whole groups", data, len(got), err)
		}
		if err == nil {
			src := data
			if i := bytes.Index(src, []byte("~>")); i != -1 {
				src = src[:i]
			}
			want := make([]byte, 4*len(src))
			n, _, stdErr := ascii85.Decode(want, src, true)
			if stdErr != nil || !bytes.Equal(got, want[:n]) {
				t.Fatalf("decodeASCII85(%q) = %x, encoding/ascii85 = %x, %v", data, got, want[:n], stdErr)
			}
		}

		enc := append(encodeASCII85(data), "~>"...)
		back, err := decodeASCII85(enc)
		if err != nil || !bytes.Equal(back, data) {
			t.Fatalf("decodeASCII85(%q) = %x, %v, want %x", enc, back, err, data)
		}
	})
}
