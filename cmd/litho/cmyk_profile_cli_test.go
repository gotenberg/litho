package main

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// translucentCyanPDF builds a 100 by 100 page filled with DeviceCMYK cyan at
// half opacity, which flatten-transparency composites and render draws
// through the profile DeviceCMYK is read in.
func translucentCyanPDF() []byte {
	body := "/GS0 gs 1 0 0 0 k 0 0 100 100 re f"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /ExtGState << /GS0 << /ca 0.5 >> >> >> /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 5 /Root 1 0 R >>\n%%EOF\n")
}

// renderCentre renders page 1 of path at 72 dpi with the extra flags and
// returns its centre pixel.
func renderCentre(t *testing.T, path string, flags ...string) [3]uint32 {
	t.Helper()
	prefix := filepath.Join(t.TempDir(), "page")
	args := append(append([]string{"render", "--pages", "1"}, flags...), "-o", prefix, path)
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("render %v: exit %d, stderr: %s", flags, rc, stderr.String())
	}
	f, err := os.Open(prefix + "-1.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(50, 50).RGBA()
	return [3]uint32{r >> 8, g >> 8, b >> 8}
}

// TestRender_CMYKProfile pins --cmyk-profile on render: DeviceCMYK is drawn
// through the profile the flag names, else through the built-in one.
func TestRender_CMYKProfile(t *testing.T) {
	t.Parallel()

	src := writeTempPDF(t, translucentCyanPDF())
	white := [3]uint32{255, 255, 255}
	if got := renderCentre(t, src); got == white {
		t.Errorf("built-in profile: the cyan drew white")
	}
	if got := renderCentre(t, src, "--cmyk-profile", writeWhiteCMYKProfile(t)); got != white {
		t.Errorf("the flag's profile: centre is %v, want white", got)
	}
}

// TestFlattenTransparency_CMYKProfile pins --cmyk-profile on
// flatten-transparency: the composite is drawn through the profile the flag
// names, so a profile that reads every colour as white leaves a white page.
func TestFlattenTransparency_CMYKProfile(t *testing.T) {
	t.Parallel()

	src := writeTempPDF(t, translucentCyanPDF())
	white := [3]uint32{255, 255, 255}
	for _, tc := range []struct {
		name      string
		args      []string
		wantWhite bool
	}{
		{"the built-in profile", nil, false},
		{"the profile the flag names", []string{"--cmyk-profile", writeWhiteCMYKProfile(t)}, true},
	} {
		out := filepath.Join(t.TempDir(), "flat.pdf")
		args := append(append([]string{"flatten-transparency"}, tc.args...), "-o", out, src)
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
			t.Fatalf("%s: exit %d, stderr: %s", tc.name, rc, stderr.String())
		}
		if got := renderCentre(t, out); (got == white) != tc.wantWhite {
			t.Errorf("%s: flattened centre is %v, want white %v", tc.name, got, tc.wantWhite)
		}
	}
}

// TestCMYKProfileFlagRefusals pins that render and flatten-transparency fail
// on a profile they cannot read, or one litho cannot read CMYK through.
func TestCMYKProfileFlagRefusals(t *testing.T) {
	t.Parallel()

	src := writeTempPDF(t, translucentCyanPDF())
	garbage := filepath.Join(t.TempDir(), "garbage.icc")
	if err := os.WriteFile(garbage, []byte("not a profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"render", "flatten-transparency"} {
		for name, tc := range map[string]struct {
			path, want string
		}{
			"a missing file": {filepath.Join(t.TempDir(), "missing.icc"), "reading the CMYK profile"},
			"not a profile":  {garbage, "invalid default CMYK profile"},
		} {
			out := filepath.Join(t.TempDir(), "out")
			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), []string{cmd, "--cmyk-profile", tc.path, "-o", out, src}, &stdout, &stderr)
			if rc != exitError || !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("%s, %s: exit %d, stderr %q; want %d naming %q", cmd, name, rc, stderr.String(), exitError, tc.want)
			}
		}
	}
}
