package signature

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"testing"
	"time"
)

func TestCheckRevocation_OCSPGood(t *testing.T) {
	cert := &x509.Certificate{SerialNumber: big.NewInt(42)}
	sigTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	ocsps := []ocspBasicResponse{{
		ProducedAt: time.Date(2023, 6, 15, 10, 0, 0, 0, time.UTC),
		Responses: []singleResponse{{
			SerialNumber: big.NewInt(42),
			Status:       0, // good
			ThisUpdate:   time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC),
			NextUpdate:   time.Date(2023, 6, 16, 0, 0, 0, 0, time.UTC),
		}},
	}}

	ok, info := checkRevocation(cert, sigTime, ocsps, nil)
	if !ok {
		t.Errorf("expected ok=true, got false: %s", info)
	}
	if info == "" {
		t.Error("expected non-empty info")
	}
}

func TestCheckRevocation_OCSPRevoked(t *testing.T) {
	cert := &x509.Certificate{SerialNumber: big.NewInt(42)}
	sigTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	ocsps := []ocspBasicResponse{{
		Responses: []singleResponse{{
			SerialNumber: big.NewInt(42),
			Status:       1, // revoked
			ThisUpdate:   time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC),
		}},
	}}

	ok, info := checkRevocation(cert, sigTime, ocsps, nil)
	if ok {
		t.Errorf("expected ok=false for revoked cert: %s", info)
	}
}

func TestCheckRevocation_OCSPUnknown(t *testing.T) {
	cert := &x509.Certificate{SerialNumber: big.NewInt(42)}
	sigTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	ocsps := []ocspBasicResponse{{
		Responses: []singleResponse{{
			SerialNumber: big.NewInt(42),
			Status:       2, // unknown
			ThisUpdate:   time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC),
		}},
	}}

	ok, _ := checkRevocation(cert, sigTime, ocsps, nil)
	if ok {
		t.Error("expected ok=false for unknown status")
	}
}

func TestCheckRevocation_OCSPSerialMismatch(t *testing.T) {
	cert := &x509.Certificate{SerialNumber: big.NewInt(42)}
	sigTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	ocsps := []ocspBasicResponse{{
		Responses: []singleResponse{{
			SerialNumber: big.NewInt(99), // different serial
			Status:       0,
			ThisUpdate:   time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC),
		}},
	}}

	ok, info := checkRevocation(cert, sigTime, ocsps, nil)
	if ok {
		t.Error("expected ok=false when no matching OCSP response")
	}
	if info != "no OCSP or CRL data available for this certificate" {
		t.Errorf("unexpected info: %s", info)
	}
}

func TestCheckRevocation_CRLNotRevoked(t *testing.T) {
	cert := &x509.Certificate{SerialNumber: big.NewInt(42)}
	sigTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	// CRL with a different serial number revoked.
	crls := []*x509.RevocationList{{
		RevokedCertificateEntries: []x509.RevocationListEntry{{
			SerialNumber:   big.NewInt(99),
			RevocationTime: time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC),
		}},
	}}

	ok, info := checkRevocation(cert, sigTime, nil, crls)
	if !ok {
		t.Errorf("expected ok=true, cert not in CRL: %s", info)
	}
}

func TestCheckRevocation_CRLRevokedBeforeSigning(t *testing.T) {
	cert := &x509.Certificate{SerialNumber: big.NewInt(42)}
	sigTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	crls := []*x509.RevocationList{{
		RevokedCertificateEntries: []x509.RevocationListEntry{{
			SerialNumber:   big.NewInt(42),
			RevocationTime: time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC), // before signing
		}},
	}}

	ok, _ := checkRevocation(cert, sigTime, nil, crls)
	if ok {
		t.Error("expected ok=false when cert was revoked before signing time")
	}
}

func TestCheckRevocation_CRLRevokedAfterSigning(t *testing.T) {
	cert := &x509.Certificate{SerialNumber: big.NewInt(42)}
	sigTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	crls := []*x509.RevocationList{{
		RevokedCertificateEntries: []x509.RevocationListEntry{{
			SerialNumber:   big.NewInt(42),
			RevocationTime: time.Date(2023, 9, 1, 0, 0, 0, 0, time.UTC), // after signing
		}},
	}}

	ok, info := checkRevocation(cert, sigTime, nil, crls)
	if !ok {
		t.Errorf("expected ok=true when cert was revoked after signing: %s", info)
	}
}

