package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// dejavuAB is a DejaVu Sans subset of the repository covering U+0020, A and
// B: the CLI tests write their text in those characters alone.
const dejavuAB = "../../pkg/internal/render/page/testdata/dejavu_sans_ab.ttf"

// convertFontDir returns a fresh directory holding dejavuAB, so no test reads
// the system's fonts.
func convertFontDir(t *testing.T) string {
	t.Helper()
	prog, err := os.ReadFile(dejavuAB)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dejavu_sans_ab.ttf"), prog, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeTempText writes content to name in a fresh directory.
func writeTempText(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// pdfText opens path and returns the text of each page.
func pdfText(t *testing.T, path string) []string {
	t.Helper()
	ctx := context.Background()
	r, err := pdf.OpenFile(ctx, path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pages []string
	for p := 1; p <= n; p++ {
		s, err := r.ExtractText(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, strings.Join(strings.Fields(s), " "))
	}
	return pages
}

func TestConvert_WritesAPDF(t *testing.T) {
	t.Parallel()
	src := writeTempText(t, "note.txt", "AB BA\nBAB\fAAA B\n")
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	args := []string{"convert", "--font-dir", convertFontDir(t), "--paper", "Letter", "--margin", "36", "--font-size", "10", "-o", out, src}
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr %s", rc, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("stdout %q, stderr %q, want nothing", stdout.String(), stderr.String())
	}
	if got := pdfText(t, out); len(got) != 2 || got[0] != "AB BA BAB" || got[1] != "AAA B" {
		t.Errorf("pages %q", got)
	}
}

// TestConvert_ReadsStdin swaps os.Stdin, so it must not run in parallel.
func TestConvert_ReadsStdin(t *testing.T) {
	f, err := os.Open(writeTempText(t, "in", "ABBA AB")) //nolint:gosec // a fixture this test just wrote
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck // read-only fixture handle
	saved := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = saved })

	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"convert", "--font-dir", convertFontDir(t), "-o", out, "-"}, &stdout, &stderr); rc != exitError || !strings.Contains(stderr.String(), "--from") {
		t.Errorf("stdin without --from: exit %d, stderr %q, want 1 naming --from", rc, stderr.String())
	}
	stderr.Reset()
	if rc := run(context.Background(), []string{"convert", "--from", "text", "--font-dir", convertFontDir(t), "-o", out, "-"}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr %s", rc, stderr.String())
	}
	if got := pdfText(t, out); len(got) != 1 || got[0] != "ABBA AB" {
		t.Errorf("pages %q", got)
	}
}

func TestConvert_ReportsJSON(t *testing.T) {
	t.Parallel()
	src := writeTempText(t, "note.text", "ABC\nAB\n")
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"convert", "-f", "json", "--font-dir", convertFontDir(t), "-o", out, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr %s", rc, stderr.String())
	}
	var res convertResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("stdout %q: %v", stdout.String(), err)
	}
	if res.Pages != 1 || res.Format != "text" || res.File != src || res.Output != out {
		t.Errorf("report %+v", res)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Code != "missing-glyph" || !strings.Contains(res.Warnings[0].Message, "U+0043") || res.Warnings[0].Page != 1 {
		t.Errorf("warnings %+v", res.Warnings)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr %q, want nothing under -f json", stderr.String())
	}

	// Without -f json the warning goes to stderr.
	stdout.Reset()
	if rc := run(context.Background(), []string{"convert", "--font-dir", convertFontDir(t), "-o", out, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d", rc)
	}
	if !strings.HasPrefix(stderr.String(), "warning: page 1: missing-glyph: no font covers U+0043") || strings.Count(stderr.String(), "\n") != 1 {
		t.Errorf("stderr %q", stderr.String())
	}
}

func TestConvert_RefusesAnUnknownExtension(t *testing.T) {
	t.Parallel()
	src := writeTempText(t, "notes.md", "AB")
	out := filepath.Join(t.TempDir(), "out.pdf")
	for _, args := range [][]string{
		{"convert", "--font-dir", convertFontDir(t), "-o", out, src},
		{"convert", "--from", "markdown", "--font-dir", convertFontDir(t), "-o", out, src},
	} {
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitError || !strings.Contains(stderr.String(), "--from") {
			t.Errorf("%q: exit %d, stderr %q, want 1 naming --from", args, rc, stderr.String())
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%q left %s: %v", args, out, err)
		}
	}
	// With --from text the extension no longer matters.
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"convert", "--from", "text", "--font-dir", convertFontDir(t), "-o", out, src}, &stdout, &stderr); rc != exitOK {
		t.Errorf("--from text: exit %d, stderr %s", rc, stderr.String())
	}
}

func TestConvert_GlobalLimitsReachTheConversion(t *testing.T) {
	t.Parallel()
	src := writeTempText(t, "pages.txt", "A\fB\fA")
	out := filepath.Join(t.TempDir(), "out.pdf")
	for _, limit := range [][]string{{"--max-convert-pages", "2"}, {"--max-convert-input-bytes", "4"}} {
		var stdout, stderr bytes.Buffer
		args := append(limit, "convert", "--font-dir", convertFontDir(t), "-o", out, src)
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitError || !strings.Contains(stderr.String(), "limit") {
			t.Errorf("%q: exit %d, stderr %q, want 1 naming the limit", args, rc, stderr.String())
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%q left %s behind: %v", args, out, err)
		}
	}
}

func TestConvert_RefusesBadFlags(t *testing.T) {
	t.Parallel()
	src := writeTempText(t, "a.txt", "A")
	out := filepath.Join(t.TempDir(), "out.pdf")
	for _, args := range [][]string{
		{"convert", src},
		{"convert", "-o", out},
		{"convert", "--margin", "1,2", "-o", out, src},
		{"convert", "--paper", "B7", "-o", out, src},
		{"convert", "--font-dir", filepath.Join(t.TempDir(), "absent"), "-o", out, src},
		{"convert", "-f", "json", "-o", "-", src},
		{"convert", "--font-dir", convertFontDir(t), "--margin", "500,0,500,0", "-o", out, src},
	} {
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitError || stderr.Len() == 0 {
			t.Errorf("%q: exit %d, stderr %q, want 1 with a message", args, rc, stderr.String())
		}
	}
}
