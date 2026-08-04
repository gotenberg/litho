package compliance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// readCMSBlob loads one of the CMS SignedData blobs in testdata.
//
// They are genuine openssl output rather than hand-built DER, because they are
// what veraPDF 1.30.2 was measured against:
//
//	openssl cms -sign -binary -in data -signer c1.pem -inkey k1.pem \
//	    -outform DER -nodetach -md sha256 -out cms_real.der
//
// with -nocerts for cms_nocert.der and a second -signer/-inkey pair for
// cms_two.der.
func readCMSBlob(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// TestParseCMSSignature covers what ISO 19005-2/3, 6.4.3 tests 2 and 3 ask of a
// signature's /Contents: whether a signing certificate rides along, and whether
// there is exactly one signer.
//
// The expectations are veraPDF 1.30.2's, measured on these same blobs embedded
// in a PDF/A-2b signature field: one signer with its certificate answers to
// neither test, -nocerts answers to t2 alone, two signers answer to t3 alone,
// and an unparseable blob answers to both.
func TestParseCMSSignature(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		blob     []byte
		wantCert bool
		wantN    int
		wantErr  bool
	}{
		{
			name:     "a real signature with its certificate",
			blob:     readCMSBlob(t, "cms_real.der"),
			wantCert: true,
			wantN:    1,
		},
		{
			name:     "a real signature signed with -nocerts",
			blob:     readCMSBlob(t, "cms_nocert.der"),
			wantCert: false,
			wantN:    1,
		},
		{
			name:     "a real signature with two signers",
			blob:     readCMSBlob(t, "cms_two.der"),
			wantCert: true,
			wantN:    2,
		},
		{name: "an empty blob", blob: nil, wantErr: true},
		{name: "a junk blob", blob: []byte{0x30, 0x80, 0x00, 0x00}, wantErr: true},
		{name: "text rather than DER", blob: []byte("not a signature at all"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			info, err := parseCMSSignature(tc.blob)
			if tc.wantErr {
				if !errors.Is(err, errCMSUnparseable) {
					t.Errorf("got err %v, want errCMSUnparseable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.HasCertificate != tc.wantCert {
				t.Errorf("HasCertificate = %v, want %v", info.HasCertificate, tc.wantCert)
			}
			if info.SignerCount != tc.wantN {
				t.Errorf("SignerCount = %d, want %d", info.SignerCount, tc.wantN)
			}
		})
	}
}

// TestParseCMSSignature_TruncatedRealBlob covers a signature cut short: the
// reader must report it rather than accept a partial structure.
func TestParseCMSSignature_TruncatedRealBlob(t *testing.T) {
	t.Parallel()

	full := readCMSBlob(t, "cms_real.der")
	for _, frac := range []int{2, 4, 8} {
		if _, err := parseCMSSignature(full[:len(full)/frac]); !errors.Is(err, errCMSUnparseable) {
			t.Errorf("blob cut to 1/%d must be refused, got %v", frac, err)
		}
	}
}

// TestParseCMSSignature_OversizeBlobRejected covers the anti-DoS cap: a blob
// past maxCMSBlobSize is refused without being handed to the decoder.
func TestParseCMSSignature_OversizeBlobRejected(t *testing.T) {
	t.Parallel()

	if _, err := parseCMSSignature(make([]byte, maxCMSBlobSize+1)); !errors.Is(err, errCMSUnparseable) {
		t.Errorf("an oversize blob must be refused, got %v", err)
	}
}
