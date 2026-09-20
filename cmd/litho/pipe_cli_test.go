package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runPipeArgs runs the pipe command and returns its exit code and stderr.
func runPipeArgs(t *testing.T, args []string) (int, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), args, &stdout, &stderr)
	return rc, stderr.String()
}

// TestPipe_RunsTwoStagesInOrder is the positive control for stage splitting.
//
// Measured before this test existed: set-metadata followed by rotate writes the
// title AND rotates the page, so a failure here means splitting broke outright
// rather than in the narrow case the tests below cover.
func TestPipe_RunsTwoStagesInOrder(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "piped.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "set-metadata", "-title", "Hello", "rotate", "-angle", "90", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe: exit %d, stderr: %s", rc, errOut)
	}

	if got := readMetadata(t, out)["Title"]; got != "Hello" {
		t.Errorf("Title = %q, want %q: the set-metadata stage did not run", got, "Hello")
	}
}

// TestPipe_AcceptsAStageNameAsAFlagValue pins the defect this file was written
// for.
//
// splitStageGroups asks isStageCmd about every token with no notion of what a
// flag consumes, so a --title whose VALUE happens to be a stage name is split
// off into a group of its own. The stage is then left holding a flag with no
// argument and the whole command fails:
//
//	flag needs an argument: -title
//	pipe stage "set-metadata": flag needs an argument: -title
//
// This is not an exotic input. Measured against every name in stageCmds, all
// thirteen break: rotate, split, merge, delete, encrypt, decrypt, autotag,
// remediate, set-metadata, set-form, flatten-form, stamp-text and stamp-image
// are all ordinary words for a document title or a stamp.
//
// The --flag=value form is immune, because the value never becomes a token of
// its own, which is what makes the failure so confusing to hit.
func TestPipe_AcceptsAStageNameAsAFlagValue(t *testing.T) {
	for _, title := range []string{"rotate", "merge", "delete", "stamp-text"} {
		src := writeTempPDF(t, untaggedTextPDF())
		out := filepath.Join(t.TempDir(), "piped.pdf")

		rc, errOut := runPipeArgs(t, []string{
			"pipe", src, "set-metadata", "-title", title, "-o", out,
		})
		if rc != exitOK {
			t.Errorf("-title %q: exit %d, want %d; stderr: %s", title, rc, exitOK, strings.TrimSpace(errOut))
			continue
		}
		if got := readMetadata(t, out)["Title"]; got != title {
			t.Errorf("-title %q: Title = %q, want %q", title, got, title)
		}
	}
}

// TestPipe_EqualsFormAlreadyWorks pins the half that is not broken, so a fix
// for the case above cannot regress it.
func TestPipe_EqualsFormAlreadyWorks(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "piped.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "set-metadata", "-title=rotate", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe: exit %d, stderr: %s", rc, errOut)
	}
	if got := readMetadata(t, out)["Title"]; got != "rotate" {
		t.Errorf("Title = %q, want %q", got, "rotate")
	}
}

// TestPipe_BooleanStageFlagDoesNotSwallowTheNextStage is the regression guard
// for the fix, and the reason the fix cannot be a blanket "the token after a
// flag is its value" rule.
//
// -aggressive is the only boolean among the stage flags, so it consumes
// nothing: the token after it is a genuine stage and must still start a new
// group. Measured working before the fix, and it has to stay that way.
func TestPipe_BooleanStageFlagDoesNotSwallowTheNextStage(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "piped.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "remediate", "-profile", "PDF/A-2b", "-aggressive",
		"rotate", "-angle", "90", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe: exit %d, stderr: %s", rc, errOut)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("no output file: %v", err)
	}
}

// TestPipe_WritesToTheOutputPathItWasGiven pins the sibling half of the
// splitting defect, in parsePipeGlobals rather than splitStageGroups.
//
// parsePipeGlobals scans every token claiming -o and -p, with no notion of what
// a flag consumes. A stage flag whose VALUE is "-o" was therefore read as the
// global output flag, the token after it was taken as the filename, and the
// real -o fell through to the stage. Measured: the command exited 0 while
// writing a file named "-o" into the working directory and setting the title to
// a filesystem path.
//
// That is worse than the splitStageGroups defect, which at least failed loudly.
// A caller is unlikely to pass "-o" as a title, so this is about the failure
// MODE rather than the likelihood: exiting 0 having written the wrong file in
// the wrong place is the one outcome a CLI must never produce.
//
// The fix cannot be a look-behind ("was the previous token a flag?"), because
// the legitimate -o here also follows one. It needs the token consumed as a
// value to be marked consumed, so the next token is examined normally.
func TestPipe_WritesToTheOutputPathItWasGiven(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	out := filepath.Join(dir, "piped.pdf")

	// The defect writes a file named for the flag into the WORKING directory,
	// which under go test is the package source tree. Run from a temp directory
	// so a failing assertion cannot leave one behind: cleanup at the end of the
	// test is not enough, because the first assertion below stops the test.
	t.Chdir(t.TempDir())

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "set-metadata", "-title", "-o", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe: exit %d, stderr: %s", rc, errOut)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("nothing written to the -o path: %v", err)
	}
	if got := readMetadata(t, out)["Title"]; got != "-o" {
		t.Errorf("Title = %q, want %q: the flag value was not passed through", got, "-o")
	}
	if _, err := os.Stat("-o"); err == nil {
		t.Error(`a file named "-o" was created in the working directory`)
		_ = os.Remove("-o")
	}
}

// TestPipe_UnknownStageIsAnError pins the default arm of addStage.
func TestPipe_UnknownStageIsAnError(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "piped.pdf")

	rc, errOut := runPipeArgs(t, []string{"pipe", src, "frobnicate", "-o", out})
	if rc == exitOK {
		t.Fatalf("pipe accepted an unknown stage; stderr: %s", errOut)
	}
	if !strings.Contains(errOut, "unknown stage") {
		t.Errorf("stderr = %q, want it to name the unknown stage", strings.TrimSpace(errOut))
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed pipe must not leave an output file", out)
	}
}

// TestPipe_MissingOutputIsAUsageError pins that -o is required.
func TestPipe_MissingOutputIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())

	rc, errOut := runPipeArgs(t, []string{"pipe", src, "autotag"})
	if rc == exitOK {
		t.Fatal("pipe accepted a missing -o")
	}
	if !strings.Contains(errOut, "-o") {
		t.Errorf("stderr = %q, want it to name the missing -o flag", strings.TrimSpace(errOut))
	}
}
