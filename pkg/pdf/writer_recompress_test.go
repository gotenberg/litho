package pdf

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"slices"
	"testing"
)

// gradientImagePDF builds a one-page document showing /Im0, a 64x48 RGB
// gradient stored without compression, which a PNG-predicted Flate encoding
// shrinks many times over.
func gradientImagePDF() ([]byte, []byte) {
	const w, h = 64, 48
	samples := make([]byte, 0, w*h*3)
	for y := range h {
		for x := range w {
			samples = append(samples, byte(x*3+y), byte(x*5), byte(y*7))
		}
	}
	return oneImagePDF(w, h, "", samples), samples
}

// jpegImagePDF builds a one-page document showing /Im0, a 64x48 RGB gradient
// stored as DCTDecode data image/jpeg encoded with the example Huffman tables
// of ITU-T T.81, Annex K.3, and returns it with that data.
func jpegImagePDF(t *testing.T) ([]byte, []byte) {
	t.Helper()
	const w, h = 64, 48
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: byte(x*3 + y), G: byte(x * 5), B: byte(y * 7), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return oneImagePDF(w, h, "/Filter /DCTDecode ", buf.Bytes()), buf.Bytes()
}

// oneImagePDF builds a one-page document showing /Im0, a w x h DeviceRGB image
// whose stream holds data under the filter entries given.
func oneImagePDF(w, h int, filter string, data []byte) []byte {
	content := fmt.Sprintf("q %d 0 0 %d 0 0 cm /Im0 Do Q", w, h)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 %s/Length %d >>\nstream\n%s\nendstream",
			w, h, filter, len(data), data),
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

func openRecompressSource(t *testing.T, data []byte, opts ...OpenOption) *Reader {
	t.Helper()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Logf("closing reader: %v", err)
		}
	})
	return r
}

// TestWriter_WithImageRecompression_KeepsPixels is the end-to-end contract:
// the option shrinks the output, the image decodes to the same pixels, and two
// runs give the same bytes.
func TestWriter_WithImageRecompression_KeepsPixels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, samples := gradientImagePDF()

	write := func(opts ...WriteOption) []byte {
		t.Helper()
		var out bytes.Buffer
		if err := NewWriter(openRecompressSource(t, data)).WriteTo(ctx, &out, append([]WriteOption{WithFullRewrite()}, opts...)...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	plain := write()
	packed := write(WithImageRecompression())
	again := write(WithImageRecompression())

	if len(packed) >= len(plain)-len(samples)/2 {
		t.Fatalf("recompressed output is %d bytes against %d without the option", len(packed), len(plain))
	}
	if !bytes.Equal(packed, again) {
		t.Fatal("two runs of the same write differ")
	}

	before, err := openRecompressSource(t, data).DecodeImage(ctx, 1, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage on the source: %v", err)
	}
	after, err := openRecompressSource(t, packed).DecodeImage(ctx, 1, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage on the output: %v", err)
	}
	if !slices.Equal(levels(before), levels(after)) {
		t.Fatal("recompression changed the image's pixels")
	}
}

// TestWriter_WithImageRecompression_HonoursImageBudget lowers the image budget
// through WithLimits below the image's pixel count, which must leave the
// image's stored samples as they were.
func TestWriter_WithImageRecompression_HonoursImageBudget(t *testing.T) {
	t.Parallel()
	data, samples := gradientImagePDF()
	r := openRecompressSource(t, data, WithLimits(Limits{MaxImagePixels: 64*48 - 1}))
	var out bytes.Buffer
	if err := NewWriter(r).WriteTo(context.Background(), &out, WithFullRewrite(), WithImageRecompression()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), samples) {
		t.Fatal("an image past the image budget was recompressed")
	}
}

