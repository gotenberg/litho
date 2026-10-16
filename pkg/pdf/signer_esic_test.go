package pdf

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/gotenberg/litho/internal/pkitest"
)

// TestSignatureESICLevel checks the ESIC developer extension a signature
// declares (ETSI EN 319 142-1 V1.2.1, 5.6): level 2 for an
// ETSI.CAdES.detached signature of a document older than PDF 2.0, none for an
// adbe.pkcs7.detached one, and none on a PDF 2.0 document, whose own
// specification (ISO 32000-2, 12.8.3.4) defines the subfilter. An ESIC entry
// the document declares already gives way to a higher level, and is kept at
// the same or a higher one, or when it is no dictionary.
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
	// declared returns a document whose catalog's extensions dictionary holds
	// the ESIC entry esic.
	declared := func(esic string) []byte {
		return buildPDF([]string{
			"<< /Type /Catalog /Pages 2 0 R /Extensions 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /ESIC " + esic + " >>",
		})
	}
	level := func(n string) string {
		return "<< /Type /DeveloperExtensions /BaseVersion /1.7 /ExtensionLevel " + n + " >>"
	}

	for _, tc := range []struct {
		name string
		src  []byte
		opts []SignOption
		want int
	}{
		{"PAdES on PDF 1.4", writerTestPDF(), []SignOption{WithPAdES()}, 2},
		{"adbe.pkcs7.detached on PDF 1.4", writerTestPDF(), nil, 0},
		{"PAdES on PDF 2.0", pdf20, []SignOption{WithPAdES()}, 0},
		// A security store update of litho or a pyHanko signature leaves
		// level 1.
		{"PAdES over a declared level 1", declared(level("1")), []SignOption{WithPAdES()}, 2},
		{"PAdES over a declared level 2", declared(level("2")), []SignOption{WithPAdES()}, 2},
		// iText declares level 5 for its security store.
		{"PAdES over a declared level 5", declared(level("5")), []SignOption{WithPAdES()}, 5},
		{"adbe.pkcs7.detached over a declared level 1", declared(level("1")), nil, 1},
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

	t.Run("PAdES over the ISO 32000-2:2020 array", func(t *testing.T) {
		t.Parallel()
		src := declared("[" + level("1") + "]")
		out := signWith(t, src, signer, WithPAdES(), WithSignatureField("S"))
		verifyOne(t, out, pki, 1)
		if bytes.Contains(out[len(src):], []byte("/ESIC")) {
			t.Error("the update replaced the ESIC array the document declares")
		}
	})
}

// TestESICLevelBothExtensions gives a document older than PDF 2.0 both
// extensions ETSI EN 319 142-1 V1.2.1, 5.6 names, in both orders: a PAdES
// signature (level 2) and a document timestamp (level 1). Its catalog ends
// declaring level 2, the higher, either way.
func TestESICLevelBothExtensions(t *testing.T) {
	t.Parallel()
	pki := pkitest.New(t)
	signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
	if err != nil {
		t.Fatal(err)
	}

	signed := timestampWith(t, writerTestPDF(), func(w *Writer) {
		w.Sign(signer, WithPAdES(), WithSignatureField("Approval"))
		w.Timestamp(pki.TSAURL(), WithTimestampField("Archive"))
	})
	if got := esicLevel(t, signed); got != 2 {
		t.Errorf("signed, then timestamped: ESIC level %d, want 2", got)
	}

	stamped := timestampWith(t, writerTestPDF(), func(w *Writer) { w.Timestamp(pki.TSAURL(), WithTimestampField("Archive")) })
	if got := esicLevel(t, stamped); got != 1 {
		t.Fatalf("timestamped: ESIC level %d, want 1", got)
	}
	out := timestampWith(t, stamped, func(w *Writer) { w.Sign(signer, WithPAdES(), WithSignatureField("Approval")) })
	if got := esicLevel(t, out); got != 2 {
		t.Errorf("timestamped, then signed: ESIC level %d, want 2", got)
	}
	for _, v := range verifyWith(t, out, pki) {
		if !v.HashValid || !v.CertChainValid {
			t.Errorf("%s: hash %v, chain %v: %s", v.FieldName, v.HashValid, v.CertChainValid, v.ErrorMessage)
		}
	}
}
