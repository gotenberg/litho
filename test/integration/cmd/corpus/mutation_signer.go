package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

// corpusSignerCN is the common name of the certificate [corpusSigner] signs
// with, which the mutate-sign feature looks for among the verified signatures.
const corpusSignerCN = "litho corpus signer"

// corpusSigner is the built-in [pdf.Signer] of [pdf.NewSigner] over a
// throwaway ECDSA P-256 key and a self-signed certificate, so mutate-sign
// measures the signer litho ships. Its CMS SignedData holds one signer and its
// certificate, what ISO 19005-2, 6.4.3 tests 2 and 3 ask of a signature, so
// signing changes no PDF/A rule set.
//
// The key is generated per run and never leaves the process. A caller that
// verifies a signature it made trusts [corpusSigner.roots] as the root.
// Safe for concurrent use: the built-in signer is, and cert is set once by
// newCorpusSigner.
type corpusSigner struct {
	pdf.Signer
	cert *x509.Certificate
}

// newCorpusSigner generates the key and the self-signed certificate, valid
// from an hour ago for ten years, so a verification at any time during a run
// sees a current certificate.
func newCorpusSigner() (*corpusSigner, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating the signing key: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: corpusSignerCN},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("creating the signing certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parsing the signing certificate: %w", err)
	}
	signer, err := pdf.NewSigner(cert, key)
	if err != nil {
		return nil, fmt.Errorf("building the signer: %w", err)
	}
	return &corpusSigner{Signer: signer, cert: cert}, nil
}

// roots returns a pool trusting the signing certificate, so litho's verifier
// can validate the chain of a signature this signer made.
func (s *corpusSigner) roots() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(s.cert)
	return pool
}
