package main

import (
	"bytes"
	"context"
	"encoding/json"
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

// TestRender_FontFlags renders with a font directory and the system fonts.
func TestRender_FontFlags(t *testing.T) {
	src := writeTempPDF(t, minimalPDF())
	prefix := filepath.Join(t.TempDir(), "page")
	var stdout, stderr bytes.Buffer
	args := []string{"render", "--dpi", "18", "--font-dir", t.TempDir(), "--system-fonts", "-o", prefix, src}
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr %q", rc, stderr.String())
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
		{[]string{"render", "--font-dir", filepath.Join(t.TempDir(), "missing"), "-o", prefix, src}, "--font-dir"},
		{[]string{"render", "--font-dir", src, "-o", prefix, src}, "is not a directory"},
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

// TestRender_Report pins the JSON litho render --report prints in place of
// the file names: one entry per page, with its image and the counts of
// pdf.RenderReport. untaggedTextPDF shows text in a font it does not embed,
// so the page reports placeholder glyphs.
func TestRender_Report(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	prefix := filepath.Join(t.TempDir(), "page")
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"render", "--dpi", "18", "--report", "-o", prefix, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr %q", rc, stderr.String())
	}
	var got renderResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not the JSON report: %v\n%s", err, stdout.String())
	}
	if got.File != src || len(got.Pages) != 1 {
		t.Fatalf("report %+v, want one page of %s", got, src)
	}
	pg := got.Pages[0]
	if pg.Page != 1 || pg.Image != prefix+"-1.png" {
		t.Errorf("page entry %+v, want page 1 written to %s-1.png", pg, prefix)
	}
	if pg.PlaceholderGlyphs == 0 {
		t.Errorf("page entry %+v, want placeholder glyphs for the unembedded font", pg)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"unreadableGlyphs": 0`)) {
		t.Errorf("report %s, want an unreadableGlyphs count of 0", stdout.String())
	}
	if _, err := os.Stat(pg.Image); err != nil {
		t.Errorf("the reported image: %v", err)
	}
}
