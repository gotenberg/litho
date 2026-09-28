package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// p12TestData is where pkg/internal/pkcs12/testdata/gen.sh writes its
// OpenSSL key stores and their root certificate.
var p12TestData = filepath.Join("..", "..", "pkg", "internal", "pkcs12", "testdata")

// writePasswordFile writes content to a file and returns its path.
func writePasswordFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSign_SignsWithAPKCS12Store signs with an OpenSSL default (AES) store,
// its password in a file, and a -legacy (RC2, triple DES) store, its
// password in an environment variable, and checks each signature verifies
// up to the stores' root.
func TestSign_SignsWithAPKCS12Store(t *testing.T) {
	t.Setenv("LITHO_TEST_P12_PASSWORD", "pässwörd")
	root := filepath.Join(p12TestData, "root.pem")
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"aes, password file", []string{"--p12", filepath.Join(p12TestData, "aes256.p12"), "--p12-password-file", writePasswordFile(t, "litho-test\r\n")}},
		{"legacy, password variable", []string{"--p12", filepath.Join(p12TestData, "legacy.p12"), "--p12-password-env", "LITHO_TEST_P12_PASSWORD"}},
		{"empty password", []string{"--p12", filepath.Join(p12TestData, "empty.p12")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "signed.pdf")
			var stdout, stderr bytes.Buffer
			args := append(append([]string{"sign"}, tc.args...), "-o", out, onePageTempPDF(t))
			if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
				t.Fatalf("sign: exit %d, stderr: %s", rc, stderr.String())
			}
			if got := verifiedSignatures(t, out, root); len(got) != 1 {
				t.Errorf("verified signatures %q, want one", got)
			}
		})
	}

	out := filepath.Join(t.TempDir(), "signed.pdf")
	var stdout, stderr bytes.Buffer
	args := []string{
		"pipe", onePageTempPDF(t), "rotate", "--angle", "90",
		"sign", "--p12", filepath.Join(p12TestData, "legacy.p12"), "--p12-password-env", "LITHO_TEST_P12_PASSWORD", "-o", out,
	}
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("pipe: exit %d, stderr: %s", rc, stderr.String())
	}
	if got := verifiedSignatures(t, out, root); len(got) != 1 {
		t.Errorf("pipe: verified signatures %q, want one", got)
	}
}

// TestSign_RefusesAPKCS12Misuse checks each misuse of the PKCS #12 flags
// fails with an explanation and leaves no output file.
func TestSign_RefusesAPKCS12Misuse(t *testing.T) {
	store := filepath.Join(p12TestData, "aes256.p12")
	password := writePasswordFile(t, "litho-test\n")
	certPath, keyPath := writeSigningPEM(t, "ec")
	src := onePageTempPDF(t)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"a wrong password", []string{"--p12", store, "--p12-password-file", writePasswordFile(t, "wrong")}, "--p12-password-file or --p12-password-env"},
		{"no password", []string{"--p12", store}, "incorrect PKCS #12 password"},
		{"--p12 with --cert", []string{"--p12", store, "--p12-password-file", password, "--cert", certPath}, "replaces --cert"},
		{"both password sources", []string{"--p12", store, "--p12-password-file", password, "--p12-password-env", "HOME"}, "exclusive"},
		{"an unset variable", []string{"--p12", store, "--p12-password-env", "LITHO_TEST_P12_UNSET"}, "is not set"},
		{"a password without a store", []string{"--cert", certPath, "--key", keyPath, "--p12-password-file", password}, "need --p12"},
		{"a store with no key", []string{"--p12", filepath.Join(p12TestData, "nokey.p12"), "--p12-password-file", password}, "0 private keys"},
		{"a missing store", []string{"--p12", filepath.Join(t.TempDir(), "absent.p12")}, "reading the key store"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "signed.pdf")
			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), append(append([]string{"sign", "-o", out}, tc.args...), src), &stdout, &stderr)
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
