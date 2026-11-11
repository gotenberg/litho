package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// pngAB is a 1 by 1 PNG, the picture the conversion tests describe or not.
var pngAB = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde" +
	"\x00\x00\x00\x0cIDATx\x9cc\xf8\xcf\xc0\x00\x00\x03\x01\x01\x00\xc9\xfe\x92\xef\x00\x00\x00\x00IEND\xaeB`\x82")

// htmlWithPicture writes an HTML document whose picture carries alt, none
// when alt is "", beside the PNG it names, and returns its path.
func htmlWithPicture(t *testing.T, alt string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.png"), pngAB, 0o600); err != nil {
		t.Fatal(err)
	}
	img := `<img src="p.png">`
	if alt != "" {
		img = `<img src="p.png" alt="` + alt + `">`
	}
	path := filepath.Join(dir, "doc.html")
	if err := os.WriteFile(path, []byte(`<!doctype html><title>AB</title><p>AB BA</p>`+img), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestConvert_ProfileReportsConformance checks that --profile writes a
// conforming file, that the JSON report carries the conformance block, and
// that --lang sets the language the profile needs.
func TestConvert_ProfileReportsConformance(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	args := []string{"convert", "--font-dir", convertFontDir(t), "--profile", "PDF/UA-1", "--profile", "PDF/A-2a", "--lang", "en-GB", "--strict", "-f", "json", "-o", out, htmlWithPicture(t, "AB")}
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr %s", rc, stderr.String())
	}
	var res convertTargetResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("report %q: %v", stdout.String(), err)
	}
	if res.Conformance == nil {
		t.Fatalf("report %s has no conformance block", stdout.String())
	}
	if got := strings.Join(res.Conformance.Profile, ","); got != "PDF/UA-1,PDF/A-2a" {
		t.Errorf("profile = %q, want the order given", got)
	}
	if len(res.Conformance.Residual) != 0 {
		t.Errorf("residual = %v, want none", res.Conformance.Residual)
	}
	ctx := context.Background()
	r, err := pdf.OpenFile(ctx, out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if v, err := r.ValidatePDFA(ctx, "PDF/A-2a"); err != nil || len(v) != 0 {
		t.Errorf("ValidatePDFA = %v, %v", v, err)
	}
	if v, err := r.ValidatePDFUA(ctx, "PDF/UA-1"); err != nil || len(v) != 0 {
		t.Errorf("ValidatePDFUA = %v, %v", v, err)
	}
}

// TestConvert_StrictFailsOnAResidual checks that a picture without text
// leaves a residual the text report prints, that --strict exits 2 for it,
// and that the file is written either way.
func TestConvert_StrictFailsOnAResidual(t *testing.T) {
	t.Parallel()
	for _, strict := range []bool{false, true} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		var stdout, stderr bytes.Buffer
		args := []string{"convert", "--font-dir", convertFontDir(t), "--profile", "PDF/UA-1", "--lang", "en", "-o", out, htmlWithPicture(t, "")}
		want := exitOK
		if strict {
			args = append([]string{"convert", "--strict"}, args[1:]...)
			want = exitValidationFail
		}
		if rc := run(context.Background(), args, &stdout, &stderr); rc != want {
			t.Errorf("strict %v: exit %d, want %d; stderr %s", strict, rc, want, stderr.String())
		}
		if !strings.Contains(stderr.String(), "residual: ") || !strings.Contains(stderr.String(), "Figure") {
			t.Errorf("strict %v: stderr %q, want the Figure residual", strict, stderr.String())
		}
		if info, err := os.Stat(out); err != nil || info.Size() == 0 {
			t.Errorf("strict %v: output %v, %v, want the file written", strict, info, err)
		}
	}
}

// TestConvert_RefusesAProfileItCannotWrite checks the --profile, --pdf-version,
// --lang and --temp-dir values litho convert refuses before it writes.
func TestConvert_RefusesAProfileItCannotWrite(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		flags []string
		want  string
	}{
		"Factur-X":           {flags: []string{"--profile", "Factur-X"}, want: "embedded invoice"},
		"PDF/A-4f":           {flags: []string{"--profile", "PDF/A-4f"}, want: "embedded file"},
		"WTPDF":              {flags: []string{"--profile", "WTPDF-1.0-Reuse"}, want: "litho remediate"},
		"unpaired":           {flags: []string{"--profile", "PDF/A-4", "--profile", "PDF/UA-1"}, want: "pairs with"},
		"version":            {flags: []string{"--profile", "PDF/A-1b", "--pdf-version", "1.7"}, want: "PDF version 1.7"},
		"unknown version":    {flags: []string{"--pdf-version", "1.3"}, want: "PDF version"},
		"language":           {flags: []string{"--lang", "en_US"}, want: "language tag"},
		"missing temp dir":   {flags: []string{"--profile", "PDF/A-2b", "--temp-dir", filepath.Join(t.TempDir(), "missing")}, want: "--temp-dir"},
		"unknown PDF/A part": {flags: []string{"--profile", "PDF/A-9"}, want: "PDF/A-9"},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		var stdout, stderr bytes.Buffer
		args := append(append([]string{"convert", "--font-dir", convertFontDir(t)}, tc.flags...), "-o", out, writeTempText(t, "a.txt", "AB"))
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitError {
			t.Errorf("%s: exit %d, want %d", name, rc, exitError)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("%s: stderr %q, want it to say %q", name, stderr.String(), tc.want)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%s: the output was written", name)
		}
	}
}
