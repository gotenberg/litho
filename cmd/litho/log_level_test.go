package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Every diagnostic the library emits was discarded for every CLI run.
//
// pkg/internal/core/document substitutes a NopLogger when the caller supplies
// none, and cmd/litho supplied none: measured, a reader opened the way the CLI
// opens one reports Enabled=false even at Error. So the 466 doc.Logger call
// sites, the five Error sites, and the panic boundary that recover.go installs
// at 51 public entry points all wrote into a discard handler. CLAUDE.md
// requires that boundary to log the recovered value and a stack; a CLI user
// received neither, and had no flag to ask for them.
//
// --log-level wires pdf.WithLogger through openPDF, the single chokepoint the
// 38 open call sites already share. Default unchanged: absent the flag no
// logger is installed and the library stays silent.

// logLines counts the structured records a run wrote to stderr.
func logLines(stderr string) int {
	n := 0
	for line := range strings.SplitSeq(stderr, "\n") {
		if strings.Contains(line, "level=") {
			n++
		}
	}
	return n
}

// TestLogLevel_DefaultInstallsNoLogger pins the behaviour every existing
// caller depends on: without the flag, nothing changes.
func TestLogLevel_DefaultInstallsNoLogger(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"info", src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	if n := logLines(stderr.String()); n != 0 {
		t.Errorf("default run wrote %d diagnostic line(s) to stderr, want 0: %q", n, stderr.String())
	}
}

// TestLogLevel_DebugSurfacesLibraryDiagnostics is the other half: the flag has
// to actually reach the library, not merely be accepted.
//
// Measured on this fixture, a debug run emits ~24 records beginning with
// "xref parsed", so asserting on a positive count is safe here. The control
// above is what makes this meaningful: without it, a build that logged
// unconditionally would pass this test.
func TestLogLevel_DebugSurfacesLibraryDiagnostics(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())

	for _, tc := range []struct {
		desc string
		args []string
	}{
		{"space form", []string{"--log-level", "debug", "info", src}},
		{"equals form", []string{"--log-level=debug", "info", src}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), tc.args, &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			if n := logLines(stderr.String()); n == 0 {
				t.Errorf("debug run wrote no diagnostics: the logger never reached the library; stderr: %q",
					stderr.String())
			}
			if !strings.Contains(stderr.String(), "level=DEBUG") {
				t.Errorf("stderr carries no DEBUG record: %q", stderr.String())
			}
		})
	}
}

// TestLogLevel_DiagnosticsNeverReachStdout is the property that protects `-o -`.
//
// A command streaming a document to stdout must not have log records
// interleaved into the bytes. Measured: stdout is byte-identical with and
// without the flag.
func TestLogLevel_DiagnosticsNeverReachStdout(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())

	var quietOut, quietErr bytes.Buffer
	if rc := run(context.Background(), []string{"info", src}, &quietOut, &quietErr); rc != exitOK {
		t.Fatalf("quiet run: exit %d, stderr: %s", rc, quietErr.String())
	}
	var loudOut, loudErr bytes.Buffer
	if rc := run(context.Background(), []string{"--log-level", "debug", "info", src}, &loudOut, &loudErr); rc != exitOK {
		t.Fatalf("debug run: exit %d, stderr: %s", rc, loudErr.String())
	}

	if quietOut.String() != loudOut.String() {
		t.Errorf("stdout changed when diagnostics were enabled:\n quiet=%q\n debug=%q",
			quietOut.String(), loudOut.String())
	}
	if logLines(loudErr.String()) == 0 {
		t.Error("the debug run produced no diagnostics, so this test is not comparing anything")
	}
}

// TestLogLevel_FiltersByLevel pins that the level is honoured rather than
// treated as an on switch. A healthy document produces no Error records, so
// these levels stay quiet while debug does not.
func TestLogLevel_FiltersByLevel(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())

	for _, level := range []string{"off", "error", "warn"} {
		t.Run(level, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), []string{"--log-level", level, "info", src}, &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			if n := logLines(stderr.String()); n != 0 {
				t.Errorf("--log-level %s emitted %d record(s) on a healthy document, want 0: %q",
					level, n, stderr.String())
			}
		})
	}
}

// TestLogLevel_RejectsBadInput pins the two refusals. An unrecognised level
// must not be silently ignored, which is the defect the neighbouring
// unknown-global-flag guard exists to prevent.
func TestLogLevel_RejectsBadInput(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())

	for _, tc := range []struct {
		desc string
		args []string
		want string
	}{
		{"an unknown level", []string{"--log-level", "bogus", "info", src}, "invalid log level"},
		{"an unknown level, equals form", []string{"--log-level=bogus", "info", src}, "invalid log level"},
		{"no value at all", []string{"--log-level"}, "needs a level"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), tc.args, &stdout, &stderr)
			if rc == exitOK {
				t.Fatalf("%s was accepted; stderr: %s", tc.desc, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr does not explain the refusal, want %q: %q", tc.want, stderr.String())
			}
		})
	}
}

// TestLogLevel_IsAdvertised keeps the help honest. A global flag the usage
// never mentions is the same undocumented-surface defect as a stage the help
// omits.
func TestLogLevel_IsAdvertised(t *testing.T) {
	var usage bytes.Buffer
	printUsage(&usage)

	if !strings.Contains(usage.String(), "--log-level") {
		t.Error("printUsage does not mention --log-level")
	}
	if !strings.Contains(usage.String(), "[--log-level LEVEL]") {
		t.Error("the Usage line does not show the --log-level syntax")
	}
}
