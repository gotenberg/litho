package pdf

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"strings"
	"testing"
)

// TestSignatureESICLevel checks the ESIC developer extension a signature
// declares (ETSI EN 319 142-1 V1.2.1, 5.6): level 2 for an
// ETSI.CAdES.detached signature of a document older than PDF 2.0, none for an
// adbe.pkcs7.detached one, and none on a PDF 2.0 document, whose own
// specification (ISO 32000-2, 12.8.3.4) defines the subfilter. An ESIC entry
// the document declares already is kept.
func TestSignatureESICLevel(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pki := newTestPKI(t, key)
	signer, err := NewSigner(pki.leaf, key, pki.root)
	if err != nil {
		t.Fatal(err)
	}
	pdf20 := []byte(strings.Replace(string(writerTestPDF()), "%PDF-1.4", "%PDF-2.0", 1))
	// A document whose catalog declares ESIC level 1 already, as a security
	// store update of litho or a pyHanko signature leaves it.
	declared := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Extensions 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /ESIC << /Type /DeveloperExtensions /BaseVersion /1.7 /ExtensionLevel 1 >> >>",
	})

	for _, tc := range []struct {
		name string
		src  []byte
		opts []SignOption
		want int
	}{
		{"PAdES on PDF 1.4", writerTestPDF(), []SignOption{WithPAdES()}, 2},
		{"adbe.pkcs7.detached on PDF 1.4", writerTestPDF(), nil, 0},
		{"PAdES on PDF 2.0", pdf20, []SignOption{WithPAdES()}, 0},
		{"PAdES over a declared level 1", declared, []SignOption{WithPAdES()}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := signWith(t, tc.src, signer, append([]SignOption{WithSignatureField("S")}, tc.opts...)...)
			verifyOne(t, out, pki, 1)
			if got := esicLevel(t, out); got != tc.want {
				t.Errorf("ESIC level %d, want %d", got, tc.want)
			}
		})
	}
}
