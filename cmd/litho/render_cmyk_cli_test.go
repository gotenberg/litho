package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tiffSize returns the width and height a TIFF file writeCMYKTIFF wrote
// states, and its photometric interpretation, failing on any other layout.
func tiffSize(t *testing.T, name string) (width, height, photometric uint32) {
	t.Helper()
	data, err := os.ReadFile(name) //nolint:gosec // a file the test wrote
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte{'I', 'I', 42, 0}) {
		t.Fatalf("%s: no little-endian TIFF header", name)
	}
	le := binary.LittleEndian
	ifd := le.Uint32(data[4:])
	n := int(le.Uint16(data[ifd:]))
	for i := range n {
		e := data[int(ifd)+2+12*i:]
		v := le.Uint32(e[8:])
		if le.Uint16(e[2:]) == tiffShort {
			v = uint32(le.Uint16(e[8:]))
		}
		switch le.Uint16(e) {
		case tagImageWidth:
			width = v
		case tagImageLength:
			height = v
		case tagPhotometricInterpretation:
			photometric = v
		}
	}
	return width, height, photometric
}

// TestRender_CMYKWritesTIFF writes a CMYK render as a separated TIFF named
// .tif, and --output-profile draws for the profile it names.
func TestRender_CMYKWritesTIFF(t *testing.T) {
	src := writeTempPDF(t, minimalPDF())
	dir := t.TempDir()
	profile := filepath.Join("..", "..", "pkg", "internal", "graphics", "icc", "testdata", "v4-cmyk-lut.icc")
	for _, extra := range [][]string{nil, {"--output-profile", profile}} {
		prefix := filepath.Join(dir, "page")
		args := append([]string{"render", "--dpi", "36", "--pages", "1", "--color-model", "cmyk", "-o", prefix}, extra...)
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), append(args, src), &stdout, &stderr); rc != exitOK {
			t.Fatalf("%q: exit %d, stderr %q", extra, rc, stderr.String())
		}
		want := prefix + "-1.tif"
		if strings.TrimSpace(stdout.String()) != want {
			t.Errorf("%q: stdout %q, want %q", extra, stdout.String(), want)
		}
		// minimalPDF is US Letter: half of 612 by 792 at 36 dpi.
		if w, h, p := tiffSize(t, want); w != 306 || h != 396 || p != 5 {
			t.Errorf("%q: %dx%d photometric %d, want 306x396 separated (5)", extra, w, h, p)
		}
	}
}

// TestRender_Box draws the media box --box names.
func TestRender_Box(t *testing.T) {
	data := bytes.Replace(minimalPDF(), []byte("/MediaBox [0 0 612 792]"), []byte("/MediaBox [0 0 612 792] /CropBox [0 0 306 396]"), 1)
	if bytes.Equal(data, minimalPDF()) {
		t.Fatal("minimalPDF holds no /MediaBox [0 0 612 792] to add a crop box beside")
	}
	src := writeTempPDF(t, data)
	for box, want := range map[string]image.Point{"crop": image.Pt(153, 198), "media": image.Pt(306, 396)} {
		prefix := filepath.Join(t.TempDir(), "page")
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), []string{"render", "--dpi", "36", "--box", box, "-o", prefix, src}, &stdout, &stderr); rc != exitOK {
			t.Fatalf("%s: exit %d, stderr %q", box, rc, stderr.String())
		}
		f, err := os.Open(prefix + "-1.png")
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := png.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := image.Pt(cfg.Width, cfg.Height); got != want {
			t.Errorf("%s: %v, want %v", box, got, want)
		}
	}
}

// TestRender_CMYKRefusals pins the errors of the box and colour model flags.
func TestRender_CMYKRefusals(t *testing.T) {
	src := writeTempPDF(t, minimalPDF())
	dir := t.TempDir()
	prefix := filepath.Join(dir, "page")
	garbage := filepath.Join(dir, "garbage.icc")
	if err := os.WriteFile(garbage, []byte("not a profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args   []string
		stderr string
	}{
		{[]string{"render", "--box", "paper", "-o", prefix, src}, "unknown --box"},
		{[]string{"render", "--color-model", "lab", "-o", prefix, src}, "unknown --color-model"},
		{[]string{"render", "--output-profile", garbage, "-o", prefix, src}, "--output-profile needs --color-model cmyk"},
		{[]string{"render", "--color-model", "cmyk", "--output-profile", filepath.Join(dir, "missing.icc"), "-o", prefix, src}, "reading the output profile"},
		{[]string{"render", "--color-model", "cmyk", "--output-profile", garbage, "-o", prefix, src}, "invalid render options"},
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
