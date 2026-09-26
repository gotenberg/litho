package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"time"
)

// OIDs of the CMS SignedData [corpusSigner] builds (RFC 5652, RFC 5754, RFC
// 5758).
//
//nolint:gochecknoglobals // static OID table
var (
	oidCMSSignedData      = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidCMSData            = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidCMSSHA256          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidCMSECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
)

// cmsAlgorithm is an AlgorithmIdentifier with no parameters.
type cmsAlgorithm struct {
	Algorithm asn1.ObjectIdentifier
}

// cmsIssuerAndSerial identifies the signing certificate (RFC 5652, 10.2.4).
type cmsIssuerAndSerial struct {
	Issuer       asn1.RawValue
	SerialNumber *big.Int
}

// cmsSignerInfo is a SignerInfo with no signed attributes (RFC 5652, 5.3).
type cmsSignerInfo struct {
	Version            int
	SID                cmsIssuerAndSerial
	DigestAlgorithm    cmsAlgorithm
	SignatureAlgorithm cmsAlgorithm
	Signature          []byte
}

// cmsEncapContent is a detached EncapsulatedContentInfo: its type and no
// content.
type cmsEncapContent struct {
	ContentType asn1.ObjectIdentifier
}

// cmsSignedData is the SignedData [corpusSigner] writes (RFC 5652, 5.1).
// Certificates is written as given, so it carries its own [0] tag.
type cmsSignedData struct {
	Version          int
	DigestAlgorithms []cmsAlgorithm `asn1:"set"`
	EncapContentInfo cmsEncapContent
	Certificates     asn1.RawValue
	SignerInfos      []cmsSignerInfo `asn1:"set"`
}

// cmsContentInfo wraps the SignedData (RFC 5652, 3). Content is written as
// given, so it carries its own [0] EXPLICIT tag.
type cmsContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue
}

// corpusSignerCN is the common name of the certificate [corpusSigner] signs
// with, which the mutate-sign feature looks for among the verified signatures.
const corpusSignerCN = "litho corpus signer"

// corpusSignerEstimatedSize is the /Contents placeholder a signature gets. The
// SignedData holds one P-256 certificate and one ECDSA signature, well under a
// kilobyte, so four kilobytes leave room without bloating the output.
const corpusSignerEstimatedSize = 4096

// corpusSigner implements [pdf.Signer] with a throwaway ECDSA P-256 key and a
// self-signed certificate. It wraps the signature in a CMS SignedData holding
// one signer and its certificate, what ISO 19005-2, 6.4.3 tests 2 and 3 ask of
// a signature, so signing changes no PDF/A rule set.
//
// The key is generated per run and never leaves the process. A caller that
// verifies a signature it made trusts [corpusSigner.Certificate] as a root.
// Safe for concurrent use: Sign reads the key and the certificate, which
// newCorpusSigner sets once.
type corpusSigner struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
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
	return &corpusSigner{cert: cert, key: key}, nil
}

// Sign signs digest, the SHA-256 of the signed byte ranges, and returns the
// DER-encoded CMS ContentInfo.
func (s *corpusSigner) Sign(digest []byte) ([]byte, error) {
	sig, err := ecdsa.SignASN1(rand.Reader, s.key, digest)
	if err != nil {
		return nil, fmt.Errorf("signing the digest: %w", err)
	}
	signed, err := asn1.Marshal(cmsSignedData{
		Version:          1,
		DigestAlgorithms: []cmsAlgorithm{{Algorithm: oidCMSSHA256}},
		EncapContentInfo: cmsEncapContent{ContentType: oidCMSData},
		Certificates: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: s.cert.Raw,
		},
		SignerInfos: []cmsSignerInfo{{
			Version:            1,
			SID:                cmsIssuerAndSerial{Issuer: asn1.RawValue{FullBytes: s.cert.RawIssuer}, SerialNumber: s.cert.SerialNumber},
			DigestAlgorithm:    cmsAlgorithm{Algorithm: oidCMSSHA256},
			SignatureAlgorithm: cmsAlgorithm{Algorithm: oidCMSECDSAWithSHA256},
			Signature:          sig,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("encoding the SignedData: %w", err)
	}
	out, err := asn1.Marshal(cmsContentInfo{
		ContentType: oidCMSSignedData,
		Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: signed},
	})
	if err != nil {
		return nil, fmt.Errorf("encoding the ContentInfo: %w", err)
	}
	return out, nil
}

// Certificate returns the self-signed signing certificate.
func (s *corpusSigner) Certificate() *x509.Certificate { return s.cert }

// EstimatedSize returns the /Contents placeholder size.
func (s *corpusSigner) EstimatedSize() int { return corpusSignerEstimatedSize }

// roots returns a pool trusting the signing certificate, so litho's verifier
// can validate the chain of a signature this signer made.
func (s *corpusSigner) roots() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(s.cert)
	return pool
}
