package main

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRender_WritesOnePNGPerPage pins the file naming of litho render, the -o
// prefix, a hyphen, the page number and .png, and the size --dpi gives the
// page.
func TestRender_WritesOnePNGPerPage(t *testing.T) {
	src := writeTempPDF(t, minimalPDF())
	prefix := filepath.Join(t.TempDir(), "page")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"render", "--dpi", "36", "--pages", "1", "-o", prefix, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr %q", rc, stderr.String())
	}
	want := prefix + "-1.png"
	if strings.TrimSpace(stdout.String()) != want {
		t.Errorf("stdout %q, want the file name %q", stdout.String(), want)
	}
	f, err := os.Open(want)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	// minimalPDF is US Letter, 612 by 792 units: half that at 36 dpi.
	if cfg.Width != 306 || cfg.Height != 396 {
		t.Errorf("page %dx%d, want 306x396", cfg.Width, cfg.Height)
	}
}

// TestRender_Refusals pins the usage error without -o, a resolution the
// library refuses, and the render pixel budget the global flag sets.
func TestRender_Refusals(t *testing.T) {
	src := writeTempPDF(t, minimalPDF())
	prefix := filepath.Join(t.TempDir(), "page")
	for _, tc := range []struct {
		args   []string
		stderr string
	}{
		{[]string{"render", src}, "Usage: litho render"},
		{[]string{"render", "--dpi", "0", "-o", prefix, src}, "invalid render options"},
		{[]string{"--max-render-pixels", "1000", "render", "-o", prefix, src}, "page render exceeds its budget"},
	} {
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), tc.args, &stdout, &stderr); rc != exitError {
			t.Errorf("%q: exit %d, want %d", tc.args, rc, exitError)
		}
		if !strings.Contains(stderr.String(), tc.stderr) {
			t.Errorf("%q: stderr %q, want it to hold %q", tc.args, stderr.String(), tc.stderr)
		}
	}
}
