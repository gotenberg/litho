package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRotate_EmptySelectionDoesNotRotateEveryPage pins the defect end to end.
//
// Measured before the guard: `rotate -angle 90 -pages even` on a one-page
// document wrote /Rotate and exited 0, i.e. it rotated the page the user had
// excluded. The control below must show a real rotation, or this test proves
// nothing about the one above it.
func TestRotate_EmptySelectionDoesNotRotateEveryPage(t *testing.T) {
	src := onePageTempPDF(t)

	hasRotate := func(path string) bool {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		return bytes.Contains(b, []byte("/Rotate"))
	}

	if hasRotate(src) {
		t.Fatal("the fixture already carries /Rotate, so the assertions below prove nothing")
	}

	ctl := filepath.Join(t.TempDir(), "control.pdf")
	var so, se bytes.Buffer
	if rc := run(context.Background(), []string{"rotate", "-angle", "90", "-pages", "1", "-o", ctl, src}, &so, &se); rc != exitOK {
		t.Fatalf("control rotate: exit %d, stderr: %s", rc, se.String())
	}
	if !hasRotate(ctl) {
		t.Fatal("the control did not rotate, so this test cannot detect an unwanted rotation")
	}

	out := filepath.Join(t.TempDir(), "even.pdf")
	so.Reset()
	se.Reset()
	rc := run(context.Background(), []string{"rotate", "-angle", "90", "-pages", "even", "-o", out, src}, &so, &se)
	if rc == exitOK {
		t.Errorf("rotate -pages even on a one-page document exited %d, want non-zero", rc)
	}
	if !strings.Contains(se.String(), "selects no pages") {
		t.Errorf("stderr = %q, want it to say the specification selects no pages", strings.TrimSpace(se.String()))
	}
	if _, err := os.Stat(out); err == nil && hasRotate(out) {
		t.Error("an empty selection still rotated the page")
	}
}
