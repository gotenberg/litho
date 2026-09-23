package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// pipe performed the confidentiality downgrade the standalone page operations
// report, and said nothing.
//
// `80cb8803` wired warnEncryptionDropped into split, merge, delete, insert and
// rotate, the five operations measured to drop encryption. Every one of them is
// also a pipe stage, and pipe called the warning zero times: measured before the
// fix, `litho delete -p pw` warned and `litho pipe -p pw ... delete` produced
// the identical plaintext output silently, for all five.
//
// The default is NOT changed here, exactly as it was not changed there:
// encrypted in, plaintext out remains deliberate. Only the silence is closed.
//
// These tests match [encryptionWarning], the same constant the standalone
// warning tests use, so a reworded message updates in one place.

// TestPipe_WarnsWhenAPageOperationDropsEncryption covers all five stages whose
// standalone command warns, including the two that open a second document,
// where the encrypted input is the SOURCE rather than the piped file.
func TestPipe_WarnsWhenAPageOperationDropsEncryption(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		build func(enc, plain, out string) []string
	}{
		{"delete", func(enc, _, out string) []string {
			return []string{"pipe", "-p", "s3cret", enc, "delete", "-pages", "1", "-o", out}
		}},
		{"rotate", func(enc, _, out string) []string {
			return []string{"pipe", "-p", "s3cret", enc, "rotate", "-angle", "90", "-o", out}
		}},
		{"split", func(enc, _, out string) []string {
			return []string{"pipe", "-p", "s3cret", enc, "split", "-pages", "1", "-o", out}
		}},
		{"merge, only the source is encrypted", func(enc, plain, out string) []string {
			return []string{"pipe", "-p", "s3cret", plain, "merge", enc, "-o", out}
		}},
		{"insert, only the source is encrypted", func(enc, plain, out string) []string {
			return []string{"pipe", "-p", "s3cret", plain, "insert", "-source", enc, "-pos", "0", "-o", out}
		}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			enc := encryptTempPDF(t, twoPageTempPDF(t), "s3cret")
			plain := onePageTempPDF(t)
			out := filepath.Join(t.TempDir(), "out.pdf")

			rc, errOut := runPipeArgs(t, tc.build(enc, plain, out))
			if rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, errOut)
			}
			if !strings.Contains(errOut, encryptionWarning) {
				t.Errorf("no encryption warning: pipe dropped the input's protection silently, "+
					"while the standalone command reports it; stderr: %q", errOut)
			}
			// Without this, the assertion above would still pass if the output
			// kept its protection, in which case there was nothing to warn
			// about and the test would not be measuring the downgrade.
			if !opensWithoutAPassword(t, out) {
				t.Error("the output is still encrypted, so no downgrade happened here")
			}
		})
	}
}

// TestPipe_NoWarningWhenAnEncryptStageProtectsTheOutput is the control that
// stops the warning from being unconditional.
//
// Without it, a version that printed the warning on every run would pass the
// test above. An encrypt stage means the output carries protection, so warning
// would be false.
func TestPipe_NoWarningWhenAnEncryptStageProtectsTheOutput(t *testing.T) {
	enc := encryptTempPDF(t, twoPageTempPDF(t), "s3cret")
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", "-p", "s3cret", enc, "delete", "-pages", "1",
		"encrypt", "-user-password", "x", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, errOut)
	}
	if strings.Contains(errOut, encryptionWarning) {
		t.Errorf("warned although an encrypt stage protects the output; stderr: %q", errOut)
	}
	if opensWithoutAPassword(t, out) {
		t.Error("the output is not encrypted, so the absence of a warning is wrong " +
			"and this control proves nothing")
	}
}

// TestPipe_NoWarningForAPlainInput is the second control: nothing was lost, so
// nothing is reported.
func TestPipe_NoWarningForAPlainInput(t *testing.T) {
	plain := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", plain, "rotate", "-angle", "90", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, errOut)
	}
	if strings.Contains(errOut, encryptionWarning) {
		t.Errorf("warned about a plain input; stderr: %q", errOut)
	}
}

// TestPipe_SilentForStagesWhoseStandaloneCommandIsSilent pins the OTHER half of
// parity, and it is the reason pageOpStages exists rather than a blanket check.
//
// autotag, set-metadata and flatten-form drop encryption too (measured: an
// autotag output opens with no password), and their standalone commands say
// nothing, because `80cb8803` scoped the warning to page operations. Warning
// for them here would fix one entry-point divergence by creating its mirror
// image, so this test fails if that happens.
func TestPipe_SilentForStagesWhoseStandaloneCommandIsSilent(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		stage []string
	}{
		{"autotag", []string{"autotag"}},
		{"set-metadata", []string{"set-metadata", "-title", "T"}},
		{"flatten-form", []string{"flatten-form"}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			enc := encryptTempPDF(t, twoPageTempPDF(t), "s3cret")
			out := filepath.Join(t.TempDir(), "out.pdf")

			args := append([]string{"pipe", "-p", "s3cret", enc}, tc.stage...)
			args = append(args, "-o", out)

			rc, errOut := runPipeArgs(t, args)
			if rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, errOut)
			}
			if strings.Contains(errOut, encryptionWarning) {
				t.Errorf("pipe warned for %s, but the standalone command does not: "+
					"parity now diverges in the opposite direction; stderr: %q", tc.desc, errOut)
			}
		})
	}
}
