package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// indexedCMYKCLIPDF builds a one-page document showing /Im1, a 2x1 indexed
// image over DeviceCMYK whose pixels are both full cyan, over
// (50,600)-(250,700): --region 1:60,610,80,80 covers the left pixel only.
func indexedCMYKCLIPDF() []byte {
	body := "q 200 0 0 100 50 600 cm /Im1 Do Q"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace [/Indexed /DeviceCMYK 0 <FF000000>]" +
		" /BitsPerComponent 8 /Length 2 >>\nstream\n\x00\x00\nendstream\nendobj\n" +
		"trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n")
}

// writeWhiteCMYKProfile writes litho's built-in CMYK profile with the output
// tables of its AToB0 table flattened to L* 100, so every colour reads as
// white, and returns its path.
func writeWhiteCMYKProfile(t *testing.T) string {
	t.Helper()
	profile, err := os.ReadFile(filepath.Join("..", "..", "pkg", "internal", "graphics", "icc", "profiles", "CGATS001Compat-v2-micro.icc"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range int(binary.BigEndian.Uint32(profile[128:])) {
		entry := profile[132+12*i:]
		if string(entry[:4]) != "A2B0" {
			continue
		}
		tag := profile[binary.BigEndian.Uint32(entry[4:]):]
		in, out, grid := int(tag[8]), int(tag[9]), int(tag[10])
		inEntries, outEntries := int(binary.BigEndian.Uint16(tag[48:])), int(binary.BigEndian.Uint16(tag[50:]))
		points := 1
		for range in {
			points *= grid
		}
		pos := 52 + 2*in*inEntries + 2*points*out
		for c := range out {
			v := uint16(0x8000)
			if c == 0 {
				v = 0xFF00
			}
			for range outEntries {
				binary.BigEndian.PutUint16(tag[pos:], v)
				pos += 2
			}
		}
		path := filepath.Join(t.TempDir(), "white.icc")
		if err := os.WriteFile(path, profile, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Fatal("no AToB0 table")
	return ""
}

// TestRedact_CMYKProfile pins --cmyk-profile: redaction re-encodes an indexed
// CMYK image as RGB, and the pixel it leaves is read through the profile the
// flag names, else through the built-in one.
func TestRedact_CMYKProfile(t *testing.T) {
	t.Parallel()

	src := writeTempPDF(t, indexedCMYKCLIPDF())
	for _, tc := range []struct {
		name string
		args []string
		want [3]uint32
	}{
		{"the built-in profile", nil, [3]uint32{0, 174, 239}},
		{"the profile the flag names", []string{"--cmyk-profile", writeWhiteCMYKProfile(t)}, [3]uint32{255, 255, 255}},
	} {
		out := filepath.Join(t.TempDir(), "redacted.pdf")
		args := append(append([]string{"redact"}, tc.args...), "--region", "1:60,610,80,80", "-o", out, src)
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
			t.Fatalf("%s: exit %d, stderr: %s", tc.name, rc, stderr.String())
		}
		r, err := pdf.OpenFile(context.Background(), out)
		if err != nil {
			t.Fatalf("%s: opening the output: %v", tc.name, err)
		}
		img, err := r.DecodeImage(context.Background(), 1, "Im1")
		if err != nil {
			t.Fatalf("%s: DecodeImage: %v", tc.name, err)
		}
		cr, cg, cb, _ := img.At(1, 0).RGBA()
		if got := [3]uint32{cr >> 8, cg >> 8, cb >> 8}; got != tc.want {
			t.Errorf("%s: the pixel left is %v, want %v", tc.name, got, tc.want)
		}
		if err := r.Close(); err != nil {
			t.Errorf("%s: Close: %v", tc.name, err)
		}
	}
}

// TestRedact_CMYKProfileRefusals pins that a profile the flag cannot read, or
// one litho cannot read CMYK through, fails the command before it writes.
func TestRedact_CMYKProfileRefusals(t *testing.T) {
	t.Parallel()

	src := writeTempPDF(t, indexedCMYKCLIPDF())
	garbage := filepath.Join(t.TempDir(), "garbage.icc")
	if err := os.WriteFile(garbage, []byte("not a profile"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		path, want string
	}{
		"a missing file": {filepath.Join(t.TempDir(), "missing.icc"), "reading the CMYK profile"},
		"not a profile":  {garbage, "invalid default CMYK profile"},
	} {
		out := filepath.Join(t.TempDir(), "redacted.pdf")
		var stdout, stderr bytes.Buffer
		rc := run(context.Background(), []string{"redact", "--cmyk-profile", tc.path, "--region", "1:60,610,80,80", "-o", out, src}, &stdout, &stderr)
		if rc != exitError || !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("%s: exit %d, stderr %q; want %d naming %q", name, rc, stderr.String(), exitError, tc.want)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("%s: an output was written: %v", name, err)
		}
	}
}
