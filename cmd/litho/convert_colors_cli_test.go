package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestConvertColorsFlags_SelectTheTarget pins how the flags map onto the
// library's target and options, and the combinations they refuse.
func TestConvertColorsFlags_SelectTheTarget(t *testing.T) {
	t.Parallel()
	profile := filepath.Join(t.TempDir(), "p.icc")
	if err := os.WriteFile(profile, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := convertColorsFlags{target: "output-intent", intent: "RelativeColorimetric"}

	for _, tc := range []struct {
		name  string
		edit  func(*convertColorsFlags)
		want  pdf.ColorTarget
		opts  int
		error string
	}{
		{"default", func(*convertColorsFlags) {}, pdf.OutputIntentColorTarget(), 1, ""},
		{"cmyk with every option", func(f *convertColorsFlags) {
			f.target, f.intent, f.bpc, f.convertSpots, f.noOutputIntent = "cmyk", "Perceptual", true, true, true
		}, pdf.CMYKColorTarget(), 4, ""},
		{"rgb", func(f *convertColorsFlags) { f.target = "rgb" }, pdf.RGBColorTarget(), 1, ""},
		{"gray", func(f *convertColorsFlags) { f.target = "gray" }, pdf.GrayColorTarget(), 1, ""},
		{"icc", func(f *convertColorsFlags) { f.target, f.profile = "icc", profile }, pdf.ICCColorTarget([]byte("bytes")), 1, ""},
		{"icc without a profile", func(f *convertColorsFlags) { f.target = "icc" }, pdf.ColorTarget{}, 0, "needs --profile"},
		{"profile without icc", func(f *convertColorsFlags) { f.target, f.profile = "cmyk", profile }, pdf.ColorTarget{}, 0, "--profile names"},
		{"unknown target", func(f *convertColorsFlags) { f.target = "lab" }, pdf.ColorTarget{}, 0, "unknown target"},
		{"unknown intent", func(f *convertColorsFlags) { f.intent = "perceptual" }, pdf.ColorTarget{}, 0, "unknown rendering intent"},
	} {
		f := base
		tc.edit(&f)
		target, opts, err := f.colorTarget()
		if tc.error != "" {
			if err == nil || !strings.Contains(err.Error(), tc.error) {
				t.Errorf("%s: err = %v, want one containing %q", tc.name, err, tc.error)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !targetsEqual(target, tc.want) || len(opts) != tc.opts {
			t.Errorf("%s: target %+v with %d option(s), want %+v with %d", tc.name, target, len(opts), tc.want, tc.opts)
		}
	}
}

// targetsEqual compares two targets by what they print, which names their
// kind and profile bytes.
func targetsEqual(a, b pdf.ColorTarget) bool {
	return fmt.Sprintf("%+v", a) == fmt.Sprintf("%+v", b)
}

// TestConvertColors_RefusalWritesNothing pins that a refused conversion
// exits with an error, says why, and leaves no output file, and that the
// encryption warning comes before the work, as for redact.
func TestConvertColors_RefusalWritesNothing(t *testing.T) {
	src := encryptTempPDF(t, writeTempPDF(t, untaggedTextPDF()), "s3cret")
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"convert-colors", "-p", "s3cret", "--target", "cmyk", "-o", out, src}, &stdout, &stderr)
	if rc != exitError {
		t.Fatalf("exit %d, want %d; stderr: %s", rc, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "colour conversion refused") {
		t.Errorf("stderr does not say the conversion was refused: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), encryptionWarning) {
		t.Errorf("no encryption warning; stderr: %q", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused conversion left %s behind (stat: %v)", out, err)
	}
}

// TestConvertColors_Usage pins the usage line of a call missing its output.
func TestConvertColors_Usage(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"convert-colors", "in.pdf"}, &stdout, &stderr); rc != exitError {
		t.Fatalf("exit %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "Usage: litho convert-colors") {
		t.Errorf("stderr = %q", stderr.String())
	}
}
