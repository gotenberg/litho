package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalPDF returns a handcrafted minimal 1-page PDF with an Info
// dict. Used so the CLI tests don't need a binary test fixture checked
// into the repo.
func minimalPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"4 0 obj\n<< /Title (CLI Test Doc) /Author (litho) >>\nendobj\n" +
			"xref\n0 5\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"0000000198 00000 n \r\n" +
			"trailer\n<< /Size 5 /Root 1 0 R /Info 4 0 R >>\n" +
			"startxref\n256\n%%EOF\n")
}

func writeTempPDF(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pdf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp PDF: %v", err)
	}
	return path
}

func TestRun_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), nil, &stdout, &stderr)
	if rc != exitError {
		t.Errorf("exit code: got %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("expected usage on stderr, got: %q", stderr.String())
	}
}

func TestRun_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"--help"}, &stdout, &stderr)
	if rc != exitOK {
		t.Errorf("exit code: got %d, want %d", rc, exitOK)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("expected usage on stdout, got: %q", stdout.String())
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"bogus"}, &stdout, &stderr)
	if rc != exitError {
		t.Errorf("exit code: got %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("expected unknown command error, got: %q", stderr.String())
	}
}

// TestRun_GlobalTimeout pins the "litho --timeout D <command>" syntax that
// printUsage advertises. The global loop advanced by one argument per
// iteration while --timeout has to consume two, so the duration landed in
// the command slot: every invocation in litho's own synopsis exited 1 with
// "unknown command: 30s". The "=" spelling matched no case at all and was
// dropped in silence, which is why an unparseable value passed there too.
func TestRun_GlobalTimeout(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())

	for _, tt := range []struct {
		name string
		args []string
		want int
	}{
		{"space form", []string{"--timeout", "30s", "info", path}, exitOK},
		{"equals form", []string{"--timeout=30s", "info", path}, exitOK},
		{"space form rejects a bad duration", []string{"--timeout", "bogus", "info", path}, exitError},
		{"equals form rejects a bad duration", []string{"--timeout=bogus", "info", path}, exitError},
		{"negative is not silently ignored", []string{"--timeout", "-5s", "info", path}, exitError},
		{"missing value", []string{"--timeout"}, exitError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), tt.args, &stdout, &stderr)
			if rc != tt.want {
				t.Fatalf("run(%q) = %d, want %d. stdout: %q stderr: %q",
					tt.args, rc, tt.want, stdout.String(), stderr.String())
			}
		})
	}
}

// TestRun_GlobalTimeoutReachesTheCommand is the control for
// TestRun_GlobalTimeout. An exit code alone passes even when the flag is
// parsed and then discarded, which is exactly what the "=" spelling did, so
// this asserts the command behind the flag actually produced its output.
func TestRun_GlobalTimeoutReachesTheCommand(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"--timeout", "30s", "info", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("run = %d, want %d. stderr: %q", rc, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Pages:       1") {
		t.Errorf("info did not run behind the flag, got: %q", stdout.String())
	}
}

// TestRun_UnknownGlobalFlag pins that a flag the loop does not recognise is
// an error. Such a flag used to match no case, fall out of the switch, and
// let the command run as if it had not been typed, so a misspelled --timout
// silently produced no deadline.
func TestRun_UnknownGlobalFlag(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())

	for _, args := range [][]string{
		{"--bogus", "info", path},
		{"-x", "info", path},
		{"--timout", "30s", "info", path},
	} {
		var stdout, stderr bytes.Buffer
		rc := run(context.Background(), args, &stdout, &stderr)
		if rc != exitError {
			t.Errorf("run(%q) = %d, want %d. stdout: %q", args, rc, exitError, stdout.String())
		}
		if !strings.Contains(stderr.String(), "unknown global flag") {
			t.Errorf("run(%q) stderr = %q, want it to name the flag", args, stderr.String())
		}
	}
}

func TestInfo_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"info", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Errorf("exit code: got %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Version:") {
		t.Errorf("expected Version in output, got: %s", out)
	}
	if !strings.Contains(out, "Pages:       1") {
		t.Errorf("expected 1 page, got: %s", out)
	}
}

func TestInfo_JSON(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"info", "-f", "json", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Errorf("exit code: got %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"pages": 1`) {
		t.Errorf("expected JSON pages field, got: %s", stdout.String())
	}
}

func TestMetadata_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"metadata", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Errorf("exit code: got %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Title: CLI Test Doc") {
		t.Errorf("expected title, got: %s", stdout.String())
	}
}

func TestPages_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"pages", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Errorf("exit code: got %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "612") {
		t.Errorf("expected width 612, got: %s", stdout.String())
	}
}

func TestText_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"text", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Errorf("exit code: got %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
}

func TestText_MissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"text", "/nonexistent/file.pdf"}, &stdout, &stderr)
	if rc != exitError {
		t.Errorf("exit code: got %d, want %d", rc, exitError)
	}
}

func TestParsePages(t *testing.T) {
	tests := []struct {
		spec      string
		pageCount int
		want      []int
		wantErr   bool
	}{
		{"1", 5, []int{1}, false},
		{"1-3", 5, []int{1, 2, 3}, false},
		{"1,3,5", 5, []int{1, 3, 5}, false},
		{"1-3,5", 5, []int{1, 2, 3, 5}, false},
		{"even", 6, []int{2, 4, 6}, false},
		{"odd", 6, []int{1, 3, 5}, false},
		{"last", 5, []int{5}, false},
		{"0", 5, nil, true},
		{"6", 5, nil, true},
		{"3-1", 5, nil, true},
		{"", 5, nil, true},
		{"abc", 5, nil, true},
		// parsePages neither sorts nor deduplicates, and the page operations
		// treat its result as a SET, so these two pin the real contract rather
		// than the "sorted slice" the doc comment used to promise.
		{"3,1", 5, []int{3, 1}, false},
		{"1,1", 5, []int{1, 1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := parsePages(tt.spec, tt.pageCount)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePages(%q, %d) err=%v, wantErr=%t", tt.spec, tt.pageCount, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			// Compare VALUES, not just the length. This assertion read
			// `len(got) != len(tt.want)` for a long time, which made every want
			// slice above decorative: parsePages("1-3", 5) could have returned
			// [9, 9, 9] and passed.
			if len(got) != len(tt.want) {
				t.Fatalf("parsePages(%q, %d) = %v, want %v", tt.spec, tt.pageCount, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parsePages(%q, %d) = %v, want %v", tt.spec, tt.pageCount, got, tt.want)
				}
			}
		})
	}
}
