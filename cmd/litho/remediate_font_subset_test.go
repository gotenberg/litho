package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wholeFontDir is a directory holding one font file,
// testdata/whole_font_sans.ttf: a TrueType program built by the fontbuild test
// package, named LiberationSans, drawing C, E, K, P, R, S, T, X, Y and Z at the
// Helvetica widths. It serves the unembedded Helvetica of redactableCLIPDF,
// which shows SECRET and KEEP, and holds glyphs that document never shows.
const wholeFontDir = "testdata"

// subsetBaseFont matches a /BaseFont carrying the subset tag ISO 32000-1,
// 9.6.4 gives a font whose program holds only part of its glyphs.
var subsetBaseFont = regexp.MustCompile(`/[A-Z]{6}\+Helvetica`)

// TestRemediate_NoFontSubsetEmbedsWholePrograms pins that --no-font-subset
// embeds the --font-dir program whole, on the command and on the pipe's
// remediate stage, where the default embeds a tagged subset.
func TestRemediate_NoFontSubsetEmbedsWholePrograms(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	for _, tc := range []struct {
		name  string
		flags []string
		whole bool
	}{
		{"default", nil, false},
		{"no-font-subset", []string{"--no-font-subset"}, true},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		args := append([]string{"remediate", "--profile", "PDF/A-2b", "--font-dir", wholeFontDir}, tc.flags...)
		rc, stdout, stderr := runPipeCLI(t, append(args, "-o", out, src)...)
		if rc != exitOK {
			t.Fatalf("%s: exit %d, stderr: %s", tc.name, rc, stderr)
		}
		want := "embedded LiberationSans (subset of "
		if tc.whole {
			want = "the caller asked for whole programs"
		}
		if !strings.Contains(stdout, want) {
			t.Errorf("%s: the report does not say %q:\n%s", tc.name, want, stdout)
		}
		assertSubsetTag(t, "remediate "+tc.name, out, !tc.whole)

		pipeOut := filepath.Join(t.TempDir(), "pipe.pdf")
		stage := []string{"remediate", "-profile", "PDF/A-2b", "-font-dir", wholeFontDir}
		for _, f := range tc.flags {
			stage = append(stage, strings.TrimPrefix(f, "-"))
		}
		if rc, _, stderr := runPipeCLI(t, append([]string{"pipe", "-o", pipeOut, src}, stage...)...); rc != exitOK {
			t.Fatalf("pipe %s: exit %d, stderr: %s", tc.name, rc, stderr)
		}
		assertSubsetTag(t, "pipe remediate "+tc.name, pipeOut, !tc.whole)
	}
}

// assertSubsetTag checks whether the file at path embeds a TrueType program
// as a subset, which tags the Helvetica font it serves.
func assertSubsetTag(t *testing.T, name, path string, subset bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if !strings.Contains(string(data), "/FontFile2") {
		t.Fatalf("%s: the output embeds no TrueType program", name)
	}
	if got := subsetBaseFont.Match(data); got != subset {
		t.Errorf("%s: subset tag present %v, want %v", name, got, subset)
	}
}

// TestRemediate_NoFontSubsetNeedsFontDir pins that --no-font-subset without
// --font-dir fails before any remediation, on the command and on the pipe's
// remediate stage: with no program supplied there is nothing to keep whole.
func TestRemediate_NoFontSubsetNeedsFontDir(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	if rc, _, stderr := runPipeCLI(t, "remediate", "--profile", "PDF/A-2b", "--no-font-subset", "-o", out, src); rc != exitError || !strings.Contains(stderr, "--no-font-subset needs --font-dir") {
		t.Errorf("remediate: exit %d, stderr: %s", rc, stderr)
	}
	if rc, _, stderr := runPipeCLI(t, "pipe", "-o", out, src, "remediate", "-profile", "PDF/A-2b", "-no-font-subset"); rc != exitError || !strings.Contains(stderr, "--no-font-subset needs --font-dir") {
		t.Errorf("pipe remediate: exit %d, stderr: %s", rc, stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("an output was written despite the lone --no-font-subset")
	}
}
