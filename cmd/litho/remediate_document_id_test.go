package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// remediateTwice runs the command args builds for each of two output paths
// and returns both outputs.
func remediateTwice(t *testing.T, args func(out string) []string) ([]byte, []byte) {
	t.Helper()
	var outs [2][]byte
	for i := range outs {
		out := filepath.Join(t.TempDir(), "out.pdf")
		if rc, _, stderr := runPipeCLI(t, args(out)...); rc != exitOK {
			t.Fatalf("exit %d, stderr: %s", rc, stderr)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		outs[i] = data
	}
	return outs[0], outs[1]
}

// TestRemediate_DocumentIDPinsOutputBytes pins that --document-id makes two
// runs over the same file write the same bytes, on the command and on the
// pipe's remediate stage. The fixture has no /ID, so remediation writes one,
// and without the flag it is random: the two runs differ, which keeps the
// check from passing on a file whose output never varied.
func TestRemediate_DocumentIDPinsOutputBytes(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	// The hex spelling of "LITHO-PINNED-ID!", which the writer emits as a
	// literal string.
	const id = "4c4954484f2d50494e4e45442d494421"

	a, b := remediateTwice(t, func(out string) []string {
		return []string{"remediate", "--profile", "PDF/A-2b", "-o", out, src}
	})
	if bytes.Equal(a, b) {
		t.Fatal("two runs without --document-id wrote the same bytes, so this fixture cannot show the flag works")
	}

	a, b = remediateTwice(t, func(out string) []string {
		return []string{"remediate", "--profile", "PDF/A-2b", "--document-id", id, "-o", out, src}
	})
	if !bytes.Equal(a, b) {
		t.Error("remediate --document-id: two runs wrote different bytes")
	}
	if !bytes.Contains(a, []byte("(LITHO-PINNED-ID!)")) {
		t.Error("remediate --document-id: the output does not carry the pinned /ID")
	}

	a, b = remediateTwice(t, func(out string) []string {
		return []string{"pipe", "-o", out, src, "remediate", "-profile", "PDF/A-2b", "-document-id", id}
	})
	if !bytes.Equal(a, b) {
		t.Error("pipe remediate -document-id: two runs wrote different bytes")
	}
	if !bytes.Contains(a, []byte("(LITHO-PINNED-ID!)")) {
		t.Error("pipe remediate -document-id: the output does not carry the pinned /ID")
	}
}

// TestRemediate_DocumentIDMustBe32HexDigits pins that a --document-id other
// than 32 hex digits fails before any remediation, on the command and on the
// pipe's remediate stage, rather than falling back to a random /ID the caller
// asked not to get.
func TestRemediate_DocumentIDMustBe32HexDigits(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	for _, bad := range []string{"0011", "zz112233445566778899aabbccddeeff", "00112233445566778899aabbccddeeff00"} {
		if rc, _, stderr := runPipeCLI(t, "remediate", "--profile", "PDF/A-2b", "--document-id", bad, "-o", out, src); rc != exitError || !strings.Contains(stderr, "--document-id") {
			t.Errorf("remediate --document-id %s: exit %d, stderr: %s", bad, rc, stderr)
		}
		if rc, _, stderr := runPipeCLI(t, "pipe", "-o", out, src, "remediate", "-profile", "PDF/A-2b", "-document-id", bad); rc != exitError || !strings.Contains(stderr, "--document-id") {
			t.Errorf("pipe remediate -document-id %s: exit %d, stderr: %s", bad, rc, stderr)
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("an output was written despite the bad --document-id")
	}
}
