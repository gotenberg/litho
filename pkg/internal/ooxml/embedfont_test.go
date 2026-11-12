package ooxml

import (
	"bytes"
	"encoding/binary"
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

// eotFile returns a version 0x00010000 Embedded OpenType file of data with
// empty names and flags (W3C Member Submission, Embedded OpenType File
// Format, 3.1).
func eotFile(data []byte, flags uint32) []byte {
	const header = 36 + 16 + 8 + 4 + 16 + 2 + 4*4
	out := make([]byte, header, header+len(data))
	binary.LittleEndian.PutUint32(out[0:], uint32(header+len(data)))
	binary.LittleEndian.PutUint32(out[4:], uint32(len(data)))
	binary.LittleEndian.PutUint32(out[8:], 0x00010000)
	binary.LittleEndian.PutUint32(out[12:], flags)
	binary.LittleEndian.PutUint16(out[34:], 0x504C)
	return append(out, data...)
}

// TestEmbeddedFontProgram locks what a PresentationML font part yields: an
// sfnt program as it is, the FontData of an EOT file, XOR-decrypted with
// 0x50 when its flags say so (4.4), and refusals for MicroType Express
// compression (4.2), a bad magic number and sizes past the part.
func TestEmbeddedFontProgram(t *testing.T) {
	sfnt := append([]byte{0, 1, 0, 0}, bytes.Repeat([]byte{7}, 20)...)
	if got, err := EmbeddedFontProgram(sfnt); err != nil || !bytes.Equal(got, sfnt) {
		t.Errorf("sfnt: % x, %v; want it as it is", got, err)
	}
	if got, err := EmbeddedFontProgram(eotFile(sfnt, 0)); err != nil || !bytes.Equal(got, sfnt) {
		t.Errorf("EOT: % x, %v; want its font data", got, err)
	}
	xored := bytes.Clone(sfnt)
	for i := range xored {
		xored[i] ^= 0x50
	}
	if got, err := EmbeddedFontProgram(eotFile(xored, 0x10000000)); err != nil || !bytes.Equal(got, sfnt) {
		t.Errorf("XOR EOT: % x, %v; want the decrypted font data", got, err)
	}
	if _, err := EmbeddedFontProgram(eotFile(sfnt, 0x4)); !errors.Is(err, ErrFontCompressed) {
		t.Errorf("compressed EOT: %v; want ErrFontCompressed", err)
	}
	badMagic := eotFile(sfnt, 0)
	badMagic[34] = 0
	long := eotFile(sfnt, 0)
	binary.LittleEndian.PutUint32(long[4:], uint32(len(long)))
	for name, data := range map[string][]byte{"bad magic": badMagic, "font data past the file": long, "short": sfnt[1:8]} {
		if _, err := EmbeddedFontProgram(data); !errors.Is(err, ErrFontContainer) {
			t.Errorf("%s: %v; want ErrFontContainer", name, err)
		}
	}
}
