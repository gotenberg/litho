package pdf_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

// ExampleNewSigner signs a document with an ECDSA key and its certificate,
// then verifies the signature against a pool trusting that certificate. A
// real program loads the key from a file, an HSM or a cloud key service: any
// [crypto.Signer] of an RSA or ECDSA key works.
func ExampleNewSigner() {
	ctx := context.Background()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	cert := selfSignedCertificate(key)
	signer, err := pdf.NewSigner(cert, key)
	if err != nil {
		log.Fatal(err)
	}

	signed := sign(ctx, signer)

	roots := x509.NewCertPool()
	roots.AddCert(cert)
	printSignatures(ctx, signed, roots)
	// Output:
	// Signature1 signed by litho example signer: hash valid true, chain valid true
}

// ExampleParseSignerPEM signs a document with a certificate and a private key
// read from PEM, the form openssl writes. A real program reads the two files,
// for instance with os.ReadFile("cert.pem") and os.ReadFile("key.pem"); a
// file holding both may be passed as both arguments.
func ExampleParseSignerPEM() {
	ctx := context.Background()

	certPEM, keyPEM := selfSignedPEM()
	signer, err := pdf.ParseSignerPEM(certPEM, keyPEM)
	if err != nil {
		log.Fatal(err)
	}

	signed := sign(ctx, signer)

	roots := x509.NewCertPool()
	roots.AddCert(signer.Certificate())
	printSignatures(ctx, signed, roots)
	// Output:
	// Signature1 signed by litho example signer: hash valid true, chain valid true
}

// sign returns unembeddedFontsPDF signed by signer, at a pinned signing time
// the certificate of selfSignedCertificate covers.
func sign(ctx context.Context, signer pdf.Signer) []byte {
	r, err := pdf.Open(ctx, bytes.NewReader(unembeddedFontsPDF), int64(len(unembeddedFontsPDF)))
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	w := pdf.NewWriter(r)
	defer w.Close()
	w.Sign(signer,
		pdf.WithSignatureReason("Approved"),
		pdf.WithSigningTime(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)),
	)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		log.Fatal(err)
	}
	return out.Bytes()
}

// printSignatures prints each signature of data as roots verify it.
func printSignatures(ctx context.Context, data []byte, roots *x509.CertPool) {
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	results, err := r.VerifySignatures(ctx, roots)
	if err != nil {
		log.Fatal(err)
	}
	for _, v := range results {
		fmt.Printf("%s signed by %s: hash valid %t, chain valid %t\n", v.FieldName, v.SignerCN, v.HashValid, v.CertChainValid)
	}
}

// selfSignedCertificate returns a self-signed signing certificate for key,
// valid from 2026 to the end of year 9999, the latest time X.509 encodes, so
// the example never outlives its certificate. A real signer's certificate
// comes from a certificate authority.
func selfSignedCertificate(key crypto.Signer) *x509.Certificate {
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "litho example signer"},
		NotBefore:             time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		log.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		log.Fatal(err)
	}
	return cert
}

// selfSignedPEM returns the PEM of a self-signed certificate and of its
// PKCS #8 private key, the content of the cert.pem and key.pem files a real
// program reads.
func selfSignedPEM() (certPEM, keyPEM []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		log.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: selfSignedCertificate(key).Raw})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	return certPEM, keyPEM
}
