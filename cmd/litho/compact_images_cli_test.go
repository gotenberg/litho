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

// imageCompactFixturePDF builds a two-page document whose second page shows
// /Im0, a 64x48 RGB gradient stored without compression.
func imageCompactFixturePDF() []byte {
	const w, h = 64, 48
	samples := make([]byte, 0, w*h*3)
	for y := range h {
		for x := range w {
			samples = append(samples, byte(x*3+y), byte(x*5), byte(y*7))
		}
	}
	const content = "q 64 0 0 48 0 0 cm /Im0 Do Q"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length %d >>\nstream\n%s\nendstream",
			w, h, len(samples), samples),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// imageFixtureFile writes imageCompactFixturePDF to a temp file.
func imageFixtureFile(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "in.pdf")
	if err := os.WriteFile(src, imageCompactFixturePDF(), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return src
}

// imagePixels returns the 8-bit RGBA levels of /Im0 on the first page of data.
func imagePixels(t *testing.T, data []byte) []byte {
	t.Helper()
	ctx := context.Background()
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Logf("closing reader: %v", err)
		}
	}()
	img, err := r.DecodeImage(ctx, 1, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	var out []byte
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			out = append(out, byte(r>>8), byte(g>>8), byte(bl>>8), byte(a>>8))
		}
	}
	return out
}

// predicted reports whether data holds an image recompressed under a PNG
// predictor, which neither the fixture nor a plain rewrite carries.
func predicted(data []byte) bool {
	return bytes.Contains(data, []byte("/Predictor 15"))
}

// TestRecompressImagesFlag_IndependentOfCompact pins the contract of the two
// flags: --compact never recompresses images, whose cost in time is too high
// for it, --recompress-images does on its own, and the two combine. The image
// decodes to the same pixels whichever is given. The fixture is too small for
// packing to pay, so packing itself is left to the --compact tests.
func TestRecompressImagesFlag_IndependentOfCompact(t *testing.T) {
	src := imageFixtureFile(t)

	plain := runDeleteTo(t, src)
	if predicted(plain) {
		t.Fatal("precondition: the plain rewrite already predicts the image")
	}
	want := imagePixels(t, plain)

	cases := []struct {
		flags     []string
		predicted bool
	}{
		{[]string{"--compact"}, false},
		{[]string{"--recompress-images"}, true},
		{[]string{"--compact", "--recompress-images"}, true},
	}
	for _, tc := range cases {
		out := runDeleteTo(t, src, tc.flags...)
		if got := predicted(out); got != tc.predicted {
			t.Errorf("%v: recompressed the image = %v, want %v", tc.flags, got, tc.predicted)
		}
		if tc.predicted && len(out) >= len(plain)/2 {
			t.Errorf("%v: output is %d bytes against %d without flags", tc.flags, len(out), len(plain))
		}
		if !bytes.Equal(imagePixels(t, out), want) {
			t.Errorf("%v: changed the image's pixels", tc.flags)
		}
	}
}

// TestPipe_RecompressImagesFlag checks the pipe global behaves as the
// standalone flag does, alone and beside --compact.
func TestPipe_RecompressImagesFlag(t *testing.T) {
	src := imageFixtureFile(t)
	run := func(extra ...string) []byte {
		t.Helper()
		out := filepath.Join(t.TempDir(), "piped.pdf")
		args := append([]string{"pipe", src, "delete", "-pages", "1"}, extra...)
		args = append(args, "-o", out)
		rc, errOut := runPipeArgs(t, args)
		if rc != exitOK {
			t.Fatalf("pipe %v: exit %d, stderr: %s", extra, rc, errOut)
		}
		data, err := os.ReadFile(out) //nolint:gosec // the test's own temp path
		if err != nil {
			t.Fatalf("reading output: %v", err)
		}
		return data
	}
	if out := run("--compact"); predicted(out) {
		t.Error("pipe --compact recompressed the image")
	}
	if out := run("--recompress-images"); !predicted(out) {
		t.Error("pipe --recompress-images did not recompress the image")
	}
	if out := run("--recompress-images", "--compact"); !predicted(out) {
		t.Error("pipe --recompress-images --compact did not recompress the image")
	}
}

// TestRecompressImagesFlag_EveryCompactCommand checks that every command that
// accepts --compact also accepts --recompress-images.
func TestRecompressImagesFlag_EveryCompactCommand(t *testing.T) {
	for _, cmd := range []string{"delete", "insert", "encrypt", "decrypt", "flatten-form", "split", "merge", "remediate"} {
		var stdout, stderr bytes.Buffer
		run(context.Background(), []string{cmd, "--recompress-images"}, &stdout, &stderr)
		if strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Errorf("%s does not accept --recompress-images: %s", cmd, stderr.String())
		}
	}
}

// TestRecompressImagesFlag_WarnsOnEncryptedOutput checks that asking to
// recompress an output the command encrypts says why nothing was recompressed.
func TestRecompressImagesFlag_WarnsOnEncryptedOutput(t *testing.T) {
	src := imageFixtureFile(t)
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	args := []string{"encrypt", "--user-password", "u", "--recompress-images", "-o", out, src}
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("encrypt: exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--recompress-images does not apply to an encrypted output") {
		t.Errorf("no warning on stderr: %q", stderr.String())
	}
}
