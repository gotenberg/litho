package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemediate_FontDirReportsRefusedFonts runs remediate with --font-dir over
// a directory holding no font for the document's unembedded Helvetica. The
// report lists the font as left unembedded, which only a remediation that
// received the provider can say: without the flag it lists no such line.
func TestRemediate_FontDirReportsRefusedFonts(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	fonts := t.TempDir()
	if err := os.WriteFile(filepath.Join(fonts, "notes.txt"), []byte("no font here"), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, stdout, stderr := runPipeCLI(t, "remediate", "--profile", "PDF/A-2b", "--font-dir", fonts,
		"-o", filepath.Join(t.TempDir(), "out.pdf"), src)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr)
	}
	if !strings.Contains(stdout, "Fonts left unembedded: 1") || !strings.Contains(stdout, "Helvetica") {
		t.Errorf("the report names no refused font:\n%s", stdout)
	}

	rc, stdout, _ = runPipeCLI(t, "remediate", "--profile", "PDF/A-2b",
		"-o", filepath.Join(t.TempDir(), "out.pdf"), src)
	if rc != exitOK || strings.Contains(stdout, "Fonts left unembedded") {
		t.Errorf("without --font-dir: exit %d, report:\n%s", rc, stdout)
	}
}

// TestRemediate_FontDirMustBeADirectory pins that a --font-dir naming no
// directory fails before any remediation, on the command and on the pipe's
// remediate stage.
func TestRemediate_FontDirMustBeADirectory(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	missing := filepath.Join(t.TempDir(), "absent")
	out := filepath.Join(t.TempDir(), "out.pdf")

	if rc, _, stderr := runPipeCLI(t, "remediate", "--profile", "PDF/A-2b", "--font-dir", missing, "-o", out, src); rc != exitError || !strings.Contains(stderr, "--font-dir") {
		t.Errorf("remediate: exit %d, stderr: %s", rc, stderr)
	}
	if rc, _, stderr := runPipeCLI(t, "pipe", "-o", out, src, "remediate", "-profile", "PDF/A-2b", "-font-dir", missing); rc != exitError || !strings.Contains(stderr, "--font-dir") {
		t.Errorf("pipe remediate: exit %d, stderr: %s", rc, stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("an output was written despite the bad --font-dir")
	}
}

// TestPipeRemediate_AcceptsFontDir pins the stage flag the standalone command
// has.
func TestPipeRemediate_AcceptsFontDir(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	if rc, _, stderr := runPipeCLI(t, "pipe", "-o", out, src, "remediate", "-profile", "PDF/A-2b", "-font-dir", t.TempDir()); rc != exitOK {
		t.Errorf("exit %d, stderr: %s", rc, stderr)
	}
}
