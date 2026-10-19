package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// fontFixtureFile copies a litho corpus fixture embedding a whole TrueType
// program the page shows a few glyphs of to a temp file.
func fontFixtureFile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/glyphs_2b.pdf")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	src := filepath.Join(t.TempDir(), "in.pdf")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return src
}

// runFlattenTo runs flatten-form, which always rewrites the whole file, over
// src with extra flags and returns the output.
func runFlattenTo(t *testing.T, src string, extra ...string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.pdf")
	args := append([]string{"flatten-form", "-o", out}, extra...)
	args = append(args, src)
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("flatten-form %v: exit %d, stderr: %s", extra, rc, stderr.String())
	}
	data, err := os.ReadFile(out) //nolint:gosec // path is this test's own temp file
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return data
}

// allText returns the text litho extracts from data.
func allText(t *testing.T, data []byte) string {
	t.Helper()
	ctx := context.Background()
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Logf("closing reader: %v", err)
		}
	}()
	text, err := r.ExtractAllText(ctx)
	if err != nil {
		t.Fatalf("ExtractAllText: %v", err)
	}
	return text
}

// TestSubsetFontsFlag_IndependentOfCompact pins the contract of the flag: it
// shrinks the output on its own and beside --compact, and the text extracts
// as it did.
func TestSubsetFontsFlag_IndependentOfCompact(t *testing.T) {
	src := fontFixtureFile(t)
	plain := runFlattenTo(t, src)
	compact := runFlattenTo(t, src, "--compact")
	want := allText(t, plain)
	for _, tc := range []struct {
		flags []string
		than  []byte
	}{
		{[]string{"--subset-fonts"}, plain},
		{[]string{"--compact", "--subset-fonts"}, compact},
	} {
		out := runFlattenTo(t, src, tc.flags...)
		if len(out) >= len(tc.than) {
			t.Errorf("%v: output is %d bytes against %d without --subset-fonts", tc.flags, len(out), len(tc.than))
		}
		if got := allText(t, out); got != want {
			t.Errorf("%v: text %q, want %q", tc.flags, got, want)
		}
	}
}

// TestSubsetFontsFlag_EveryCompactCommand checks that every command that
// accepts --compact also accepts --subset-fonts.
func TestSubsetFontsFlag_EveryCompactCommand(t *testing.T) {
	for _, cmd := range []string{"delete", "insert", "encrypt", "decrypt", "flatten-form", "split", "merge", "remediate"} {
		var stdout, stderr bytes.Buffer
		run(context.Background(), []string{cmd, "--subset-fonts"}, &stdout, &stderr)
		if strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Errorf("%s does not accept --subset-fonts: %s", cmd, stderr.String())
		}
	}
}

// TestPipe_SubsetFontsFlag checks the pipe global: it subsets the final write
// of a stage that rewrites the whole file, and warns when the write is an
// incremental update it cannot reach.
func TestPipe_SubsetFontsFlag(t *testing.T) {
	src := fontFixtureFile(t)
	pipe := func(stage ...string) ([]byte, string) {
		t.Helper()
		out := filepath.Join(t.TempDir(), "piped.pdf")
		args := append(append([]string{"pipe", src}, stage...), "--subset-fonts", "-o", out)
		rc, errOut := runPipeArgs(t, args)
		if rc != exitOK {
			t.Fatalf("pipe %v: exit %d, stderr: %s", stage, rc, errOut)
		}
		data, err := os.ReadFile(out) //nolint:gosec // the test's own temp path
		if err != nil {
			t.Fatalf("reading output: %v", err)
		}
		return data, errOut
	}
	plain := runFlattenTo(t, src)
	if out, _ := pipe("flatten-form"); len(out) >= len(plain) {
		t.Errorf("pipe flatten-form --subset-fonts wrote %d bytes against %d without it", len(out), len(plain))
	}
	if _, errOut := pipe("rotate", "-angle", "90"); !strings.Contains(errOut, "--subset-fonts does not apply to an incremental update") {
		t.Errorf("no incremental-update warning on stderr: %q", errOut)
	}
}
