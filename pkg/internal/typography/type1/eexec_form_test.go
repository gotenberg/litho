package type1

import "testing"

// TestNormaliseEexec_BinarySaltLooksHex pins that the hex-versus-binary
// decision reads four bytes, not one.
//
// Per the Type 1 Font Format specification (Adobe Systems, 1993), 7.2, the
// encrypted section starts with a four-byte random salt, so a binary payload
// begins with an arbitrary byte that is a hexadecimal digit better than one
// time in twelve. Deciding on the first byte alone hex-decoded such a payload
// into noise and left the program with no readable glyph.
//
// # Corpus evidence
//
//   - govdocs thread_300_300035.pdf: opens its binary section with 0x32
//     ('2'), which cost litho every charstring name and width in the font,
//     and with them the ISO 19005-2 §6.2.11.5 mismatch veraPDF reports on
//     its code 109.
func TestNormaliseEexec_BinarySaltLooksHex(t *testing.T) {
	t.Parallel()

	// This salt encrypts to the bytes 0x41 0x47 0x33 0xf4, so the payload opens
	// with 'A': hexadecimal on its own, and binary from the second byte on.
	salt := []byte{0x98, 0x02, 0x41, 0x42}
	plain := append(salt, "/CharStrings 1 dict dup begin\n/A 4 RD ABCD ND\nend"...)

	cases := []struct {
		name string
		body []byte
		want string
	}{
		{
			"binary payload starting with a hex digit",
			encryptEexec(plain),
			"binary",
		},
		{
			"hex payload",
			hexEncode(encryptEexec(plain)),
			"hex",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParseCharStringNames(append([]byte("currentfile eexec\n"), tc.body...))
			if len(got) != 1 || got[0] != "A" {
				t.Errorf("%s payload parsed to %v, want [A]", tc.want, got)
			}
		})
	}
}

// TestIsHexEncoded pins the four-byte probe itself.
func TestIsHexEncoded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"four hex digits", []byte("2a9fdeadbeef"), true},
		{"hex digits split by whitespace", []byte("2a\n9f de"), true},
		{"one hex digit then binary", []byte{'2', 0x93, 0xc6, 0xd7}, false},
		{"three hex digits then binary", []byte{'2', 'a', '9', 0xd7}, false},
		{"no hex digit at all", []byte{0x93, 0xc6, 0xd7, 0xfe}, false},
		{"shorter than the probe", []byte("2a9"), false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isHexEncoded(tc.data); got != tc.want {
				t.Errorf("isHexEncoded(%q) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

// hexEncode writes bytes in the ASCII-hex form the eexec section may take.
func hexEncode(data []byte) []byte {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(data)*2)
	for _, b := range data {
		out = append(out, digits[b>>4], digits[b&0x0F])
	}
	return out
}
