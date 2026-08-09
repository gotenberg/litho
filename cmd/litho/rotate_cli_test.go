package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// pageRotations runs the pages command and returns the /Rotate column, which
// reports each page's EFFECTIVE rotation. Scanning the raw file bytes would not:
// litho writes an incremental update, so the superseded page object and its old
// /Rotate value are still present in the output.
func pageRotations(t *testing.T, path string) []string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"pages", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("pages %s: exit %d, stderr: %s", path, rc, stderr.String())
	}
	var out []string
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && f[0] != "Page" {
			out = append(out, f[3])
		}
	}
	if len(out) == 0 {
		t.Fatalf("pages %s reported no page rows: %q", path, stdout.String())
	}
	return out
}

// TestRotate_NoPagesFlagRotatesWholeDocument pins the "default: all" the -pages
// flag advertises.
//
// The command leaves its page list empty when -pages is absent, and an empty
// list used to mean "no pages": rotating a whole document reported success and
// wrote back a byte-identical file.
func TestRotate_NoPagesFlagRotatesWholeDocument(t *testing.T) {
	path := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "rotated.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"rotate", "-angle", "90", "-o", out, path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit code: got %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	for i, got := range pageRotations(t, out) {
		if got != "90" {
			t.Errorf("page %d /Rotate = %s, want 90; rotating without -pages did nothing", i+1, got)
		}
	}
}

// TestRotate_AngleZeroIsAccepted pins that 0 reaches the writer. It is one of
// the four values RotatePages takes and the only way to reset a turned page,
// but a plain zero-value check cannot tell it from an absent flag.
func TestRotate_AngleZeroIsAccepted(t *testing.T) {
	path := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	turned := filepath.Join(dir, "turned.pdf")
	reset := filepath.Join(dir, "reset.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"rotate", "-angle", "180", "-o", turned, path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("rotate 180: exit %d, stderr: %s", rc, stderr.String())
	}
	for i, got := range pageRotations(t, turned) {
		if got != "180" {
			t.Fatalf("precondition: page %d /Rotate = %s, want 180", i+1, got)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if rc := run(context.Background(), []string{"rotate", "-angle", "0", "-o", reset, turned}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("rotate 0 was rejected: exit %d, stderr: %s", rc, stderr.String())
	}
	for i, got := range pageRotations(t, reset) {
		if got != "0" {
			t.Errorf("page %d /Rotate = %s, want 0; a turned page cannot be reset", i+1, got)
		}
	}
}

// TestRotate_MissingAngleIsUsageError pins that dropping the flag entirely is
// still refused, so accepting 0 did not make -angle optional.
func TestRotate_MissingAngleIsUsageError(t *testing.T) {
	path := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "no.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"rotate", "-o", out, path}, &stdout, &stderr); rc == exitOK {
		t.Error("rotate without -angle succeeded, want a usage error")
	}
}
