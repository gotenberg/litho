package signature

import (
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

func TestParsePDFDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantY   int
		wantM   time.Month
		wantD   int
	}{
		{
			name:  "full with timezone",
			input: "D:20231215103045+05'30'",
			wantY: 2023, wantM: time.December, wantD: 15,
		},
		{
			name:  "full with Z timezone",
			input: "D:20231215103045Z",
			wantY: 2023, wantM: time.December, wantD: 15,
		},
		{
			name:  "without timezone",
			input: "D:20231215103045",
			wantY: 2023, wantM: time.December, wantD: 15,
		},
		{
			name:  "date only",
			input: "D:20231215",
			wantY: 2023, wantM: time.December, wantD: 15,
		},
		{
			name:  "no D: prefix",
			input: "20231215103045",
			wantY: 2023, wantM: time.December, wantD: 15,
		},
		{
			name:  "negative timezone",
			input: "D:20231215103045-08'00'",
			wantY: 2023, wantM: time.December, wantD: 15,
		},
		{
			name:  "ISO 8601 fallback",
			input: "2023-12-15T10:30:45Z",
			wantY: 2023, wantM: time.December, wantD: 15,
		},
		{
			name:    "garbage",
			input:   "not-a-date",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePDFDate(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Year() != tc.wantY || got.Month() != tc.wantM || got.Day() != tc.wantD {
				t.Errorf("got %v, want %d-%02d-%02d", got, tc.wantY, tc.wantM, tc.wantD)
			}
		})
	}
}

// buildTestPKCS7 creates a minimal DER-encoded PKCS#7 SignedData structure
// containing the given certificates. This is NOT a valid signature — it only
// tests that our parser can extract the embedded certificates.
func buildTestPKCS7(certs ...*x509.Certificate) ([]byte, error) {
	// OID for PKCS#7 SignedData: 1.2.840.113549.1.7.2
	oidSignedData := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	// OID for PKCS#7 Data: 1.2.840.113549.1.7.1
	oidData := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}

	// Concatenate raw certificate DER bytes.
	var rawCerts []byte
	for _, cert := range certs {
		rawCerts = append(rawCerts, cert.Raw...)
	}

	// Build SignedData.
	type signedDataASN1 struct {
		Version          int
		DigestAlgorithms asn1.RawValue
		EncapContentInfo asn1.RawValue
		Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	}

	encapCI, _ := asn1.Marshal(oidData)
	sd := signedDataASN1{
		Version:          1,
		DigestAlgorithms: asn1.RawValue{Tag: asn1.TagSet, Class: asn1.ClassUniversal, IsCompound: true, Bytes: nil},
		EncapContentInfo: asn1.RawValue{FullBytes: encapCI},
		Certificates:     asn1.RawValue{Tag: 0, Class: asn1.ClassContextSpecific, IsCompound: true, Bytes: rawCerts},
	}

	sdBytes, err := asn1.Marshal(sd)
	if err != nil {
		return nil, err
	}

	ci := struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}{
		ContentType: oidSignedData,
		Content:     asn1.RawValue{Tag: 0, Class: asn1.ClassContextSpecific, IsCompound: true, Bytes: sdBytes},
	}

	return asn1.Marshal(ci)
}

func TestParsePKCS7Certificates(t *testing.T) {
	// Generate a self-signed test certificate.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test Signer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}

	pkcs7DER, err := buildTestPKCS7(cert)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := parsePKCS7Certificates(pkcs7DER)
	if err != nil {
		t.Fatalf("parsePKCS7Certificates: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(parsed))
	}
	if parsed[0].Subject.CommonName != "Test Signer" {
		t.Errorf("CN: got %q, want %q", parsed[0].Subject.CommonName, "Test Signer")
	}
}

func TestParsePKCS7Certificates_MultipleCerts(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	rootDER, _ := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &key.PublicKey, key)
	rootCert, _ := x509.ParseCertificate(rootDER)

	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Test Leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	leafDER, _ := x509.CreateCertificate(rand.Reader, leafTemplate, rootCert, &leafKey.PublicKey, key)
	leafCert, _ := x509.ParseCertificate(leafDER)

	pkcs7DER, err := buildTestPKCS7(leafCert, rootCert)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := parsePKCS7Certificates(pkcs7DER)
	if err != nil {
		t.Fatalf("parsePKCS7Certificates: %v", err)
	}

	if len(parsed) != 2 {
		t.Fatalf("expected 2 certificates, got %d", len(parsed))
	}
	if parsed[0].Subject.CommonName != "Test Leaf" {
		t.Errorf("first cert CN: got %q, want %q", parsed[0].Subject.CommonName, "Test Leaf")
	}
	if parsed[1].Subject.CommonName != "Test Root CA" {
		t.Errorf("second cert CN: got %q, want %q", parsed[1].Subject.CommonName, "Test Root CA")
	}
}

func TestParsePKCS7Certificates_InvalidDER(t *testing.T) {
	_, err := parsePKCS7Certificates([]byte("not valid der"))
	if err == nil {
		t.Error("expected error for invalid DER, got nil")
	}
}
