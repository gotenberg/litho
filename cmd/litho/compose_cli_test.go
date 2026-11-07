package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompose_PlacesTwoPagesOnOne pins the command's main use: two -place
// flags naming target page 1 give a one-page document.
func TestCompose_PlacesTwoPagesOnOne(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "label.pdf")

	var stdout, stderr bytes.Buffer
	args := []string{
		"compose", "-o", out, "-width", "432", "-height", "360",
		"-place", "src=" + src + ",x=0,y=216,scale=0.25",
		"-place", "src=" + src + ",page=1,x=0,y=0,scale=0.25,rotation=0,on=1",
	}
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("compose: exit %d, stderr: %s", rc, stderr.String())
	}
	if got := pageCountOf(t, out); got != 1 {
		t.Errorf("compose produced %d pages, want 1", got)
	}
}

// TestCompose_RefusesAPageSizeOutOfRange pins that a width under 3 points
// exits with the invalid-input status, names the size, and leaves no output.
func TestCompose_RefusesAPageSizeOutOfRange(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	args := []string{"compose", "-o", out, "-width", "2", "-height", "360", "-place", "src=" + src}
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitError {
		t.Fatalf("compose -width 2: exit %d, want %d; stderr: %s", rc, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "2 by 360") {
		t.Errorf("stderr = %q, want it to name the size", strings.TrimSpace(stderr.String()))
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused composition left %s behind: %v", out, err)
	}
}

// TestCompose_PlaceWithoutSrcFailsBeforeOpening pins that a -place naming no
// source is refused as the flags parse, before the file an earlier -place
// names is opened: that file does not exist, and the error is not about it.
func TestCompose_PlaceWithoutSrcFailsBeforeOpening(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pdf")
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	args := []string{
		"compose", "-o", out, "-width", "432", "-height", "360",
		"-place", "src=" + missing, "-place", "x=10,y=10",
	}
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitError {
		t.Fatalf("compose: exit %d, want %d; stderr: %s", rc, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "src is required") {
		t.Errorf("stderr = %q, want it to say src is required", strings.TrimSpace(stderr.String()))
	}
	if strings.Contains(stderr.String(), "missing.pdf") {
		t.Errorf("stderr = %q, the command opened a source before refusing the flag", strings.TrimSpace(stderr.String()))
	}
}
