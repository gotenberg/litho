package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
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
