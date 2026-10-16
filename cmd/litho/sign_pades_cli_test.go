package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/internal/pkitest"
	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// TestSign_PAdES signs with --pades, alone and through a pipe's sign stage
// followed by nothing, and checks the output holds one verifying signature
// whose dictionary names the ETSI.CAdES.detached subfilter.
func TestSign_PAdES(t *testing.T) {
	certPath, keyPath := writeSigningPEM(t, "ec")
	for _, tc := range []struct {
		name string
		args func(src, out string) []string
	}{
		{"sign", func(src, out string) []string {
			return []string{"sign", "--cert", certPath, "--key", keyPath, "--pades", "-o", out, src}
		}},
		{"pipe", func(src, out string) []string {
			return []string{"pipe", src, "sign", "--cert", certPath, "--key", keyPath, "--pades", "-o", out}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "signed.pdf")
			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), tc.args(onePageTempPDF(t), out), &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			if got := verifiedSignatures(t, out, certPath); len(got) != 1 {
				t.Errorf("verified signatures %q, want one", got)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte("/SubFilter /ETSI.CAdES.detached")) {
				t.Error("the signature dictionary does not name ETSI.CAdES.detached")
			}
		})
	}
}

// writePKIPEM writes the PKI's signing certificate followed by its
// intermediate, and its private key, to PEM files and returns their paths.
func writePKIPEM(t *testing.T, pki *pkitest.PKI) (certPath, keyPath string) {
	t.Helper()
	dir := t.TempDir()
	certPath, keyPath = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	var certs []byte
	for _, c := range []*x509.Certificate{pki.Leaf, pki.Intermediate} {
		certs = append(certs, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	der, err := x509.MarshalECPrivateKey(pki.LeafKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, certs, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

// validationsOf verifies the signatures of the document at path against the
// PKI's root.
func validationsOf(t *testing.T, path string, pki *pkitest.PKI) []models.SignatureValidation {
	t.Helper()
	ctx := context.Background()
	r, err := pdf.OpenFile(ctx, path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer r.Close()
	results, err := r.VerifySignatures(ctx, pki.Roots())
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	return results
}

// TestSign_TimestampAuthority signs with --tsa-url against a local timestamp
// authority, and checks a refusing authority fails the command and leaves no
// output file.
func TestSign_TimestampAuthority(t *testing.T) {
	pki := pkitest.New(t)
	certPath, keyPath := writePKIPEM(t, pki)
	out := filepath.Join(t.TempDir(), "signed.pdf")
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{
		"sign", "--cert", certPath, "--key", keyPath, "--pades", "--tsa-url", pki.TSAURL(), "-o", out, onePageTempPDF(t),
	}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	results := validationsOf(t, out, pki)
	if len(results) != 1 || !results[0].HashValid || !results[0].CertChainValid || !results[0].HasTimestamp || results[0].TimestampError != "" {
		t.Errorf("validations %+v, want one timestamped signature that verifies", results)
	}

	pki.SetTSAMode(pkitest.TSAReject)
	refused := filepath.Join(t.TempDir(), "refused.pdf")
	stderr.Reset()
	rc = run(context.Background(), []string{
		"sign", "--cert", certPath, "--key", keyPath, "--tsa-url", pki.TSAURL(), "-o", refused, onePageTempPDF(t),
	}, &stdout, &stderr)
	if rc != exitError {
		t.Errorf("a refusing authority: exit %d, want %d", rc, exitError)
	}
	if _, err := os.Stat(refused); !os.IsNotExist(err) {
		t.Errorf("a refused signature left %s behind: %v", refused, err)
	}
}

// TestSign_LTV signs with --ltv against a local OCSP responder, alone and
// through a pipe's sign stage, and checks the verifier calls the signature
// LTV-enabled; a revoked signer fails the command.
func TestSign_LTV(t *testing.T) {
	pki := pkitest.New(t)
	certPath, keyPath := writePKIPEM(t, pki)
	for _, tc := range []struct {
		name string
		args func(src, out string) []string
	}{
		{"sign", func(src, out string) []string {
			return []string{"sign", "--cert", certPath, "--key", keyPath, "--pades", "--tsa-url", pki.TSAURL(), "--ltv", "-o", out, src}
		}},
		{"pipe", func(src, out string) []string {
			return []string{"pipe", src, "sign", "--cert", certPath, "--key", keyPath, "--pades", "--ltv", "--tsa-url", pki.TSAURL(), "-o", out}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "signed.pdf")
			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), tc.args(onePageTempPDF(t), out), &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			results := validationsOf(t, out, pki)
			if len(results) != 1 || !results[0].HashValid || !results[0].LTVEnabled || !results[0].RevocationOK {
				t.Errorf("validations %+v, want one LTV-enabled signature", results)
			}
		})
	}

	pki.Revoke(pki.Leaf, time.Now().Add(-time.Hour))
	out := filepath.Join(t.TempDir(), "revoked.pdf")
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"sign", "--cert", certPath, "--key", keyPath, "--ltv", "-o", out, onePageTempPDF(t)}, &stdout, &stderr); rc != exitError {
		t.Errorf("a revoked signer: exit %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "revoked") {
		t.Errorf("stderr %q does not say the certificate is revoked", stderr.String())
	}
}

// TestSign_PKCS12PAdESTimestamp signs with a PKCS #12 key store, --pades and
// --tsa-url against a local timestamp authority, alone and through a pipe's
// sign stage, and checks the output holds one PAdES signature that verifies
// up to the store's root and carries a verifying signature timestamp.
func TestSign_PKCS12PAdESTimestamp(t *testing.T) {
	pki := pkitest.New(t)
	store := filepath.Join(p12TestData, "aes256.p12")
	password := writePasswordFile(t, "litho-test\n")

	rootPEM, err := os.ReadFile(filepath.Join(p12TestData, "root.pem"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		t.Fatal("no certificate in the key stores' root.pem")
	}
	roots.AddCert(pki.Root)

	for _, tc := range []struct {
		name string
		args func(src, out string) []string
	}{
		{"sign", func(src, out string) []string {
			return []string{"sign", "--p12", store, "--p12-password-file", password, "--pades", "--tsa-url", pki.TSAURL(), "-o", out, src}
		}},
		{"pipe", func(src, out string) []string {
			return []string{"pipe", src, "sign", "--p12", store, "--p12-password-file", password, "--pades", "--tsa-url", pki.TSAURL(), "-o", out}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "signed.pdf")
			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), tc.args(onePageTempPDF(t), out), &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			ctx := context.Background()
			r, err := pdf.OpenFile(ctx, out)
			if err != nil {
				t.Fatalf("opening %s: %v", out, err)
			}
			defer r.Close()
			results, err := r.VerifySignatures(ctx, roots)
			if err != nil {
				t.Fatalf("VerifySignatures: %v", err)
			}
			if len(results) != 1 || !results[0].HashValid || !results[0].CertChainValid || !results[0].HasTimestamp || results[0].TimestampError != "" {
				t.Errorf("validations %+v, want one timestamped signature that verifies", results)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte("/SubFilter /ETSI.CAdES.detached")) {
				t.Error("the signature dictionary does not name ETSI.CAdES.detached")
			}
		})
	}
}
