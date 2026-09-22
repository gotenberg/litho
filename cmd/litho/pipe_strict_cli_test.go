package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A remediate stage asserts the profile in the output whatever it could not
// repair, so a pipeline ending in one reported success for a document that
// does not conform, exactly as the standalone command did before --strict.
//
// pipe answers on the exit code alone. It prints nothing but the document, and
// `-o -` streams that document to stdout, so a report written there would
// corrupt the result. The stage's report was already discarded deliberately,
// but that decision answered the PRINTING question; the exit code is a
// separate channel and was never weighed.
//
// The fixture is the one remediation cannot fully repair: redactableCLIPDF
// paints text through a non-embedded Helvetica and litho ships no font
// program, so the embedding violation survives.

// runPipeCLI runs a pipe invocation and returns the exit code with whatever
// reached stdout and stderr.
func runPipeCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), args, &stdout, &stderr)
	return rc, stdout.String(), stderr.String()
}

// TestPipeStrict_RefusesADocumentARemediateStageCouldNotRepair carries its own
// precondition: the unflagged run must still exit 0, which both pins the
// default and proves the fixture reaches a stage that leaves residuals. An
// assertion on the strict run alone could pass against a flag wired to refuse
// unconditionally.
func TestPipeStrict_RefusesADocumentARemediateStageCouldNotRepair(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())

	plainOut := filepath.Join(t.TempDir(), "plain.pdf")
	if rc, _, stderr := runPipeCLI(t,
		"pipe", "-o", plainOut, src, "remediate", "-profile", "PDF/A-2b",
	); rc != exitOK {
		t.Fatalf("without --strict: exit %d, want %d; stderr: %s", rc, exitOK, stderr)
	}

	strictOut := filepath.Join(t.TempDir(), "strict.pdf")
	rc, stdout, stderr := runPipeCLI(t,
		"pipe", "--strict", "-o", strictOut, src, "remediate", "-profile", "PDF/A-2b")
	if rc != exitValidationFail {
		t.Errorf("with --strict: exit %d, want %d; stderr: %s", rc, exitValidationFail, stderr)
	}

	// The exit status is the verdict, not a refusal to produce the document.
	data, err := os.ReadFile(strictOut) //nolint:gosec // path is this test's own temp file
	if err != nil || len(data) == 0 {
		t.Errorf("--strict withheld the output file: %v", err)
	}
	if stdout != "" {
		t.Errorf("pipe wrote %q to stdout; -o - streams the document there, so it must stay silent", stdout)
	}
}

// TestPipeStrict_SilentWhenNoRemediateStageRan keeps the flag from failing a
// pipeline it has nothing to judge.
func TestPipeStrict_SilentWhenNoRemediateStageRan(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, _, stderr := runPipeCLI(t, "pipe", "--strict", "-o", out, src, "rotate", "-angle", "90")
	if rc != exitOK {
		t.Errorf("--strict with no remediate stage: exit %d, want %d; stderr: %s", rc, exitOK, stderr)
	}
}

// TestPipeStrict_ReadAsAGlobalAfterAStageFlagValue exercises the parsing rule
// the --compact arm documents. parsePipeGlobals scans every token, skipping
// those consumed as a stage flag's VALUE, so --strict written after
// "-profile PDF/A-2b" must still register as a global rather than falling
// through to the stage. Pipe's argument splitting has broken this way before.
func TestPipeStrict_ReadAsAGlobalAfterAStageFlagValue(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, _, stderr := runPipeCLI(t,
		"pipe", "-o", out, src, "remediate", "-profile", "PDF/A-2b", "--strict")
	if rc != exitValidationFail {
		t.Errorf("--strict after a stage flag value: exit %d, want %d; stderr: %s",
			rc, exitValidationFail, stderr)
	}
}

// TestPipeUsage_AdvertisesStrict: a flag the usage does not name is a flag
// nobody finds.
func TestPipeUsage_AdvertisesStrict(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pipeUsage(&buf)
	if !bytes.Contains(buf.Bytes(), []byte("--strict")) {
		t.Errorf("pipe usage does not mention --strict:\n%s", buf.String())
	}
}
