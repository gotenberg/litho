package signature

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/gotenberg/litho/internal/pkitest"
)

// TestRequestTimestamp asks a local timestamp authority for tokens and
// checks a granted one verifies, and that every answer RFC 3161 makes
// unusable is refused.
func TestRequestTimestamp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pki := pkitest.New(t)
	digest := sha256.Sum256([]byte("a signature value"))

	token, err := RequestTimestamp(ctx, pki.TSAURL(), digest[:])
	if err != nil {
		t.Fatalf("RequestTimestamp: %v", err)
	}
	info, err := VerifyTimestampToken(token, digest[:])
	if err != nil {
		t.Fatalf("VerifyTimestampToken: %v", err)
	}
	if !bytes.Equal(info.Signer.Raw, pki.TSA.Raw) {
		t.Errorf("the token is signed by %q, want the TSA", info.Signer.Subject.CommonName)
	}
	if d := time.Since(info.Time); d < -time.Minute || d > time.Minute {
		t.Errorf("genTime %v is not now", info.Time)
	}
	other := sha256.Sum256([]byte("another value"))
	if _, err := VerifyTimestampToken(token, other[:]); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("verifying against another digest: %v, want ErrSignatureInvalid", err)
	}
	if _, err := verifyTimestampOver(token, []byte("a signature value")); err != nil {
		t.Errorf("verifyTimestampOver the timestamped data: %v", err)
	}

	for _, tc := range []struct {
		name string
		mode pkitest.TSAMode
	}{
		{"a rejection", pkitest.TSAReject},
		{"another nonce", pkitest.TSAWrongNonce},
		{"another imprint", pkitest.TSAWrongImprint},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			t.Parallel()
			pki := pkitest.New(t)
			pki.SetTSAMode(tc.mode)
			if _, err := RequestTimestamp(ctx, pki.TSAURL(), digest[:]); !errors.Is(err, ErrTimestampAuthority) {
				t.Errorf("RequestTimestamp: %v, want ErrTimestampAuthority", err)
			}
		})
	}
	for _, url := range []string{"ldap://tsa.example/", pki.URL() + "/missing"} {
		if _, err := RequestTimestamp(ctx, url, digest[:]); !errors.Is(err, ErrTimestampAuthority) {
			t.Errorf("RequestTimestamp(%s): %v, want ErrTimestampAuthority", url, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := RequestTimestamp(cancelled, pki.TSAURL(), digest[:]); !errors.Is(err, context.Canceled) {
		t.Errorf("RequestTimestamp with a cancelled ctx: %v, want context.Canceled", err)
	}
}

// TestAddSignatureTimestamp timestamps a SignedData's signature value and
// checks the signature still verifies and the verifier finds the token.
func TestAddSignatureTimestamp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pki := pkitest.New(t)
	s, err := NewCMSSigner(pki.Leaf, pki.LeafKey, []*x509.Certificate{pki.Intermediate})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("the signed byte ranges of a PDF")
	digest := sha256.Sum256(content)
	der, err := s.Sign(digest[:], time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	value, err := SignatureValue(der)
	if err != nil {
		t.Fatalf("SignatureValue: %v", err)
	}
	imprint := sha256.Sum256(value)
	token, err := RequestTimestamp(ctx, pki.TSAURL(), imprint[:])
	if err != nil {
		t.Fatal(err)
	}
	stamped, err := AddSignatureTimestamp(der, token)
	if err != nil {
		t.Fatalf("AddSignatureTimestamp: %v", err)
	}
	if _, err := verifyCMSSignature(stamped, content, []*x509.Certificate{pki.Leaf, pki.Intermediate}); err != nil {
		t.Errorf("the timestamped SignedData does not verify: %v", err)
	}
	if ts, err := signatureTimestamp(stamped, content, false); err != nil || ts == nil || !bytes.Equal(ts.Signer.Raw, pki.TSA.Raw) {
		t.Errorf("the verifier finds timestamp %+v (%v), want one from the TSA that verifies", ts, err)
	}
	after, err := SignatureValue(stamped)
	if err != nil || !bytes.Equal(after, value) {
		t.Errorf("the signature value changed: %v", err)
	}
	// A second token joins the first in the same unsigned attributes.
	twice, err := AddSignatureTimestamp(stamped, token)
	if err != nil {
		t.Fatalf("a second AddSignatureTimestamp: %v", err)
	}
	if _, err := verifyCMSSignature(twice, content, []*x509.Certificate{pki.Leaf, pki.Intermediate}); err != nil {
		t.Errorf("the twice timestamped SignedData does not verify: %v", err)
	}
	if _, err := AddSignatureTimestamp(der[:len(der)-1], token); !errors.Is(err, ErrSignatureMalformed) {
		t.Errorf("a truncated SignedData: %v, want ErrSignatureMalformed", err)
	}
}