func TestCheckRevocation_NoData(t *testing.T) {
	cert := &x509.Certificate{SerialNumber: big.NewInt(42)}
	sigTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	ok, info := checkRevocation(cert, sigTime, nil, nil)
	if ok {
		t.Error("expected ok=false with no revocation data")
	}
	if info != "no OCSP or CRL data available for this certificate" {
		t.Errorf("unexpected info: %s", info)
	}
}

func TestResolveDSSStreamArray_EmptyDict(t *testing.T) {
	// Smoke test: no keys match.
	result := resolveDSSStreamArray(context.Background(), nil, nil, "Certs", "Cert")
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestTimestampTokenParsing(t *testing.T) {
	// Generate a test certificate for the TSA.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test TSA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}

	// Build a minimal timestamp token (CMS wrapping a TSTInfo).
	tstDER, err := buildTestTimestampToken(cert, time.Date(2023, 12, 15, 10, 30, 45, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	result, err := parseTimestampToken(tstDER)
	if err != nil {
		t.Fatalf("parseTimestampToken: %v", err)
	}

	if result.IssuerCN != "Test TSA" {
		t.Errorf("IssuerCN: got %q, want %q", result.IssuerCN, "Test TSA")
	}

	if result.Time.Year() != 2023 || result.Time.Month() != time.December || result.Time.Day() != 15 {
		t.Errorf("timestamp time: got %v, want 2023-12-15", result.Time)
	}
}

func TestTimestampTokenParsing_InvalidDER(t *testing.T) {
	_, err := parseTimestampToken([]byte("not valid"))
	if err == nil {
		t.Error("expected error for invalid DER")
	}
}

// buildTestTimestampToken creates a minimal RFC 3161 timestamp token (CMS
// SignedData wrapping a TSTInfo) for testing. This is NOT a valid signature.
func buildTestTimestampToken(tsaCert *x509.Certificate, genTime time.Time) ([]byte, error) {
	// OIDs
	oidSignedData := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidTSTInfo := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
	oidSHA256 := asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}

	// Build TSTInfo.
	type messageImprint struct {
		HashAlgorithm pkix.AlgorithmIdentifier
		HashedMessage []byte
	}
	type tstInfoASN1 struct {
		Version        int
		Policy         asn1.ObjectIdentifier
		MessageImprint messageImprint
		SerialNumber   *big.Int
		GenTime        time.Time `asn1:"generalized"`
	}

	tst := tstInfoASN1{
		Version: 1,
		Policy:  asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}, // fake policy
		MessageImprint: messageImprint{
			HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: oidSHA256},
			HashedMessage: make([]byte, 32),
		},
		SerialNumber: big.NewInt(1),
		GenTime:      genTime,
	}

	tstBytes, err := asn1.Marshal(tst)
	if err != nil {
		return nil, err
	}

	// Wrap TSTInfo in an OCTET STRING for EncapsulatedContentInfo.
	tstOctetString, err := asn1.Marshal(tstBytes)
	if err != nil {
		return nil, err
	}

	// Build EncapsulatedContentInfo.
	type encapCI struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	eci := encapCI{
		ContentType: oidTSTInfo,
		Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: tstOctetString},
	}
	eciBytes, err := asn1.Marshal(eci)
	if err != nil {
		return nil, err
	}

	// Build SignedData.
	type signedDataASN1 struct {
		Version          int
		DigestAlgorithms asn1.RawValue
		EncapContentInfo asn1.RawValue
		Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	}

	sd := signedDataASN1{
		Version:          3,
		DigestAlgorithms: asn1.RawValue{Tag: asn1.TagSet, Class: asn1.ClassUniversal, IsCompound: true, Bytes: nil},
		EncapContentInfo: asn1.RawValue{FullBytes: eciBytes},
		Certificates:     asn1.RawValue{Tag: 0, Class: asn1.ClassContextSpecific, IsCompound: true, Bytes: tsaCert.Raw},
	}

	sdBytes, err := asn1.Marshal(sd)
	if err != nil {
		return nil, err
	}

	// Build ContentInfo.
	ci := struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}{
		ContentType: oidSignedData,
		Content:     asn1.RawValue{Tag: 0, Class: asn1.ClassContextSpecific, IsCompound: true, Bytes: sdBytes},
	}

	return asn1.Marshal(ci)
}
