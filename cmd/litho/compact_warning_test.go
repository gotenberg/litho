package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// The encryption warning answers "is the output encrypted", and once read the
// writeOpts slice to do so. Adding compaction options to that slice made its
// length stop answering the question: --compact alone made it non-empty, which
// suppressed the warning entirely.
//
// Nothing caught it. The full suite and the linter passed, because no test
// asserted the warning beside --compact. This does.

const warnEncrypted = "input is encrypted"

// TestCompactWarning_DoesNotSuppressTheEncryptionWarning is the regression.
//
// An encrypted input written to a plaintext output must still say so, and
// asking for compaction must not silence it: that warning tells the caller
// their protected document just became an unprotected one.
//
// # Corpus evidence
//
//   - All corpora: the corpus gate passed while --compact suppressed the
//     encryption warning, because no test asserted that the two coexist.
func TestCompactWarning_DoesNotSuppressTheEncryptionWarning(t *testing.T) {
	enc := encryptTempPDF(t, onePageTempPDF(t), "secret")
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"split", "-pages", "1", "-p", "secret", "--compact", "-o", out, enc,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("split: exit %d, stderr: %s", rc, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, warnEncrypted) {
		t.Errorf("--compact suppressed the encryption warning; stderr was: %q", got)
	}
}

// TestCompact_AppliesToAnEncryptedOutput pins that --compact and
// --recompress-images reach an encrypted output: the write engine compacts
// before it encrypts, so the commands that encrypt pack their output and say
// nothing about it. An earlier engine refused the passes there, and the
// commands warned.
func TestCompact_AppliesToAnEncryptedOutput(t *testing.T) {
	ctx := context.Background()
	src := writeTempPDF(t, compactFixturePDF())

	for _, tc := range []struct {
		name string
		args func(out string) []string
	}{
		{"encrypt", func(out string) []string {
			return []string{"encrypt", "--compact", "--recompress-images", "--user-password", "s", "-o", out, src}
		}},
		{"split", func(out string) []string {
			// Page 2 carries the padded objects; page 1 alone is too small to pack.
			return []string{"split", "-pages", "2", "--compact", "--user-password", "s", "-o", out, src}
		}},
		{"merge", func(out string) []string {
			return []string{"merge", "--compact", "--user-password", "s", "-o", out, src, src}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.pdf")
			var stdout, stderr bytes.Buffer
			if rc := run(ctx, tc.args(out), &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			if got := stderr.String(); strings.Contains(got, "does not apply") {
				t.Errorf("the command warned that compaction does not apply; stderr: %q", got)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("reading the output: %v", err)
			}
			if !bytes.Contains(data, []byte("/Type /ObjStm")) {
				t.Error("the encrypted output carries no object stream")
			}
			if _, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data))); err == nil {
				t.Fatal("the output opens without its password, so it is not encrypted")
			}
			r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)), pdf.WithPassword("s"))
			if err != nil {
				t.Fatalf("the output does not open with its password: %v", err)
			}
			defer r.Close()
			if n, err := r.PageCount(ctx); err != nil || n < 1 {
				t.Errorf("PageCount on the output: %d, %v", n, err)
			}
		})
	}
}
