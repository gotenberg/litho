package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestPipe_StageFlagErrorReachesTheInjectedWriter pins that a pipe stage's own
// FlagSet output goes to the writer [run] was handed.
//
// addStage was the only flag-parsing function in this package to receive no
// writer, so its ten FlagSets kept the flag package's default of the process's
// os.Stderr while [run] documents the opposite: "all output goes to the
// stdout/stderr writers passed by the caller". MEASURED before the fix, the
// captured buffer held only runPipe's own wrapping line,
//
//	pipe stage "rotate": flag provided but not defined: -bogus
//
// while the bare message and the entire usage block were printed past it. That
// also explains the doubled output transcribed on
// TestPipe_AcceptsAStageNameAsAFlagValue: the first line escaped the writer,
// the second was captured.
//
// ARGUMENT ORDER MATTERS. -o has to precede the stage. parsePipeGlobals treats
// any unrecognised dash-token as value-taking (expectsValue), so a stage flag
// placed before -o swallows it and the run dies on the missing output file
// without ever reaching addStage. A probe written the other way round measured
// nothing at all.
func TestPipe_StageFlagErrorReachesTheInjectedWriter(t *testing.T) {
	// Every stage that builds a FlagSet. decrypt, flatten-form and autotag take
	// no flags and build none, so they have no output to route.
	for _, stage := range []string{
		"rotate", "split", "merge", "delete", "encrypt",
		"set-metadata", "set-form", "stamp-text", "stamp-image", "remediate",
	} {
		t.Run(stage, func(t *testing.T) {
			src := writeTempPDF(t, untaggedTextPDF())
			out := filepath.Join(t.TempDir(), "piped.pdf")

			rc, errOut := runPipeArgs(t, []string{
				"pipe", src, "-o", out, stage, "-bogus",
			})
			if rc != exitError {
				t.Fatalf("exit %d, want %d: an unknown stage flag must fail", rc, exitError)
			}
			if !strings.Contains(errOut, "not defined: -bogus") {
				t.Errorf("stderr = %q, want the flag error itself rather than only the wrapper", errOut)
			}
			if want := "Usage of " + stage + ":"; !strings.Contains(errOut, want) {
				t.Errorf("stderr = %q, want %q: the FlagSet usage went to the process stderr", errOut, want)
			}
		})
	}
}

// TestPipe_ValidStageWritesNothingToStderr is the control for the test above,
// which would otherwise pass on a build that dumped usage text unconditionally.
func TestPipe_ValidStageWritesNothingToStderr(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "piped.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "-o", out, "rotate", "-angle", "90",
	})
	if rc != exitOK {
		t.Fatalf("exit %d, want %d; stderr: %s", rc, exitOK, errOut)
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want empty on a pipe that succeeds", errOut)
	}
}
