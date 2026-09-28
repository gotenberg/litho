package pdf

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// pkcs12TestData is where pkg/internal/pkcs12/testdata/gen.sh writes its
// OpenSSL stores.
var pkcs12TestData = filepath.Join("..", "internal", "pkcs12", "testdata")

// readPKCS12Fixture returns the bytes of an OpenSSL store of pkcs12TestData.
func readPKCS12Fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pkcs12TestData, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestParseSignerPKCS12 signs with each OpenSSL store, default AES and
// -legacy RC2 and triple DES among them, and checks litho's verifier accepts
// the signature and its chain up to the stores' root.
func TestParseSignerPKCS12(t *testing.T) {
	t.Parallel()
	rootPEM, err := os.ReadFile(filepath.Join(pkcs12TestData, "root.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(rootPEM)
	if block == nil {
		t.Fatal("root.pem holds no PEM block")
	}
	root, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		file, password, signer string
	}{
		{"aes256.p12", "litho-test", "litho pkcs12 ec signer"},
		{"legacy.p12", "pässwörd", "litho pkcs12 rsa signer"},
		{"mixed.p12", "litho-test", "litho pkcs12 ec signer"},
		{"plain.p12", "litho-test", "litho pkcs12 ec signer"},
		{"empty.p12", "", "litho pkcs12 ec signer"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			signer, err := ParseSignerPKCS12(readPKCS12Fixture(t, tc.file), tc.password)
			if err != nil {
				t.Fatalf("ParseSignerPKCS12: %v", err)
			}
			leaf := signer.Certificate()
			if leaf.Subject.CommonName != tc.signer {
				t.Fatalf("signs as %q, want %q", leaf.Subject.CommonName, tc.signer)
			}
			verifyOne(t, signWith(t, writerTestPDF(), signer), testPKI{root: root, leaf: leaf}, 1)
		})
	}

	for _, tc := range []struct {
		name, file, password string
		data                 []byte
		want                 error
	}{
		{name: "a wrong password", file: "aes256.p12", password: "wrong", want: ErrSignerPKCS12Password},
		{name: "a wrong legacy password", file: "legacy.p12", password: "passwort", want: ErrSignerPKCS12Password},
		{name: "a store with no key", file: "nokey.p12", password: "litho-test", want: ErrSignerPKCS12},
		{name: "a PEM file", data: rootPEM, want: ErrSignerPKCS12},
		{name: "nothing", data: []byte{}, want: ErrSignerPKCS12},
	} {
		data := tc.data
		if tc.file != "" {
			data = readPKCS12Fixture(t, tc.file)
		}
		if _, err := ParseSignerPKCS12(data, tc.password); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}
