package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
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
// This is not an exotic input. Every name in stageCmds is an ordinary word a
// caller may pass as a document title or a stamp: rotate, merge, delete,
// insert, attach and detach all read as plain English.
//
// The loop measures every name the map holds rather than a hand-picked few, so
// a stage added later is covered the day it is added. It carries no count on
// purpose: the set grew from thirteen to eighteen, and a number written here
// would have gone stale with nothing to catch it.
//
// The --flag=value form is immune, because the value never becomes a token of
// its own, which is what makes the failure so confusing to hit.
func TestPipe_AcceptsAStageNameAsAFlagValue(t *testing.T) {
	titles := make([]string, 0, len(stageCmds))
	for name := range stageCmds {
		titles = append(titles, name)
	}
	sort.Strings(titles)

	for _, title := range titles {
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

// TestPipe_HelpAsAFlagValueDoesNotSwallowTheNextStage is the -h analogue of the
// test above, and it is the ONLY place the h/help entries in boolStageFlags are
// observable.
//
// -h is a flag like any other to splitStageGroups: unregistered, expectsValue
// reports that it consumes the following token, so the `rotate` after it is
// read as -h's value and never starts a group. set-metadata's FlagSet then
// stops at the first non-flag and ignores the rest, so the rotation silently
// does not happen and the command still exits 0.
//
// The stage-help tests cannot cover this. A help request returns exitOK from
// the ErrHelp branch before the missing -o is ever examined, so an eaten -o
// produces no observable error there: measured, unregistering h/help leaves
// every one of those tests passing. This assertion is self-controlling
// instead. If the rotate stage is swallowed the rotation reads 0, not 90.
func TestPipe_HelpAsAFlagValueDoesNotSwallowTheNextStage(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "piped.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "set-metadata", "-title", "-h",
		"rotate", "-angle", "90", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe: exit %d, stderr: %s", rc, errOut)
	}
	if got := readMetadata(t, out)["Title"]; got != "-h" {
		t.Errorf("Title = %q, want %q: -h stopped being usable as a flag VALUE", got, "-h")
	}
	if got := pageRotations(t, out)[0]; got != "90" {
		t.Errorf("rotation = %q, want 90: the rotate stage after `-title -h` was swallowed, "+
			"so the pipeline ran one stage and reported success", got)
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

// TestPipe_FailedWriteLeavesNoOutputFile pins the cleanup contract that every
// other output-producing command in this package already honours.
//
// Eighteen commands call abandonOutput when WriteTo fails; runPipe called it
// zero times. createOutput has already created the file by then, so a stage
// that fails during the write left a partial document on disk next to a
// non-zero exit: the caller sees failure and still finds a file that looks like
// a result.
//
// Both cases below fail INSIDE pipeline.WriteTo rather than while the stage is
// being built, which is what puts them after createOutput. A stage that fails
// earlier, stamp-image with an unreadable -image for instance, never reaches
// that point and was always clean, which is why the defect hid.
func TestPipe_FailedWriteLeavesNoOutputFile(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
	}{
		{"field that does not exist", "name=Jane"},
		{"malformed assignment", "nameJane"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := writeTempPDF(t, untaggedTextPDF())
			out := filepath.Join(t.TempDir(), "piped.pdf")

			rc, errOut := runPipeArgs(t, []string{
				"pipe", src, "set-form", "-field", tc.field, "-o", out,
			})
			if rc == exitOK {
				t.Fatalf("pipe succeeded with -field %q on a document that has no form; stderr: %s",
					tc.field, errOut)
			}
			if _, err := os.Stat(out); err == nil {
				t.Errorf("%s exists: a failed pipe must not leave an output file", out)
			}
		})
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
