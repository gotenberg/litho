package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// TestCompactFlag_RecompressesImagesLosslessly checks that --compact
// recompresses an uncompressed image, and that the image still decodes to the
// same pixels as without the flag.
func TestCompactFlag_RecompressesImagesLosslessly(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.pdf")
	if err := os.WriteFile(src, imageCompactFixturePDF(), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	plain := runDeleteTo(t, src)
	compact := runDeleteTo(t, src, "--compact")
	if bytes.Contains(plain, []byte("/Predictor")) {
		t.Fatal("precondition: the plain rewrite already predicts the image")
	}
	if !bytes.Contains(compact, []byte("/Predictor 15")) {
		t.Error("--compact did not recompress the image with a PNG predictor")
	}
	if len(compact) >= len(plain)/2 {
		t.Errorf("--compact output is %d bytes against %d without it", len(compact), len(plain))
	}

	pixels := func(data []byte) []byte {
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
	if !bytes.Equal(pixels(plain), pixels(compact)) {
		t.Error("--compact changed the image's pixels")
	}
}
