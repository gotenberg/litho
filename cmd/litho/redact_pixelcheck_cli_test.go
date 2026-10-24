package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedact_PixelCheckPassesAnExactRedaction(t *testing.T) {
	path := writeTempPDF(t, twoPageSecretCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("no output written: %v", err)
	}
}

// TestRedact_PixelCheckRefusesAPageItCannotRender passes the deprecated
// --pixel-check, which scripts written when the check was opt-in still pass.
func TestRedact_PixelCheckRefusesAPageItCannotRender(t *testing.T) {
	path := writeTempPDF(t, twoPageSecretCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"--max-render-pixels", "1000", "redact", "-region", "1:90,690,210,40", "--pixel-check", "-o", out, path},
		&stdout, &stderr)
	if rc != exitError {
		t.Fatalf("exit %d, want %d; stderr: %s", rc, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "redaction pixel check: page 1 could not be checked") {
		t.Errorf("stderr = %q, want the pixel check's refusal", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused redaction left %s behind (stat err %v)", out, err)
	}
}

func TestRedact_NoPixelCheckSkipsIt(t *testing.T) {
	path := writeTempPDF(t, twoPageSecretCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	// The page cannot be rendered within the budget, so a check would refuse.
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"--max-render-pixels", "1000", "redact", "-region", "1:90,690,210,40", "--no-pixel-check", "-o", out, path},
		&stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("no output written: %v", err)
	}
}
