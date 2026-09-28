package pdf

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/asn1"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// wrappedSigner hides the signer of NewSigner behind the Signer interface, so
// the Writer treats it as a caller's own implementation.
type wrappedSigner struct{ Signer }

// oidSigningTime is the CMS signing-time attribute (RFC 5652, 11.3).
var oidSigningTime = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}

// TestWithPAdES signs a PAdES baseline B-B signature and checks its
// dictionary, its SignedData and that the verifier accepts it; a caller's
// Signer that writes a signing-time is refused.
func TestWithPAdES(t *testing.T) {
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

	out := signWith(t, writerTestPDF(), signer, WithPAdES(), WithSignatureField("PAdES"))
	verifyOne(t, out, pki, 1)
	sig := signatureDictOf(t, out, "PAdES")
	if got := sig["SubFilter"]; got != ast.PdfName("ETSI.CAdES.detached") {
		t.Errorf("/SubFilter = %v, want ETSI.CAdES.detached", got)
	}
	if _, ok := sig["M"]; !ok {
		t.Error("a PAdES signature dictionary has no /M, which carries its signing time")
	}
	contents, _ := sig["Contents"].(ast.PdfString)
	if bytes.Contains([]byte(contents), mustMarshalOID(t, oidSigningTime)) {
		t.Error("the PAdES SignedData carries a signing-time attribute")
	}

	plain := signWith(t, writerTestPDF(), signer, WithSignatureField("Plain"))
	plainContents, _ := signatureDictOf(t, plain, "Plain")["Contents"].(ast.PdfString)
	if !bytes.Contains([]byte(plainContents), mustMarshalOID(t, oidSigningTime)) {
		t.Error("the adbe.pkcs7.detached SignedData lost its signing-time attribute, so the probe above proves nothing")
	}

	ctx := context.Background()
	src := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	w.Sign(wrappedSigner{signer}, WithPAdES())
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); !errors.Is(err, ErrSignatureNotPAdES) {
		t.Errorf("a Signer writing a signing-time under WithPAdES: %v, want ErrSignatureNotPAdES", err)
	}
	if buf.Len() != 0 {
		t.Errorf("a refused signature wrote %d bytes", buf.Len())
	}
}

// mustMarshalOID returns the DER of oid.
func mustMarshalOID(t *testing.T, oid asn1.ObjectIdentifier) []byte {
	t.Helper()
	der, err := asn1.Marshal(oid)
	if err != nil {
		t.Fatal(err)
	}
	return der
}
