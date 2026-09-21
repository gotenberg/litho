package signature

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"math/big"
	"testing"
	"time"
)

var (
	oidPKCS7Data       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidPKCS7SignedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidAttrContentType = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidEcdsaWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
)

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := asn1.Marshal(v)
	if err != nil {
		t.Fatalf("asn1.Marshal(%T): %v", v, err)
	}
	return b
}

// buildSignedCMS produces a detached PKCS#7/CMS SignedData over content,
// signed with ECDSA-SHA256 and carrying the contentType + messageDigest
// signed attributes, mirroring how a real PDF signer builds /Contents.
func buildSignedCMS(t *testing.T, key *ecdsa.PrivateKey, cert *x509.Certificate, content []byte) []byte {
	t.Helper()

	digest := sha256.Sum256(content)

	// Signed attributes: contentType (id-data) and messageDigest.
	type rawAttribute struct {
		Type   asn1.ObjectIdentifier
		Values []asn1.RawValue `asn1:"set"`
	}
	contentTypeAttr := rawAttribute{
		Type:   oidAttrContentType,
		Values: []asn1.RawValue{{FullBytes: mustMarshal(t, oidPKCS7Data)}},
	}
	messageDigestAttr := rawAttribute{
		Type:   oidAttrMessageDigest,
		Values: []asn1.RawValue{{Class: asn1.ClassUniversal, Tag: asn1.TagOctetString, Bytes: digest[:]}},
	}

	// Concatenated Attribute SEQUENCEs form the SET content.
	attrContent := append(mustMarshal(t, contentTypeAttr), mustMarshal(t, messageDigestAttr)...)

	// SET form (0x31) is what gets signed (RFC 5652 §5.4).
	setForm := mustMarshal(t, asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: attrContent})
	// [0] IMPLICIT form is what gets embedded in the SignerInfo.
	implForm := mustMarshal(t, asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: attrContent})

	attrsDigest := sha256.Sum256(setForm)
	sig, err := ecdsa.SignASN1(rand.Reader, key, attrsDigest[:])
	if err != nil {
		t.Fatalf("ecdsa.SignASN1: %v", err)
	}

	sid := mustMarshal(t, issuerAndSerialNumber{
		Issuer:       asn1.RawValue{FullBytes: cert.RawIssuer},
		SerialNumber: cert.SerialNumber,
	})

	type signerInfoMarshal struct {
		Version            int
		SID                asn1.RawValue
		DigestAlgorithm    asn1.RawValue
		SignedAttrs        asn1.RawValue
		SignatureAlgorithm asn1.RawValue
		Signature          []byte
	}
	si := signerInfoMarshal{
		Version:            1,
		SID:                asn1.RawValue{FullBytes: sid},
		DigestAlgorithm:    asn1.RawValue{FullBytes: mustMarshal(t, algorithmIdentifier{Algorithm: oidSHA256})},
		SignedAttrs:        asn1.RawValue{FullBytes: implForm},
		SignatureAlgorithm: asn1.RawValue{FullBytes: mustMarshal(t, algorithmIdentifier{Algorithm: oidEcdsaWithSHA256})},
		Signature:          sig,
	}
	siBytes := mustMarshal(t, si)

	digestAlgsSet := mustMarshal(t, asn1.RawValue{
		Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true,
		Bytes: mustMarshal(t, algorithmIdentifier{Algorithm: oidSHA256}),
	})
	// Detached EncapContentInfo: just the content type, no eContent.
	encapCI := mustMarshal(t, struct{ ContentType asn1.ObjectIdentifier }{oidPKCS7Data})

	type signedDataMarshal struct {
		Version          int
		DigestAlgorithms asn1.RawValue
		EncapContentInfo asn1.RawValue
		Certificates     asn1.RawValue `asn1:"optional,tag:0"`
		SignerInfos      asn1.RawValue
	}
	sd := signedDataMarshal{
		Version:          1,
		DigestAlgorithms: asn1.RawValue{FullBytes: digestAlgsSet},
		EncapContentInfo: asn1.RawValue{FullBytes: encapCI},
		Certificates:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: cert.Raw},
		SignerInfos:      asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: siBytes},
	}
	sdBytes := mustMarshal(t, sd)

	ci := struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}{
		ContentType: oidPKCS7SignedData,
		Content:     asn1.RawValue{Tag: 0, Class: asn1.ClassContextSpecific, IsCompound: true, Bytes: sdBytes},
	}
	return mustMarshal(t, ci)
}

// testSigningCert takes a [testing.TB] rather than a *testing.T so a fuzz
// target's setup, which is handed a *testing.F, can build the same seed
// certificate the unit tests use.
func testSigningCert(tb testing.TB) (*ecdsa.PrivateKey, *x509.Certificate) {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatalf("GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(0x4242),
		Subject:      pkix.Name{CommonName: "CMS Test Signer"},
		Issuer:       pkix.Name{CommonName: "CMS Test Signer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		tb.Fatalf("CreateCertificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		tb.Fatalf("ParseCertificate: %v", err)
	}
	return key, cert
}

func TestVerifyCMSSignature_Valid(t *testing.T) {
	key, cert := testSigningCert(t)
	content := []byte("the document bytes covered by the signature ByteRange")
	der := buildSignedCMS(t, key, cert, content)

	signer, err := verifyCMSSignature(der, content, []*x509.Certificate{cert})
	if err != nil {
		t.Fatalf("verifyCMSSignature on a valid signature: %v", err)
	}
	if signer == nil || signer.Subject.CommonName != "CMS Test Signer" {
		t.Fatalf("returned signer = %v, want the test signer", signer)
	}
}

func TestVerifyCMSSignature_TamperedContent(t *testing.T) {
	key, cert := testSigningCert(t)
	content := []byte("original signed content")
	der := buildSignedCMS(t, key, cert, content)

	// A different byte range yields a different digest; the messageDigest
	// attribute no longer matches.
	_, err := verifyCMSSignature(der, []byte("tampered content!!!!!"), []*x509.Certificate{cert})
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("tampered content: got %v, want ErrSignatureInvalid", err)
	}
}

func TestVerifyCMSSignature_TamperedSignature(t *testing.T) {
	key, cert := testSigningCert(t)
	content := []byte("content with a corrupted signature value")
	der := buildSignedCMS(t, key, cert, content)

	// Flip a byte inside the DER. Walk from the end (the signature value
	// and signed attributes live there) to corrupt the signature without
	// breaking the outer ASN.1 framing the parser needs.
	tampered := make([]byte, len(der))
	copy(tampered, der)
	tampered[len(tampered)-1] ^= 0xFF

	_, err := verifyCMSSignature(tampered, content, []*x509.Certificate{cert})
	if err == nil {
		t.Fatal("corrupted signature verified as valid")
	}
}

func TestVerifyCMSSignature_WrongSignerCert(t *testing.T) {
	key, cert := testSigningCert(t)
	content := []byte("signed by key A, verified against cert B")
	der := buildSignedCMS(t, key, cert, content)

	// A different certificate (different serial) is not the signer.
	_, otherCert := testSigningCert(t)
	_, err := verifyCMSSignature(der, content, []*x509.Certificate{otherCert})
	if err == nil {
		t.Fatal("verified against a non-signer certificate")
	}
}
