package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An empty page selection must be an error, never an empty slice.
//
// [pdf.Writer.RotatePages] and the three stamps document an empty variadic as
// EVERY page, so a selection that names nothing inverts the request instead of
// doing nothing. Only "even" and "odd" can select nothing: a comma or range part
// yields at least one page or fails, and "last" already rejects a page-less
// document. DeletePages is the one operation that caught this on its own, via
// ErrEmptyPageList.

func TestParsePages_EmptySelectionIsAnError(t *testing.T) {
	tests := []struct {
		spec      string
		pageCount int
	}{
		{"even", 1},
		{"even", 0},
		{"odd", 0},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := parsePages(tt.spec, tt.pageCount)
			if err == nil {
				t.Fatalf("parsePages(%q, %d) = %v, want an error: an empty selection "+
					"reaches the writer as \"every page\"", tt.spec, tt.pageCount, got)
			}
		})
	}
}

// TestParsePages_NonEmptySelectionStillWorks is the control for the test above.
//
// Without it, the error assertions would also pass if "even" and "odd" stopped
// resolving at all.
func TestParsePages_NonEmptySelectionStillWorks(t *testing.T) {
	tests := []struct {
		spec      string
		pageCount int
		want      []int
	}{
		{"even", 2, []int{2}},
		{"even", 6, []int{2, 4, 6}},
		{"odd", 1, []int{1}},
		{"odd", 6, []int{1, 3, 5}},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := parsePages(tt.spec, tt.pageCount)
			if err != nil {
				t.Fatalf("parsePages(%q, %d) err = %v, want nil", tt.spec, tt.pageCount, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parsePages(%q, %d) = %v, want %v", tt.spec, tt.pageCount, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parsePages(%q, %d) = %v, want %v", tt.spec, tt.pageCount, got, tt.want)
				}
			}
		})
	}
}

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
