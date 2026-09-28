package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

// writeSigningPEM writes a self-signed certificate and its unencrypted private
// key, of kind "ec" (P-256, SEC 1) or "rsa" (2048 bits, PKCS #8), to PEM files
// and returns their paths.
func writeSigningPEM(t *testing.T, kind string) (certPath, keyPath string) {
	t.Helper()

	var key crypto.Signer
	var keyBlock *pem.Block
	switch kind {
	case "ec":
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		key, keyBlock = k, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	case "rsa":
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		key, keyBlock = k, &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	default:
		t.Fatalf("unknown key kind %q", kind)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "litho cli signer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(keyBlock), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

// verifiedSignatures opens path and verifies its signatures, trusting the
// certificate in certPath.
func verifiedSignatures(t *testing.T, path, certPath string) []string {
	t.Helper()
	ctx := context.Background()

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("no certificate in the PEM file")
	}
	r, err := pdf.OpenFile(ctx, path)
	if err != nil {
		t.Fatalf("opening the signed output: %v", err)
	}
	defer r.Close()
	results, err := r.VerifySignatures(ctx, roots)
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	var valid []string
	for _, v := range results {
		if v.HashValid && v.CertChainValid {
			valid = append(valid, v.FieldName)
		}
	}
	return valid
}

// signatureCountOf returns how many signatures the document at path holds.
func signatureCountOf(t *testing.T, path string) int {
	t.Helper()
	ctx := context.Background()
	r, err := pdf.OpenFile(ctx, path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer r.Close()
	sigs, err := r.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	return len(sigs)
}

// TestSign_SignsWithAPEMCertificateAndKey signs with an ECDSA and an RSA key
// and checks the output holds one signature that verifies, in the field the
// command was asked for.
func TestSign_SignsWithAPEMCertificateAndKey(t *testing.T) {
	for _, kind := range []string{"ec", "rsa"} {
		t.Run(kind, func(t *testing.T) {
			certPath, keyPath := writeSigningPEM(t, kind)
			out := filepath.Join(t.TempDir(), "signed.pdf")

			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), []string{
				"sign", "--cert", certPath, "--key", keyPath, "--reason", "Approved",
				"--location", "Paris", "--field", "Approval", "--signing-time", "2026-09-28T10:00:00Z",
				"-o", out, onePageTempPDF(t),
			}, &stdout, &stderr)
			if rc != exitOK {
				t.Fatalf("sign: exit %d, stderr: %s", rc, stderr.String())
			}
			if got := verifiedSignatures(t, out, certPath); len(got) != 1 || got[0] != "Approval" {
				t.Errorf("verified signatures %q, want one in Approval", got)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte("/M (D:20260928100000Z)")) {
				t.Error("the signature dictionary does not record --signing-time")
			}
		})
	}
}

// TestSign_Refuses checks each input sign cannot use fails with an
// explanation and leaves no output file.
func TestSign_Refuses(t *testing.T) {
	certPath, keyPath := writeSigningPEM(t, "ec")
	otherCert, _ := writeSigningPEM(t, "rsa")
	encryptedKey := filepath.Join(t.TempDir(), "encrypted.pem")
	if err := os.WriteFile(encryptedKey, pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte{0x30, 0}}), 0o600); err != nil {
		t.Fatal(err)
	}
	src := onePageTempPDF(t)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"an encrypted key", []string{"--cert", certPath, "--key", encryptedKey, src}, "decrypt it first"},
		{"another key's certificate", []string{"--cert", otherCert, "--key", keyPath, src}, "does not match"},
		{"an encrypted source", []string{"--cert", certPath, "--key", keyPath, "-p", "secret", encryptTempPDF(t, src, "secret")}, "encrypted document"},
		{"no key", []string{"--cert", certPath, src}, "Usage"},
		{"a bad signing time", []string{"--cert", certPath, "--key", keyPath, "--signing-time", "yesterday", src}, "--signing-time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "signed.pdf")
			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), append([]string{"sign", "-o", out}, tc.args...), &stdout, &stderr)
			if rc == exitOK {
				t.Fatal("sign succeeded")
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want it to mention %q", strings.TrimSpace(stderr.String()), tc.want)
			}
			if _, err := os.Stat(out); err == nil {
				t.Errorf("%s exists: a refusal must not create an output file", out)
			}
		})
	}
}

// TestPipe_SignStage signs after a remediation in one pipe, and refuses a
// stage after sign and an encrypt stage beside it.
func TestPipe_SignStage(t *testing.T) {
	certPath, keyPath := writeSigningPEM(t, "ec")
	src := onePageTempPDF(t)
	sign := []string{"sign", "--cert", certPath, "--key", keyPath, "--reason", "rotate"}

	out := filepath.Join(t.TempDir(), "signed.pdf")
	args := append(append([]string{"pipe", src, "rotate", "--angle", "90", "remediate", "--profile", "PDF/A-2b"}, sign...), "-o", out)
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("pipe: exit %d, stderr: %s", rc, stderr.String())
	}
	if got := verifiedSignatures(t, out, certPath); len(got) != 1 {
		t.Errorf("verified signatures %q, want one", got)
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"a stage after sign", append(append([]string{"pipe", src}, sign...), "rotate", "--angle", "90"), "sign must be the last stage"},
		{"two sign stages", append(append([]string{"pipe", src}, sign...), sign...), "only one sign stage"},
		{"an encrypt stage", append([]string{"pipe", src, "encrypt", "--user-password", "a"}, sign...), "cannot come with a sign stage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "signed.pdf")
			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), append(tc.args, "-o", out), &stdout, &stderr); rc == exitOK {
				t.Fatal("pipe succeeded")
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want it to mention %q", strings.TrimSpace(stderr.String()), tc.want)
			}
			if _, err := os.Stat(out); err == nil {
				t.Errorf("%s exists: a refusal must not create an output file", out)
			}
		})
	}
}
