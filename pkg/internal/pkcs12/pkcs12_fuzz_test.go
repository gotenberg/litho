package pkcs12

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzDecode feeds arbitrary bytes to [Decode] and asserts it never panics.
func FuzzDecode(f *testing.F) {
	for _, name := range []string{"aes256.p12", "legacy.p12"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data, "litho-test")
		f.Add(data[:len(data)/2], "litho-test")
	}
	f.Fuzz(func(_ *testing.T, data []byte, password string) {
		c, err := Decode(data, password)
		if err != nil && (len(c.Keys) != 0 || len(c.Certificates) != 0) {
			panic("Decode returned contents with an error")
		}
	})
}

// FuzzToDER feeds arbitrary bytes to [toDER] and asserts it never panics and
// that its output is a fixed point.
func FuzzToDER(f *testing.F) {
	data, err := os.ReadFile(filepath.Join("testdata", "aes256.p12"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add(data[:len(data)/2])
	f.Add([]byte{0x30, 0x80, 0x04, 0x01, 0x00, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		der, err := toDER(data)
		if err != nil {
			return
		}
		again, err := toDER(der)
		if err != nil || string(again) != string(der) {
			t.Fatalf("toDER is not idempotent: %v", err)
		}
	})
}

// FuzzRC2 feeds arbitrary keys and blocks to the RC2 decrypter and asserts it
// never panics.
func FuzzRC2(f *testing.F) {
	f.Add([]byte{0x88, 0xbc, 0xa9, 0x0e, 0x90, 0x87, 0x5a}, 64, []byte{0x6c, 0xcf, 0x43, 0x08, 0x97, 0x4c, 0x26, 0x7f})
	f.Add([]byte{0x88}, 40, []byte{0x61, 0xa8})
	f.Fuzz(func(_ *testing.T, key []byte, bits int, block []byte) {
		c, err := newRC2Decrypter(key, bits)
		if err != nil || len(block) < rc2BlockSize {
			return
		}
		out := make([]byte, rc2BlockSize)
		c.Decrypt(out, block[:rc2BlockSize])
	})
}
