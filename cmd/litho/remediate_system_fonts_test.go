package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemediate_SystemFontsConfiguresAProvider pins that --system-fonts
// reaches the remediation. What the host's fonts supply varies, so the test
// asserts only what a configured provider alone produces: the report either
// embeds the document's unembedded Helvetica or lists it as left unembedded,
// where a remediation without a provider says neither.
func TestRemediate_SystemFontsConfiguresAProvider(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	for _, flags := range [][]string{{"--system-fonts"}, {"--system-fonts", "--no-font-subset"}, {"--system-fonts", "--font-dir", t.TempDir()}} {
		args := append([]string{"remediate", "--profile", "PDF/A-2b"}, flags...)
		args = append(args, "-o", filepath.Join(t.TempDir(), "out.pdf"), src)
		rc, stdout, stderr := runPipeCLI(t, args...)
		if rc != exitOK {
			t.Fatalf("%v: exit %d, stderr: %s", flags, rc, stderr)
		}
		if !strings.Contains(stdout, "Fonts left unembedded") && !strings.Contains(stdout, "embedded ") {
			t.Errorf("%v: the report shows no font provider:\n%s", flags, stdout)
		}
	}
}

// TestPipeRemediate_AcceptsSystemFonts pins the stage flag the standalone
// command has, and that it takes no value: the -o after it stays the pipe's
// output flag.
func TestPipeRemediate_AcceptsSystemFonts(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	if rc, _, stderr := runPipeCLI(t, "pipe", src, "remediate", "-profile", "PDF/A-2b", "-system-fonts", "-o", out); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("no output written: %v", err)
	}
}
