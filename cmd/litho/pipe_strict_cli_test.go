package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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

// TestPipeUsage_AdvertisesEveryStage applies that rule to the larger claim on
// the same screen, and in both directions.
//
// pipeUsage prints a "Stages:" list and nothing compared it to stageCmds, so
// the two drift silently. A stage the help omits is one nobody finds. A name
// the help invents sends a caller to "unknown stage" for a surface litho
// advertises. `e4f99d9f` grew the set from thirteen to eighteen and both lists
// had to be edited by hand; only a check makes that safe.
//
// Measured when written: 18 names each way, symmetric difference empty. This
// is protection rather than a defect found.
func TestPipeUsage_AdvertisesEveryStage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pipeUsage(&buf)

	// The list heads a line and continues on indented ones. Stopping at the
	// first unindented line keeps a section added later out of the stage set.
	advertised := map[string]bool{}
	inList := false
	for line := range strings.SplitSeq(buf.String(), "\n") {
		rest, isHead := strings.CutPrefix(line, "Stages:")
		switch {
		case isHead:
			inList, line = true, rest
		case inList && strings.HasPrefix(line, " "):
		default:
			inList = false
			continue
		}
		for _, name := range strings.Split(line, ",") {
			if name = strings.TrimSpace(name); name != "" {
				advertised[name] = true
			}
		}
	}

	// The control. A parser that matched nothing would report every stage
	// missing below, which reads like a real failure; naming two proves the
	// list was actually read.
	for _, name := range []string{"rotate", "remediate"} {
		if !advertised[name] {
			t.Fatalf("parsed no %q out of the usage, so the comparison below means nothing:\n%s",
				name, buf.String())
		}
	}

	for name := range stageCmds {
		if !advertised[name] {
			t.Errorf("stage %q dispatches but the usage never names it: a stage nobody finds", name)
		}
	}
	for name := range advertised {
		if !stageCmds[name] {
			t.Errorf("usage advertises %q but no stage answers it: a caller following the help "+
				"gets \"unknown stage\"", name)
		}
	}
}
