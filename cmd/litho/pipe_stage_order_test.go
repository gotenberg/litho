package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPipe_RefusesAStageOrderBeforeRunningAnything pins that pipe checks the
// order of its stages before it opens the input or runs a stage.
//
// A remediation is the last edit a document takes: its report describes the
// output only when nothing edits it afterwards, so remediate must be the last
// stage, and a pipeline takes one. PDF/A forbids encryption (ISO 19005-1 to
// -4, 6.1.3), so an encrypt stage cannot precede a PDF/A remediation either.
// The input named here does not exist, so a pipeline that got as far as
// opening it would fail with a different message.
func TestPipe_RefusesAStageOrderBeforeRunningAnything(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pdf")
	for _, tc := range []struct {
		name   string
		stages []string
		want   []string
	}{
		{
			name:   "a stage after remediate",
			stages: []string{"remediate", "-profile", "PDF/A-2b", "rotate", "-angle", "90"},
			want:   []string{`"rotate"`, "remediate must be the last stage"},
		},
		{
			name:   "every stage after remediate is named",
			stages: []string{"remediate", "-profile", "PDF/UA-1", "stamp-text", "-text", "X", "delete", "-pages", "1"},
			want:   []string{`"stamp-text"`, `"delete"`},
		},
		{
			name:   "a second remediate",
			stages: []string{"rotate", "-angle", "90", "remediate", "-profile", "PDF/A-2b", "remediate", "-profile", "PDF/UA-1"},
			want:   []string{"one remediate stage"},
		},
		{
			name:   "encrypt before a PDF/A remediate",
			stages: []string{"encrypt", "-user-password", "u", "remediate", "-profile", "PDF/A-2b"},
			want:   []string{"ISO 19005-2, 6.1.3", `"encrypt"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.pdf")
			args := append([]string{"pipe", missing}, tc.stages...)
			rc, _, stderr := runPipeCLI(t, append(args, "-o", out)...)
			if rc != exitError {
				t.Fatalf("exit %d, want %d; stderr: %s", rc, exitError, stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr %q lacks %q", stderr, want)
				}
			}
			if strings.Contains(stderr, "no such file") {
				t.Errorf("pipe opened the input before refusing the stage order: %q", stderr)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("pipe created %s for a refused pipeline", out)
			}
		})
	}
}

// TestPipe_AcceptsEditsBeforeRemediate is the control: the same stages in the
// order the rule asks for run, and so does an encryption before a PDF/UA
// remediation, which ISO 14289-1 permits.
func TestPipe_AcceptsEditsBeforeRemediate(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	for _, stages := range [][]string{
		{"rotate", "-angle", "90", "remediate", "-profile", "PDF/A-2b"},
		{"stamp-text", "-text", "X", "set-metadata", "-title", "T", "remediate", "-profile", "PDF/UA-1"},
		{"encrypt", "-user-password", "u", "remediate", "-profile", "PDF/UA-1"},
	} {
		t.Run(strings.Join(stages, " "), func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.pdf")
			args := append([]string{"pipe", src}, stages...)
			if rc, _, stderr := runPipeCLI(t, append(args, "-o", out)...); rc != exitOK {
				t.Fatalf("exit %d; stderr: %s", rc, stderr)
			}
			if _, err := os.Stat(out); err != nil {
				t.Errorf("no output: %v", err)
			}
		})
	}
}
