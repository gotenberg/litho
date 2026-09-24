package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// pipe performed the confidentiality downgrade the standalone page operations
// report, and said nothing.
//
// `f2a37513` wired warnEncryptionDropped into split, merge, delete, insert and
// rotate, the five operations measured to drop encryption. Every one of them is
// also a pipe stage, and pipe called the warning zero times: measured before the
// fix, `litho delete -p pw` warned and `litho pipe -p pw ... delete` produced
// the identical plaintext output silently, for all five.
//
// The default is NOT changed here, exactly as it was not changed there:
// encrypted in, plaintext out remains deliberate. Only the silence is closed.
//
// These tests match encryptionWarning, the same constant the standalone
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

// TestPipe_WarnsForEveryStageWhoseStandaloneCommandWarns covers the ten stages
// that regressed, and it replaces a test that had inverted into a lock on the
// bug.
//
// Its predecessor asserted pipe stays SILENT for autotag, set-metadata and
// flatten-form, which was correct when written: `b59fc758` matched pipe to the
// five page operations, the only commands warning at the time. `bbed371f` then
// gave every other output-producing command the warning without widening
// pipe's allow list, so those three (and seven more) warned standalone and
// stayed silent through pipe. The old test did not fail. It defended the
// divergence, because its premise had changed underneath it.
//
// Measured before the fix, all ten: standalone emits the warning, pipe does
// not. A test written against a true premise can become a defender of the
// defect when the premise moves, which is why this one asserts PARITY with the
// standalone command rather than a hardcoded expectation per stage.
func TestPipe_WarnsForEveryStageWhoseStandaloneCommandWarns(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		stage func(f encFixtures) []string
	}{
		{"set-metadata", func(encFixtures) []string { return []string{"set-metadata", "-title", "T"} }},
		{"stamp-text", func(encFixtures) []string { return []string{"stamp-text", "-text", "DRAFT"} }},
		{"attach", func(f encFixtures) []string { return []string{"attach", "-name", "table.csv", "-data", f.data} }},
		{"detach", func(encFixtures) []string { return []string{"detach", "-name", "absent.csv"} }},
		{"set-bookmarks", func(encFixtures) []string { return []string{"set-bookmarks", "-generate"} }},
		{"autotag", func(encFixtures) []string { return []string{"autotag"} }},
		{"flatten-form", func(encFixtures) []string { return []string{"flatten-form"} }},
		{"remediate", func(encFixtures) []string { return []string{"remediate", "-profile", "PDF/A-2b"} }},
		{"stamp-image", func(f encFixtures) []string { return []string{"stamp-image", "-image", f.png} }},
		{"stamp-pdf", func(f encFixtures) []string { return []string{"stamp-pdf", "-stamp", f.stampPDF} }},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			f := newEncFixtures(t)
			out := filepath.Join(t.TempDir(), "out.pdf")

			args := append([]string{"pipe", "-p", "s3cret", f.doc}, tc.stage(f)...)
			args = append(args, "-o", out)

			rc, errOut := runPipeArgs(t, args)
			if rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, errOut)
			}
			if !strings.Contains(errOut, encryptionWarning) {
				t.Errorf("pipe %s dropped the input's protection silently while the standalone "+
					"command reports it; stderr: %q", tc.desc, errOut)
			}
			if !opensWithoutAPassword(t, out) {
				t.Error("the output is still encrypted, so no downgrade happened here")
			}
		})
	}
}

// TestPipe_WarnsWhenOnlyTheStampStageSourceIsEncrypted pins the pipe half of
// the stamp-password change, and it is the only place the stage's
// `*opened = append(*opened, stampReader)` is observable.
//
// The piped document is PLAINTEXT and the stamp is encrypted, so runPipe's
// warning can fire only if the stamp reader reached stageReaders. A stage that
// opened the stamp correctly but never handed the reader back would produce an
// unprotected document in silence, which is the same shape as the merge
// closers that leaked before `c433b788`.
func TestPipe_WarnsWhenOnlyTheStampStageSourceIsEncrypted(t *testing.T) {
	plain := writeTempPDF(t, untaggedTextPDF())
	stamp := encryptTempPDF(t, writeTempPDF(t, untaggedTextPDF()), "s3cret")
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", "-p", "s3cret", plain, "stamp-pdf", "-stamp", stamp, "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, errOut)
	}
	if !strings.Contains(errOut, encryptionWarning) {
		t.Errorf("the stamp's protection was dropped silently: the stage opened it but never "+
			"handed the reader to runPipe; stderr: %q", errOut)
	}
	if !opensWithoutAPassword(t, out) {
		t.Error("the output is encrypted, so nothing was dropped and this row proves nothing")
	}
}

// TestPipe_DecryptStageStaysSilent is the control that keeps the deny list
// honest, and the reason silentStages exists rather than warning on every
// stage.
//
// decrypt is the one command whose standalone form deliberately says nothing,
// because producing an unprotected document is what it was asked for. Warning
// here would report the requested outcome as a loss, which is the mirror-image
// divergence.
func TestPipe_DecryptStageStaysSilent(t *testing.T) {
	enc := encryptTempPDF(t, twoPageTempPDF(t), "s3cret")
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, errOut := runPipeArgs(t, []string{"pipe", "-p", "s3cret", enc, "decrypt", "-o", out})
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, errOut)
	}
	if strings.Contains(errOut, encryptionWarning) {
		t.Errorf("the decrypt stage warned about the downgrade it was asked to perform; stderr: %q", errOut)
	}
	if !opensWithoutAPassword(t, out) {
		t.Error("decrypt did not remove the protection, so this control proves nothing")
	}
}
