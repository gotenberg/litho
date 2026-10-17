package crypto

import (
	"bytes"
	"crypto/md5" //nolint:gosec // MD5 is the PDF key derivation under test.
	"crypto/rc4" //nolint:gosec // RC4 is the PDF cipher under test.
	"testing"
)

// TestObjKeyExtendsShortV4Key pins the object key a /V 4 handler derives from
// a file key shorter than 16 bytes: the key zero-extended to 16 bytes goes into
// Algorithm 1, as Acrobat writes it and pdf.js reads it. With the 5-byte key
// Algorithm 1 states, the object streams of the pdfjs files issue19484_1 and
// issue19484_2 decrypt to bytes that are not zlib data, and the page tree is
// lost.
func TestObjKeyExtendsShortV4Key(t *testing.T) {
	t.Parallel()

	fileKey := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	extended := make([]byte, 16)
	copy(extended, fileKey)

	md := md5.New() //nolint:gosec // MD5 is the PDF key derivation under test.
	md.Write(extended)
	md.Write([]byte{12, 0, 0, 0, 0})
	want := md.Sum(nil)

	h := &Handler{V: 4, CFM: "V2", Length: 40, EncryptionKey: fileKey}
	if got := h.ObjKey(12, 0); !bytes.Equal(got, want) {
		t.Fatalf("ObjKey = %x, want %x (the 16-byte zero-extended key)", got, want)
	}

	plain := []byte("x\x9c stand-in for an object stream")
	c, err := rc4.NewCipher(want) //nolint:gosec // RC4 is the PDF cipher under test.
	if err != nil {
		t.Fatal(err)
	}
	cipherText := make([]byte, len(plain))
	c.XORKeyStream(cipherText, plain)
	got, err := h.DecryptFunc(12, 0)(cipherText)
	if err != nil {
		t.Fatalf("DecryptFunc: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("DecryptFunc = %q, want %q", got, plain)
	}

	// Below /V 4, Algorithm 1 is applied as written: a 40-bit RC4 key gives a
	// 10-byte object key.
	legacy := &Handler{V: 1, CFM: "V2", Length: 40, EncryptionKey: fileKey}
	if got := legacy.ObjKey(12, 0); len(got) != len(fileKey)+5 {
		t.Errorf("V 1 ObjKey length = %d, want %d", len(got), len(fileKey)+5)
	}
}
