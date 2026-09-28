package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/internal/pkitest"
)

// TestTimestamp adds a document timestamp with the command and with a pipe's
// timestamp stage after its sign stage, and checks each verifies.
func TestTimestamp(t *testing.T) {
	pki := pkitest.New(t)
	certPath, keyPath := writePKIPEM(t, pki)

	t.Run("command", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "stamped.pdf")
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), []string{"timestamp", "--tsa-url", pki.TSAURL(), "--field", "Archive", "-o", out, onePageTempPDF(t)}, &stdout, &stderr); rc != exitOK {
			t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
		}
		results := validationsOf(t, out, pki)
		if len(results) != 1 || results[0].FieldName != "Archive" || !results[0].HashValid || results[0].SubFilter != "ETSI.RFC3161" {
			t.Errorf("validations %+v, want one document timestamp in Archive that verifies", results)
		}
	})

	t.Run("pipe after sign", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "lta.pdf")
		var stdout, stderr bytes.Buffer
		rc := run(context.Background(), []string{
			"pipe", onePageTempPDF(t),
			"sign", "--cert", certPath, "--key", keyPath, "--pades", "--tsa-url", pki.TSAURL(),
			"timestamp", "--tsa-url", pki.TSAURL(),
			"-o", out,
		}, &stdout, &stderr)
		if rc != exitOK {
			t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
		}
		results := validationsOf(t, out, pki)
		if len(results) != 2 {
			t.Fatalf("%d signatures, want 2", len(results))
		}
		for _, v := range results {
			if !v.HashValid || !v.CertChainValid {
				t.Errorf("%s: hash %v, chain %v: %s", v.FieldName, v.HashValid, v.CertChainValid, v.ErrorMessage)
			}
		}
	})

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"a missing URL", []string{"timestamp", "-o", "OUT", "SRC"}, "Usage: litho timestamp"},
		{"a stage after it", []string{"pipe", "SRC", "timestamp", "--tsa-url", pki.TSAURL(), "rotate", "-angle", "90", "-o", "OUT"}, "timestamp must be the last stage"},
		{"sign after it", []string{"pipe", "SRC", "timestamp", "--tsa-url", pki.TSAURL(), "sign", "--cert", certPath, "--key", keyPath, "-o", "OUT"}, `move "sign" before timestamp`},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.pdf")
			src := onePageTempPDF(t)
			args := make([]string, len(tc.args))
			for i, a := range tc.args {
				args[i] = strings.NewReplacer("OUT", out, "SRC", src).Replace(a)
			}
			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
				t.Fatal("exit 0, want a refusal")
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr %q, want it to say %q", stderr.String(), tc.want)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("a refused run left %s behind: %v", out, err)
			}
		})
	}
}