// TestWriter_WithImageRecompression_DeclinesEncryptedOutput checks that an
// encrypted output keeps its images as they were. Encryption's keys and salts
// are random but its lengths are not, so the option must not change the size.
func TestWriter_WithImageRecompression_DeclinesEncryptedOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, _ := gradientImagePDF()
	write := func(opts ...WriteOption) []byte {
		t.Helper()
		var out bytes.Buffer
		opts = append(opts, WithEncryption(EncryptConfig{UserPassword: "u", OwnerPassword: "o"}))
		if err := NewWriter(openRecompressSource(t, data)).WriteTo(ctx, &out, opts...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	plain, asked := write(), write(WithImageRecompression())
	if len(plain) != len(asked) {
		t.Fatalf("encrypted output is %d bytes with the option and %d without", len(asked), len(plain))
	}
	if _, err := openRecompressSource(t, asked, WithPassword("u")).DecodeImage(ctx, 1, "Im0"); err != nil {
		t.Fatalf("DecodeImage on the encrypted output: %v", err)
	}
}

// TestWriter_WithImageRecompression_RecodesJPEG checks that a JPEG image keeps
// DCTDecode, shrinks, decodes to the same pixels, and is written the same way
// twice.
func TestWriter_WithImageRecompression_RecodesJPEG(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, jpegData := jpegImagePDF(t)

	write := func(opts ...WriteOption) []byte {
		t.Helper()
		var out bytes.Buffer
		if err := NewWriter(openRecompressSource(t, data)).WriteTo(ctx, &out, append([]WriteOption{WithFullRewrite()}, opts...)...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	plain := write()
	recoded := write(WithImageRecompression())
	again := write(WithImageRecompression())

	if !bytes.Contains(plain, jpegData) || bytes.Contains(recoded, jpegData) {
		t.Fatal("the JPEG data was not re-coded")
	}
	if len(recoded) >= len(plain) || !bytes.Contains(recoded, []byte("/DCTDecode")) {
		t.Fatalf("re-coded output is %d bytes against %d, or lost DCTDecode", len(recoded), len(plain))
	}
	if !bytes.Equal(recoded, again) {
		t.Fatal("two runs of the same write differ")
	}

	before, err := openRecompressSource(t, data).DecodeImage(ctx, 1, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage on the source: %v", err)
	}
	after, err := openRecompressSource(t, recoded).DecodeImage(ctx, 1, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage on the output: %v", err)
	}
	if !slices.Equal(levels(before), levels(after)) {
		t.Fatal("re-coding changed the image's pixels")
	}
}

// TestWriter_WithImageRecompression_IndexesFewColours checks that an RGB image
// of few colours is written as an Indexed image that decodes to the same
// pixels, with or without a colour-key mask, and that two runs give the same
// bytes. DecodeImage does not apply a colour key, so the key's translation is
// proved in the writer package's own tests.
func TestWriter_WithImageRecompression_IndexesFewColours(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const w, h = 120, 40
	samples := make([]byte, 0, w*h*3)
	for y := range h {
		for x := range w {
			c := (x/20 + y/10) % 5
			samples = append(samples, byte(50*c), byte(200-40*c), byte(17*c))
		}
	}
	for _, extra := range []string{"", "/Mask [100 160 0 255 0 255] "} {
		data := oneImagePDF(w, h, extra, samples)
		write := func() []byte {
			t.Helper()
			var out bytes.Buffer
			if err := NewWriter(openRecompressSource(t, data)).WriteTo(ctx, &out, WithFullRewrite(), WithImageRecompression()); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			return out.Bytes()
		}
		packed, again := write(), write()
		if !bytes.Contains(packed, []byte("/Indexed")) || len(packed) >= len(data) {
			t.Fatalf("mask %q: output is %d bytes against %d, or holds no Indexed space", extra, len(packed), len(data))
		}
		if !bytes.Equal(packed, again) {
			t.Fatal("two runs of the same write differ")
		}
		before, err := openRecompressSource(t, data).DecodeImage(ctx, 1, "Im0")
		if err != nil {
			t.Fatalf("DecodeImage on the source: %v", err)
		}
		after, err := openRecompressSource(t, packed).DecodeImage(ctx, 1, "Im0")
		if err != nil {
			t.Fatalf("DecodeImage on the output: %v", err)
		}
		b := before.Bounds()
		if b != after.Bounds() {
			t.Fatalf("bounds %v became %v", b, after.Bounds())
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r0, g0, b0, a0 := before.At(x, y).RGBA()
				r1, g1, b1, a1 := after.At(x, y).RGBA()
				if r0 != r1 || g0 != g1 || b0 != b1 || a0 != a1 {
					t.Fatalf("mask %q: pixel (%d,%d) changed", extra, x, y)
				}
			}
		}
	}
}
