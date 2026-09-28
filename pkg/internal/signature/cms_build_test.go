package signature

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"math/big"
	"testing"
	"time"
)

// buildTestCert returns a certificate for key's public half, issued by parent
// with parentKey, or self-signed when parent is nil.
func buildTestCert(t *testing.T, cn string, key crypto.Signer, usage x509.KeyUsage, parent *x509.Certificate, parentKey crypto.Signer) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:              usage,
		BasicConstraintsValid: parent == nil,
		IsCA:                  parent == nil,
	}
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, key.Public(), parentKey)
	if err != nil {
		t.Fatalf("creating the certificate of %s: %v", cn, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing the certificate of %s: %v", cn, err)
	}
	return cert
}

// TestCMSSignerSignsWhatTheVerifierAccepts signs with each key type litho
// supports and checks the SignedData verifies, carries the four signed
// attributes and the chain, and fits in the size it announced.
func TestCMSSignerSignsWhatTheVerifierAccepts(t *testing.T) {
	t.Parallel()

	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := buildTestCert(t, "root", rootKey, x509.KeyUsageCertSign, nil, nil)

	for _, tc := range []struct {
		name string
		key  func() (crypto.Signer, error)
	}{
		{"RSA-2048", func() (crypto.Signer, error) { return rsa.GenerateKey(rand.Reader, 2048) }},
		{"RSA-3072", func() (crypto.Signer, error) { return rsa.GenerateKey(rand.Reader, 3072) }},
		{"ECDSA-P256", func() (crypto.Signer, error) { return ecdsa.GenerateKey(elliptic.P256(), rand.Reader) }},
		{"ECDSA-P384", func() (crypto.Signer, error) { return ecdsa.GenerateKey(elliptic.P384(), rand.Reader) }},
		{"ECDSA-P521", func() (crypto.Signer, error) { return ecdsa.GenerateKey(elliptic.P521(), rand.Reader) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			key, err := tc.key()
			if err != nil {
				t.Fatal(err)
			}
			leaf := buildTestCert(t, "signer "+tc.name, key, x509.KeyUsageDigitalSignature, root, rootKey)
			s, err := NewCMSSigner(leaf, key, []*x509.Certificate{root, leaf, root})
			if err != nil {
				t.Fatalf("NewCMSSigner: %v", err)
			}

			content := []byte("the signed byte ranges of a PDF")
			digest := sha256.Sum256(content)
			at := time.Date(2026, 9, 28, 10, 11, 12, 0, time.FixedZone("CEST", 2*3600))
			der, err := s.Sign(digest[:], at, false)
			if err != nil {
				t.Fatalf("Sign: %v", err)
			}
			if len(der) > s.EstimatedSize() {
				t.Errorf("the SignedData holds %d bytes, over the %d EstimatedSize announced", len(der), s.EstimatedSize())
			}

			certs, err := parsePKCS7Certificates(der)
			if err != nil {
				t.Fatalf("parsing the certificates: %v", err)
			}
			if len(certs) != 2 || !bytes.Equal(certs[0].Raw, leaf.Raw) || !bytes.Equal(certs[1].Raw, root.Raw) {
				t.Errorf("certificates = %d, want the signer then its root, once each", len(certs))
			}
			signer, err := verifyCMSSignature(der, content, certs)
			if err != nil {
				t.Fatalf("verifying: %v", err)
			}
			if !bytes.Equal(signer.Raw, leaf.Raw) {
				t.Errorf("the verifier matched %q, want the signer", signer.Subject.CommonName)
			}
			if _, err := verifyCMSSignature(der, append(content, '!'), certs); !errors.Is(err, ErrSignatureInvalid) {
				t.Errorf("verifying other content: %v, want ErrSignatureInvalid", err)
			}

			attrs := signerAttributes(t, der, 4)
			var gotTime time.Time
			if _, err := asn1.Unmarshal(attrs[oidCMSAttrSigningTime.String()], &gotTime); err != nil || !gotTime.Equal(at) {
				t.Errorf("signing-time = %v (%v), want %v", gotTime, err, at)
			}
			var ct asn1.ObjectIdentifier
			if _, err := asn1.Unmarshal(attrs[oidCMSAttrContentType.String()], &ct); err != nil || !ct.Equal(oidCMSData) {
				t.Errorf("content-type = %v (%v), want id-data", ct, err)
			}
			var essCert struct {
				Certs []struct {
					Hash   []byte
					Serial asn1.RawValue
				}
			}
			certHash := sha256.Sum256(leaf.Raw)
			if _, err := asn1.Unmarshal(attrs[oidCMSAttrSigningCert.String()], &essCert); err != nil ||
				len(essCert.Certs) != 1 || !bytes.Equal(essCert.Certs[0].Hash, certHash[:]) {
				t.Errorf("signing-certificate-v2 = %+v (%v), want the signer's SHA-256", essCert, err)
			}
		})
	}
}

// signerAttributes returns the value of each signed attribute of the first
// SignerInfo of der, by attribute OID.
func signerAttributes(t *testing.T, der []byte, want int) map[string][]byte {
	t.Helper()
	var ci contentInfo
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		t.Fatal(err)
	}
	var sd signedDataFull
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		t.Fatal(err)
	}
	out := make(map[string][]byte)
	rest := sd.SignerInfos[0].SignedAttrs.Bytes
	for len(rest) > 0 {
		var a attribute
		var err error
		if rest, err = asn1.Unmarshal(rest, &a); err != nil {
			t.Fatal(err)
		}
		out[a.Type.String()] = a.Values.Bytes
	}
	if len(out) != want {
		t.Errorf("%d signed attributes, want %d", len(out), want)
	}
	return out
}

// TestNewCMSSignerRefuses checks each key and certificate NewCMSSigner cannot
// sign with is refused with its sentinel.
func TestNewCMSSignerRefuses(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p224, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := buildTestCert(t, "signer", key, x509.KeyUsageDigitalSignature, nil, nil)

	for _, tc := range []struct {
		name string
		cert *x509.Certificate
		key  crypto.Signer
		want error
	}{
		{"no certificate", nil, key, ErrSignerKeyMismatch},
		{"no key", cert, nil, ErrSignerKeyMismatch},
		{"another key", cert, other, ErrSignerKeyMismatch},
		{"Ed25519", buildTestCert(t, "ed", edKey, x509.KeyUsageDigitalSignature, nil, nil), edKey, ErrSignerKeyUnsupported},
		{"P-224", buildTestCert(t, "p224", p224, x509.KeyUsageDigitalSignature, nil, nil), p224, ErrSignerKeyUnsupported},
		{"encipherment only", buildTestCert(t, "enc", key, x509.KeyUsageKeyAgreement, nil, nil), key, ErrSignerCertificateUsage},
	} {
		if _, err := NewCMSSigner(tc.cert, tc.key, nil); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := NewCMSSigner(buildTestCert(t, "nonrep", key, x509.KeyUsageContentCommitment, nil, nil), key, nil); err != nil {
		t.Errorf("a nonRepudiation certificate: %v, want accepted", err)
	}
}
