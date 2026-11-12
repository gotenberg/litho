package ooxml

import (
	"bytes"
	"errors"
	"testing"
)

// TestDeobfuscateFont locks the obfuscation of ECMA-376 Part 1, 17.8.1:
// the first 32 bytes XORed with the key's bytes from the last to the
// first, the rest of the program untouched, and a key that is not a GUID
// or a part shorter than the head refused with the data unchanged.
func TestDeobfuscateFont(t *testing.T) {
	const key = "{00112233-4455-6677-8899-AABBCCDDEEFF}"
	plain := make([]byte, 40)
	for i := range plain {
		plain[i] = byte(i)
	}
	data := bytes.Clone(plain)
	if err := DeobfuscateFont(data, key); err != nil {
		t.Fatal(err)
	}
	if data[0] != 0x00^0xFF || data[15] != 0x0F^0x00 || data[16] != 0x10^0xFF || data[31] != 0x1F^0x00 {
		t.Errorf("head % x, want the key's bytes reversed XORed twice over", data[:32])
	}
	if !bytes.Equal(data[32:], plain[32:]) {
		t.Errorf("tail % x, want it untouched", data[32:])
	}
	if err := DeobfuscateFont(data, key); err != nil || !bytes.Equal(data, plain) {
		t.Errorf("a second pass gives % x, %v; want the plain program", data, err)
	}
	for _, bad := range []string{"", "{0011}", "{00112233-4455-6677-8899-AABBCCDDEEGG}"} {
		if err := DeobfuscateFont(data, bad); !errors.Is(err, ErrFontKey) || !bytes.Equal(data, plain) {
			t.Errorf("key %q: %v, data changed %v; want ErrFontKey and no change", bad, err, !bytes.Equal(data, plain))
		}
	}
	short := plain[:31]
	if err := DeobfuscateFont(short, key); !errors.Is(err, ErrFontTooShort) || !bytes.Equal(short, plain[:31]) {
		t.Errorf("a 31-byte part: %v; want ErrFontTooShort and no change", err)
	}
}
