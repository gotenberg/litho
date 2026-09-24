package compliance

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// TestParseCMSSignatureBER pins that a SignedData written in BER with
// indefinite lengths, as openssl cms -sign -stream writes it, reads like its
// DER form: one certificate, one signer, so ISO 19005-2, 6.4.3 t2 and t3 both
// pass, as veraPDF finds. Zero padding after the value, which a signature
// /Contents carries, is ignored.
//
// # Corpus evidence
//
//   - pdfjs issue16553.pdf: veraPDF finds one certificate and one signer in
//     its BER SignedData.
func TestParseCMSSignatureBER(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, b64 string }{
		{"BER with indefinite lengths", cmsSignedBERBase64},
		{"DER", cmsSignedDERBase64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			blob, err := base64.StdEncoding.DecodeString(tc.b64)
			if err != nil {
				t.Fatalf("decode fixture: %v", err)
			}
			padded := append(bytes.Clone(blob), make([]byte, 64)...)
			info, err := parseCMSSignature(padded)
			if err != nil {
				t.Fatalf("parseCMSSignature: %v", err)
			}
			if !info.HasCertificate || info.SignerCount != 1 {
				t.Errorf("got %+v, want a certificate and one signer", info)
			}
		})
	}
}

// TestBERToDER pins the length rewriting: an indefinite length becomes the
// minimal definite one, a DER value comes back unchanged, and a truncated or
// primitive indefinite value is refused.
func TestBERToDER(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		in, out []byte
		wantErr bool
	}{
		{"indefinite sequence", []byte{0x30, 0x80, 0x02, 0x01, 0x05, 0x00, 0x00}, []byte{0x30, 0x03, 0x02, 0x01, 0x05}, false},
		{"nested indefinite", []byte{0x30, 0x80, 0x31, 0x80, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00}, []byte{0x30, 0x04, 0x31, 0x02, 0x05, 0x00}, false},
		{"non-minimal length", []byte{0x04, 0x81, 0x01, 0xAA}, []byte{0x04, 0x01, 0xAA}, false},
		{"DER unchanged", []byte{0x30, 0x03, 0x02, 0x01, 0x05}, []byte{0x30, 0x03, 0x02, 0x01, 0x05}, false},
		{"trailing padding ignored", []byte{0x02, 0x01, 0x05, 0x00, 0x00}, []byte{0x02, 0x01, 0x05}, false},
		{"truncated", []byte{0x30, 0x80, 0x02, 0x01}, nil, true},
		{"primitive indefinite", []byte{0x04, 0x80, 0x00, 0x00}, nil, true},
		{"length past input", []byte{0x04, 0x05, 0xAA}, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := berToDER(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("berToDER error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !bytes.Equal(got, tt.out) {
				t.Errorf("berToDER = % x, want % x", got, tt.out)
			}
		})
	}
}
