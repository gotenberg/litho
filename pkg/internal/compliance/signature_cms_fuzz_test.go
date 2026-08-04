package compliance

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseCMSSignature drives the CMS reader with arbitrary bytes. A
// signature's /Contents is attacker-controlled, so the reader must report a
// malformed blob rather than panic or read past its input.
func FuzzParseCMSSignature(f *testing.F) {
	valid, err := os.ReadFile(filepath.Join("testdata", "cms_real.der"))
	if err != nil {
		f.Fatalf("read cms_real.der: %v", err)
	}

	f.Add(valid)
	f.Add(valid[:len(valid)/2]) // truncated
	f.Add([]byte{})
	f.Add([]byte{0x30, 0x80, 0x00, 0x00}) // indefinite length, which DER forbids

	f.Fuzz(func(t *testing.T, data []byte) {
		info, err := parseCMSSignature(data)
		if err != nil {
			return
		}
		// A successful parse must report a sane signer count.
		if info.SignerCount < 0 {
			t.Fatalf("negative signer count %d", info.SignerCount)
		}
	})
}
