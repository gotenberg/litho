package crypto

import (
	"bytes"
	"crypto/aes"
	"testing"
)

// xmpFixture is the opening of a real XMP packet, the shape a producer stores in
// plaintext inside an encrypted file.
const xmpFixture = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?><x:xmpmeta xmlns:x="adobe:ns:meta/"/>`

// TestAESDecryptPassesThroughWhatCannotBeCiphertext pins the leniency [aesDecrypt]
// documents: bytes that ISO 32000-1, 7.6.2 could never have produced come back
// exactly as stored, with no error.
//
// pdfjs bug1826783.pdf is the file that needs it. It declares /EncryptMetadata
// true with AESV2 and then stores its /Metadata packet as plaintext, 1544 bytes,
// which leaves 8 bytes past the initialisation vector.
func TestAESDecryptPassesThroughWhatCannotBeCiphertext(t *testing.T) {
	t.Parallel()

	key := make([]byte, 16)

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{
			name: "a partial block after the IV",
			data: append(make([]byte, aes.BlockSize), xmpFixture...),
		},
		{
			name: "fewer bytes than one block",
			data: []byte("<?xpacket"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want := bytes.Clone(tc.data)
			got, err := aesDecrypt(key, tc.data)
			if err != nil {
				t.Fatalf("aesDecrypt returned an error for bytes that are not ciphertext: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("aesDecrypt rewrote the stored bytes:\n got %q\nwant %q", got, want)
			}
		})
	}
}

// TestAESDecryptRoundTripsRealCiphertext is the control: bytes the algorithm did
// produce are decrypted, so the passthrough above cannot be masking a decryption
// that simply never runs.
func TestAESDecryptRoundTripsRealCiphertext(t *testing.T) {
	t.Parallel()

	key := []byte("0123456789abcdef")
	plaintext := []byte(xmpFixture)

	encrypted, err := aesEncrypt(key, plaintext)
	if err != nil {
		t.Fatalf("aesEncrypt: %v", err)
	}
	if (len(encrypted)-aes.BlockSize)%aes.BlockSize != 0 {
		t.Fatalf("fixture is not aligned: %d bytes past the IV", len(encrypted)-aes.BlockSize)
	}

	got, err := aesDecrypt(key, encrypted)
	if err != nil {
		t.Fatalf("aesDecrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round trip = %q, want %q", got, plaintext)
	}
}
