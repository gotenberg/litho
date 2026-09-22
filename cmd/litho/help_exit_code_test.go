package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommands_HelpRequestExitsOK pins that asking a command for its flags is a
// request, not a failure.
//
// Every command builds its FlagSet with [flag.ContinueOnError] and used to
// write
//
//	if err := fs.Parse(reorderArgs(fs, args)); err != nil { return exitError }
//
// which lumps [flag.ErrHelp] in with a genuine parse failure. MEASURED before
// the fix: `litho info -h` printed its usage and exited 1 while `litho --help`
// exited 0, so one binary disagreed with itself about the same request. The
// flag package draws the distinction itself, since [flag.ExitOnError] exits 0
// on ErrHelp and 2 on anything else.
func TestCommands_HelpRequestExitsOK(t *testing.T) {
	for _, cmd := range []string{
		"info", "metadata", "pages", "text", "validate",
		"rotate", "delete", "split", "merge", "encrypt",
		"attach", "detach", "remediate", "stamp-text",
	} {
		for _, spelling := range []string{"-h", "--help", "-help"} {
			t.Run(cmd+" "+spelling, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				rc := run(context.Background(), []string{cmd, spelling}, &stdout, &stderr)
				if rc != exitOK {
					t.Errorf("exit %d, want %d: a help request is not a failure (stderr: %q)",
						rc, exitOK, strings.TrimSpace(stderr.String()))
				}
				// The usage still has to be printed, or the exit code would be
				// the only thing this pins and a silent success would pass.
				if stderr.Len() == 0 {
					t.Errorf("stderr is empty: the help request printed no usage")
				}
			})
		}
	}
}

// TestCommands_BadFlagStillFails is the control for the test above. Mapping
// ErrHelp to success must not turn every parse failure into one, which an
// exit-code-only assertion on -h could not detect.
func TestCommands_BadFlagStillFails(t *testing.T) {
	src := writeTempPDF(t, minimalPDF())

	for _, args := range [][]string{
		{"info", "-bogus", src},
		{"metadata", "-bogus", src},
		{"rotate", "-bogus", src},
		{"split", "-bogus", src},
	} {
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitError {
			t.Errorf("run(%q) = %d, want %d: an unknown flag is still an error",
				args, rc, exitError)
		}
	}
}

// TestPipe_HelpRequestExitsOK covers the one command that owns no FlagSet.
//
// pipe prints its usage by hand, so parseCommandFlags never runs for it and
// `litho pipe -h` fell into the "too few arguments" arm and exited 1 even
// though it printed exactly the text the user asked for.
func TestPipe_HelpRequestExitsOK(t *testing.T) {
	for _, spelling := range []string{"-h", "--help", "-help"} {
		t.Run(spelling, func(t *testing.T) {
			rc, errOut := runPipeArgs(t, []string{"pipe", spelling})
			if rc != exitOK {
				t.Errorf("exit %d, want %d; stderr: %s", rc, exitOK, strings.TrimSpace(errOut))
			}
			if !strings.Contains(errOut, "Usage: litho pipe") {
				t.Errorf("stderr = %q, want the pipe usage", errOut)
			}
		})
	}
}

// TestPipe_TooFewArgsStillFails is the control: only a help request earns a
// success exit, an incomplete invocation still does not.
func TestPipe_TooFewArgsStillFails(t *testing.T) {
	src := writeTempPDF(t, minimalPDF())

	for _, args := range [][]string{
		{"pipe"},
		{"pipe", src},
	} {
		rc, errOut := runPipeArgs(t, args)
		if rc != exitError {
			t.Errorf("run(%q) = %d, want %d; stderr: %s", args, rc, exitError, strings.TrimSpace(errOut))
		}
	}
}

// TestPipe_StageHelpExitsOK pins the same contract one level down. A stage's
// flags are parsed inside addStage, whose error travels up to runPipe, so the
// help request used to arrive wrapped as `pipe stage "rotate": flag: help
// requested` and exited 1.
func TestPipe_StageHelpExitsOK(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "piped.pdf")

	rc, errOut := runPipeArgs(t, []string{"pipe", src, "-o", out, "rotate", "-h"})
	if rc != exitOK {
		t.Errorf("exit %d, want %d; stderr: %s", rc, exitOK, errOut)
	}
	if !strings.Contains(errOut, "Usage of rotate:") {
		t.Errorf("stderr = %q, want the stage usage", errOut)
	}
	if strings.Contains(errOut, "pipe stage") {
		t.Errorf("stderr = %q, want no error wrapper around a help request", errOut)
	}
}
