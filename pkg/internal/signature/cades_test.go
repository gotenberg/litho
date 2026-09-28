package signature

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"testing"
	"time"
)

// TestCMSSignerCAdES checks the CAdES form drops the signing-time attribute
// ETSI EN 319 142-1, 5.2.1 forbids and still verifies, and that CheckCAdES
// tells the two forms apart.
func TestCMSSignerCAdES(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := buildTestCert(t, "cades signer", key, x509.KeyUsageDigitalSignature, nil, nil)
	s, err := NewCMSSigner(leaf, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("the signed byte ranges of a PDF")
	digest := sha256.Sum256(content)
	at := time.Date(2026, 9, 28, 10, 11, 12, 0, time.UTC)

	cades, err := s.Sign(digest[:], at, true)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	attrs := signerAttributes(t, cades, 3)
	if _, ok := attrs[oidCMSAttrSigningTime.String()]; ok {
		t.Error("the CAdES SignedData carries a signing-time attribute")
	}
	if _, err := verifyCMSSignature(cades, content, []*x509.Certificate{leaf}); err != nil {
		t.Errorf("verifying the CAdES SignedData: %v", err)
	}
	if err := CheckCAdES(cades); err != nil {
		t.Errorf("CheckCAdES(CAdES form): %v", err)
	}

	pkcs7, err := s.Sign(digest[:], at, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckCAdES(pkcs7); !errors.Is(err, ErrNotCAdES) {
		t.Errorf("CheckCAdES(adbe.pkcs7.detached form): %v, want ErrNotCAdES", err)
	}
	if err := CheckCAdES([]byte{0x30, 0x03, 0x02, 0x01}); !errors.Is(err, ErrSignatureMalformed) {
		t.Errorf("CheckCAdES(truncated): %v, want ErrSignatureMalformed", err)
	}
}
