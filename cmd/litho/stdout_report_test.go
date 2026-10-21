package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createOutput maps "-o -" and an empty path to stdout, so a command that also
// printed its report there wrote the report INTO the document. `remediate -o -`
// appended "Profile: ..." after %%EOF and `redact -o -` appended
// "Removed 0 text run(s), ...". litho reopens such a file without complaint,
// because trailing bytes after %%EOF are tolerated, so the corruption was
// silent: a caller capturing stdout received a document carrying text it never
// asked for.
//
// WHY `-o -` IS NOT DRIVEN END TO END HERE. createOutput returns os.Stdout
// itself, the real process stream, not the writer injected into run(), so a
// document written under `-o -` bypasses a test's buffer entirely. An attempt
// at it dumped a whole PDF into the `go test` log and then failed with "no
// document reached stdout", which is the harness telling the truth about its
// own reach. That is also why no other test in this package passes "-o", "-".
// So the predicate is pinned directly below, and the ordinary file path is
// pinned end to end; the `-o -` behaviour itself was verified against the
// built binary (a 497-byte document on stdout, the 45-byte report on stderr).
// Making createOutput take an injectable writer would close the gap, but that
// is ~18 call sites and a different change.

// TestReportWriter pins the predicate, which is where the fix actually lives.
func TestReportWriter(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer
	stdout, stderr := io.Writer(&out), io.Writer(&errOut)

	name := func(w io.Writer) string {
		if w == stderr {
			return "stderr"
		}
		return "stdout"
	}

	for _, tc := range []struct {
		desc string
		path string
		want io.Writer
	}{
		{"a dash means the document owns stdout", "-", stderr},
		{"an empty path means the document owns stdout too", "", stderr},
		{"an ordinary file leaves the report on stdout", "out.pdf", stdout},
		{"a name merely containing a dash is a file", "my-file.pdf", stdout},
		{"a relative path ending in a dash is a file", "./-", stdout},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			if got := reportWriter(tc.path, stdout, stderr); got != tc.want {
				t.Errorf("reportWriter(%q) returned %s, want %s", tc.path, name(got), name(tc.want))
			}
		})
	}
}

// TestReportWriter_MatchesCreateOutput states the coupling that keeps the fix
// correct: whatever createOutput sends to stdout, reportWriter must divert the
// report away from. The first assertion is the precondition, so this fails
// loudly if createOutput's own notion of "stdout" ever changes rather than
// silently testing nothing.
func TestReportWriter_MatchesCreateOutput(t *testing.T) {
	t.Parallel()
	var out, errOut bytes.Buffer

	for _, path := range []string{"", "-"} {
		w, _, err := createOutput(path)
		if err != nil {
			t.Fatalf("createOutput(%q): %v", path, err)
		}
		if w != io.Writer(os.Stdout) {
			t.Fatalf("createOutput(%q) no longer writes to stdout, so this coupling test is moot", path)
		}
		if got := reportWriter(path, &out, &errOut); got != io.Writer(&errOut) {
			t.Errorf("createOutput(%q) puts the document on stdout, but reportWriter keeps the report there", path)
		}
	}
}

// TestReportGoesToStdoutForAFileOutput is the control. The ordinary case must
// be untouched: a report on stdout is what every existing caller reads,
// including the one that parses it as JSON.
func TestReportGoesToStdoutForAFileOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   func(src, out string) []string
		expect string
	}{
		{"remediate", func(src, out string) []string {
			return []string{"remediate", "--profile", "PDF/A-2b", "-o", out, src}
		}, "Profile:"},
		{"redact", func(src, out string) []string {
			return []string{"redact", "--region", "1:0,0,1,1", "-o", out, src}
		}, "Removed "},
		{"convert-colors", func(src, out string) []string {
			return []string{"convert-colors", "--target", "cmyk", "-o", out, src}
		}, "Converted "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := writeTempPDF(t, redactableCLIPDF())
			out := filepath.Join(t.TempDir(), "out.pdf")

			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), tc.args(src, out), &stdout, &stderr)
			if rc != exitOK {
				t.Fatalf("exit %d; stderr: %s", rc, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.expect) {
				t.Errorf("a file output no longer reports on stdout; stdout: %q", stdout.String())
			}
			if strings.Contains(stderr.String(), tc.expect) {
				t.Errorf("the report moved to stderr for a FILE output; stderr: %q", stderr.String())
			}
		})
	}
}
