package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// stageFlagNames returns the flag names a usage dump lists, one per line
// starting with a dash, sorted.
func stageFlagNames(usage string) []string {
	var names []string
	for line := range strings.SplitSeq(usage, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || !strings.HasPrefix(f[0], "-") {
			continue
		}
		names = append(names, strings.TrimLeft(f[0], "-"))
	}
	slices.Sort(names)
	return names
}

// TestPipeColorStages_FlagParity pins that the convert-colors and
// flatten-transparency stages take the flags of their commands, with the same
// defaults, less the ones pipe owns: the output, the password, the report
// format and the default page size.
func TestPipeColorStages_FlagParity(t *testing.T) {
	t.Parallel()

	src := writeTempPDF(t, untaggedTextPDF())
	pipeOwned := map[string]bool{"o": true, "p": true, "f": true, "default-page-size": true}
	for _, name := range []string{"convert-colors", "flatten-transparency"} {
		var cmdOut, cmdErr bytes.Buffer
		if rc := run(context.Background(), []string{name, "-h"}, &cmdOut, &cmdErr); rc != exitOK {
			t.Fatalf("%s -h: exit %d", name, rc)
		}
		var stageOut, stageErr bytes.Buffer
		if rc := run(context.Background(), []string{"pipe", src, name, "-h"}, &stageOut, &stageErr); rc != exitOK {
			t.Fatalf("pipe %s -h: exit %d, stderr %s", name, rc, stageErr.String())
		}
		var want []string
		for _, n := range stageFlagNames(cmdErr.String()) {
			if !pipeOwned[n] {
				want = append(want, n)
			}
		}
		got := stageFlagNames(stageErr.String())
		if len(want) < 4 {
			t.Fatalf("%s: parsed only %v out of the command's usage", name, want)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: stage flags %v, command flags %v", name, got, want)
		}
		// Each flag line of the stage, defaults included, reads as the
		// command's.
		for line := range strings.SplitSeq(stageErr.String(), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "-") && !strings.Contains(cmdErr.String(), line) {
				t.Errorf("%s: stage usage line %q differs from the command's", name, line)
			}
		}
	}
}

// TestPipeColorStages_WriteWhatTheCommandsWrite pins that each stage writes
// the bytes its command writes, on a page with transparency and one without.
func TestPipeColorStages_WriteWhatTheCommandsWrite(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		pdf   []byte
		stage []string
	}{
		{"convert to CMYK", translucentCyanPDF(), []string{"convert-colors", "--target", "cmyk", "--intent", "Perceptual", "--bpc"}},
		{"convert to RGB, nothing to convert", untaggedTextPDF(), []string{"convert-colors", "--target", "rgb"}},
		{"flatten", translucentCyanPDF(), []string{"flatten-transparency", "--dpi", "72"}},
		{"flatten, nothing to flatten", untaggedTextPDF(), []string{"flatten-transparency"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := writeTempPDF(t, tc.pdf)
			dir := t.TempDir()
			standalone, piped := filepath.Join(dir, "standalone.pdf"), filepath.Join(dir, "piped.pdf")

			var stdout, stderr bytes.Buffer
			args := append(append([]string{tc.stage[0]}, tc.stage[1:]...), "-o", standalone, src)
			if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
				t.Fatalf("standalone: exit %d, stderr %s", rc, stderr.String())
			}
			rc, errOut := runPipeArgs(t, append(append([]string{"pipe", src}, tc.stage...), "-o", piped))
			if rc != exitOK {
				t.Fatalf("pipe: exit %d, stderr %s", rc, errOut)
			}
			want, err := os.ReadFile(standalone) //nolint:gosec // the test's own temp file
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(piped) //nolint:gosec // the test's own temp file
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("pipe wrote %d bytes, the command %d, and they differ", len(got), len(want))
			}
		})
	}
}

// TestPipeColorStages_Refusals pins that a refused conversion fails the
// pipeline and writes nothing, as the command does, and that two stages
// naming different CMYK profiles are refused before anything runs.
func TestPipeColorStages_Refusals(t *testing.T) {
	t.Parallel()

	src := writeTempPDF(t, unconvertibleImagePDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	rc, errOut := runPipeArgs(t, []string{"pipe", src, "convert-colors", "--target", "cmyk", "-o", out})
	if rc != exitError || !strings.Contains(errOut, "converting colours") {
		t.Errorf("unconvertible image: exit %d, stderr %q", rc, errOut)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused conversion left an output: %v", err)
	}

	cyan := writeTempPDF(t, translucentCyanPDF())
	white := writeWhiteCMYKProfile(t)
	rc, errOut = runPipeArgs(t, []string{
		"pipe", cyan, "convert-colors", "--target", "rgb", "--cmyk-profile", white,
		"flatten-transparency", "--cmyk-profile", filepath.Join(t.TempDir(), "other.icc"), "-o", out,
	})
	if rc != exitError || !strings.Contains(errOut, "one profile for the whole pipeline") {
		t.Errorf("two CMYK profiles: exit %d, stderr %q", rc, errOut)
	}
}

// TestPipeColorStages_CMYKProfile pins that a stage's --cmyk-profile reaches
// the document the stage reads: a profile that reads every colour as white
// flattens the translucent cyan page to white, as the command does.
func TestPipeColorStages_CMYKProfile(t *testing.T) {
	t.Parallel()

	src := writeTempPDF(t, translucentCyanPDF())
	white := [3]uint32{255, 255, 255}
	for _, tc := range []struct {
		name      string
		stage     []string
		wantWhite bool
	}{
		{"built-in", []string{"flatten-transparency"}, false},
		{"flag", []string{"flatten-transparency", "--cmyk-profile", writeWhiteCMYKProfile(t)}, true},
		{"after another stage", []string{"set-metadata", "-title", "T", "flatten-transparency", "--cmyk-profile", writeWhiteCMYKProfile(t)}, true},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		rc, errOut := runPipeArgs(t, append(append([]string{"pipe", src}, tc.stage...), "-o", out))
		if rc != exitOK {
			t.Fatalf("%s: exit %d, stderr %s", tc.name, rc, errOut)
		}
		if got := renderCentre(t, out); (got == white) != tc.wantWhite {
			t.Errorf("%s: centre %v, want white %v", tc.name, got, tc.wantWhite)
		}
	}
}
