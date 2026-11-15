package pdf

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"testing"
)

// grainyPhotoPDF builds a one-page document drawing /Im0, a 300 x 300 RGB
// image with grain, stored without compression, one inch square: 300 pixels
// an inch.
func grainyPhotoPDF() []byte {
	const side = 300
	samples := make([]byte, 0, side*side*3)
	seed := uint32(7)
	for y := range side {
		for x := range side {
			for k := range 3 {
				seed = seed*1664525 + 1013904223
				samples = append(samples, byte(x+y/2+k*60+int(seed>>27)))
			}
		}
	}
	data := oneImagePDF(side, side, "", samples)
	// oneImagePDF draws the image one point a pixel: draw it in one inch,
	// padded to the length the stream declares.
	from := fmt.Sprintf("q %d 0 0 %d 0 0 cm", side, side)
	to := fmt.Sprintf("%-*s", len(from), "q 72 0 0 72 0 0 cm")
	return bytes.Replace(data, []byte(from), []byte(to), 1)
}

// rewriteWithImageOptions rewrites data in full with opts.
func rewriteWithImageOptions(t *testing.T, data []byte, opts ...WriteOption) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := NewWriter(openRecompressSource(t, data)).WriteTo(context.Background(), &out, append([]WriteOption{WithFullRewrite()}, opts...)...); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// TestWriter_WithMaxImageDPI_DownsamplesToTheTarget is the end-to-end
// contract: an image drawn at 300 dpi is written at 150 dpi, as JPEG with
// [WithImageQuality], in fewer bytes, the same bytes on every run.
func TestWriter_WithMaxImageDPI_DownsamplesToTheTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := grainyPhotoPDF()
	plain := rewriteWithImageOptions(t, data)
	reduced := rewriteWithImageOptions(t, data, WithMaxImageDPI(150), WithImageQuality(75))
	if !bytes.Equal(reduced, rewriteWithImageOptions(t, data, WithMaxImageDPI(150), WithImageQuality(75))) {
		t.Fatal("two runs of the same write differ")
	}
	if len(reduced) >= len(plain)/4 {
		t.Fatalf("reduced output is %d bytes against %d", len(reduced), len(plain))
	}
	img, err := openRecompressSource(t, reduced).DecodeImage(ctx, 1, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 150 || b.Dy() != 150 {
		t.Fatalf("image is %dx%d, want 150x150", b.Dx(), b.Dy())
	}
	if !bytes.Contains(reduced, []byte("/DCTDecode")) {
		t.Fatal("the reduced image is not a JPEG")
	}
}

// TestWriter_ImageOptionsOutOfRange: a quality below 1 is read as 1 and one
// above 100 as 100; a resolution that is not positive and finite asks for no
// maximum and changes no byte.
func TestWriter_ImageOptionsOutOfRange(t *testing.T) {
	t.Parallel()
	data := grainyPhotoPDF()
	plain := rewriteWithImageOptions(t, data)
	for _, dpi := range []float64{0, -72, math.NaN(), math.Inf(1)} {
		if !bytes.Equal(rewriteWithImageOptions(t, data, WithMaxImageDPI(dpi)), plain) {
			t.Errorf("WithMaxImageDPI(%v) changed the output", dpi)
		}
	}
	if !bytes.Equal(rewriteWithImageOptions(t, data, WithImageQuality(-5)), rewriteWithImageOptions(t, data, WithImageQuality(1))) {
		t.Error("quality -5 is not read as 1")
	}
	if !bytes.Equal(rewriteWithImageOptions(t, data, WithImageQuality(1000)), rewriteWithImageOptions(t, data, WithImageQuality(100))) {
		t.Error("quality 1000 is not read as 100")
	}
}

// TestWriter_ImageOptionsSkippedOnAnUpdate: an incremental update carries no
// compaction, and [WithCompactionSkipped] says so for the two image options.
func TestWriter_ImageOptionsSkippedOnAnUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, opt := range []WriteOption{WithImageQuality(50), WithMaxImageDPI(96)} {
		w := NewWriter(openRecompressSource(t, grainyPhotoPDF()))
		if err := w.RotatePages(90); err != nil {
			t.Fatal(err)
		}
		var skipped bool
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out, opt, WithCompactionSkipped(&skipped)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if !skipped {
			t.Error("an incremental update with an image option did not report the compaction skipped")
		}
	}
}

// TestPipeline_ImageOptionsReachTheLastWrite: a pipeline's last write carries
// the image options, after a Split too.
func TestPipeline_ImageOptionsReachTheLastWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := grainyPhotoPDF()
	write := func(opts ...WriteOption) []byte {
		t.Helper()
		var out bytes.Buffer
		p := NewPipeline().Open(openRecompressSource(t, data)).Split([]int{1})
		if _, err := p.WriteTo(ctx, &out, opts...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	plain, reduced := write(), write(WithMaxImageDPI(150))
	if len(reduced) >= len(plain) {
		t.Fatalf("pipeline output is %d bytes with the option and %d without", len(reduced), len(plain))
	}
	img, err := openRecompressSource(t, reduced).DecodeImage(ctx, 1, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 150 || b.Dy() != 150 {
		t.Fatalf("image is %dx%d, want 150x150", b.Dx(), b.Dy())
	}
}

// TestWriter_ImageOptionsApplyToEncryptedOutput: the pass runs before
// encryption, so an encrypted output is reduced too and its image decodes,
// under the password, at the reduced size.
func TestWriter_ImageOptionsApplyToEncryptedOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := grainyPhotoPDF()
	enc := WithEncryption(EncryptConfig{UserPassword: "u", OwnerPassword: "o"})
	plain := rewriteWithImageOptions(t, data, enc)
	reduced := rewriteWithImageOptions(t, data, enc, WithMaxImageDPI(150), WithImageQuality(75))
	if len(reduced) >= len(plain)/4 {
		t.Fatalf("encrypted output is %d bytes with the options and %d without", len(reduced), len(plain))
	}
	img, err := openRecompressSource(t, reduced, WithPassword("u")).DecodeImage(ctx, 1, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 150 || b.Dy() != 150 {
		t.Fatalf("image is %dx%d, want 150x150", b.Dx(), b.Dy())
	}
}
