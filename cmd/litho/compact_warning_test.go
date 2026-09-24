package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The compaction warning and the encryption warning both answer "is the output
// encrypted", and both once read the same writeOpts slice. Adding compaction
// options to that slice made its length stop answering the question: --compact
// alone made it non-empty, which suppressed the encryption warning entirely and
// fired the compaction one on a file that compacted fine.
//
// Nothing caught it. The full suite and the linter passed, because no test
// asserted that the two warnings coexist. These do.

const (
	warnEncrypted = "input is encrypted"
	warnCompact   = "compact does not apply"
)

// TestCompactWarning_DoesNotSuppressTheEncryptionWarning is the regression.
//
// An encrypted input written to a plaintext output must still say so, and
// asking for compaction must not silence it. That warning is the one that
// matters more: it tells the caller their protected document just became an
// unprotected one.
//
// # Corpus evidence
//
//   - All corpora: the corpus gate passed while --compact suppressed the
//     encryption warning, because no test asserted that the two warnings
//     coexist.
func TestCompactWarning_DoesNotSuppressTheEncryptionWarning(t *testing.T) {
	enc := encryptTempPDF(t, onePageTempPDF(t), "secret")
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"split", "-pages", "1", "-p", "secret", "--compact", "-o", out, enc,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("split: exit %d, stderr: %s", rc, stderr.String())
	}

	got := stderr.String()
	if !strings.Contains(got, warnEncrypted) {
		t.Errorf("--compact suppressed the encryption warning; stderr was: %q", got)
	}
	// The output is NOT encrypted here, so compaction ran and its warning
	// must not appear.
	if strings.Contains(got, warnCompact) {
		t.Errorf("compaction warning fired on an unencrypted output; stderr was: %q", got)
	}
}

// TestCompactWarning_FiresWhenTheOutputIsEncrypted covers the silence this
// warning exists to end: the engine refuses to compact an encrypted document,
// and before this the caller was told nothing.
func TestCompactWarning_FiresWhenTheOutputIsEncrypted(t *testing.T) {
	src := onePageTempPDF(t)

	for _, tc := range []struct {
		name string
		args func(out string) []string
	}{
		{"encrypt", func(out string) []string {
			return []string{"encrypt", "--compact", "--user-password", "s", "-o", out, src}
		}},
		{"split", func(out string) []string {
			return []string{"split", "-pages", "1", "--compact", "--user-password", "s", "-o", out, src}
		}},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), tc.args(out), &stdout, &stderr); rc != exitOK {
			t.Fatalf("%s: exit %d, stderr: %s", tc.name, rc, stderr.String())
		}
		if !strings.Contains(stderr.String(), warnCompact) {
			t.Errorf("%s --compact with an encrypted output said nothing; stderr: %q",
				tc.name, stderr.String())
		}
	}
}

// TestCompactWarning_StaysQuietWhenCompactionHappens is the control that
// caught the conflation in the first place. A warning that fires on a
// successful --compact is worse than the silence it replaced, because it
// teaches the caller to ignore it.
func TestCompactWarning_StaysQuietWhenCompactionHappens(t *testing.T) {
	src := writeTempPDF(t, compactFixturePDF())

	for _, tc := range []struct {
		name string
		args func(out string) []string
	}{
		{"split", func(out string) []string {
			return []string{"split", "-pages", "1", "--compact", "-o", out, src}
		}},
		{"delete", func(out string) []string {
			return []string{"delete", "-pages", "1", "--compact", "-o", out, src}
		}},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), tc.args(out), &stdout, &stderr); rc != exitOK {
			t.Fatalf("%s: exit %d, stderr: %s", tc.name, rc, stderr.String())
		}
		if strings.Contains(stderr.String(), warnCompact) {
			t.Errorf("%s --compact warned on an output it did compact; stderr: %q",
				tc.name, stderr.String())
		}
	}
}

// TestCompactWarning_SilentWithoutTheFlag: a caller who never asked for
// compaction must not be told about it, however the output is written.
func TestCompactWarning_SilentWithoutTheFlag(t *testing.T) {
	src := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"encrypt", "--user-password", "s", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("encrypt: exit %d, stderr: %s", rc, stderr.String())
	}
	if strings.Contains(stderr.String(), warnCompact) {
		t.Errorf("compaction warning appeared without --compact; stderr: %q", stderr.String())
	}
}
