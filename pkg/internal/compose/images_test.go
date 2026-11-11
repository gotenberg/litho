package compose_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// oneShot returns data compressed in one write, as the backend compressed
// a decoded picture before sources were encoded when met.
func oneShot(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zlib.NewWriter(&buf)
	if _, err := z.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestEncodeImageMatchesOneShot locks that a picture encoded row by row
// holds the bytes the whole picture compressed at once gives, so the file
// does not depend on when a picture was encoded.
func TestEncodeImageMatchesOneShot(t *testing.T) {
	t.Parallel()
	const w, h = 97, 61
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	rgba := image.NewRGBA(image.Rect(3, 5, 3+w, 5+h))
	gray := image.NewGray(image.Rect(0, 0, w, h))
	var rgb, alpha, opaqueRGB, grays []byte
	for y := range h {
		for x := range w {
			c := color.NRGBA{R: uint8(x * 3), G: uint8(y * 5), B: uint8(x ^ y), A: uint8(255 - (x+y)%7)} //nolint:gosec // test values wrap on purpose
			nrgba.SetNRGBA(x, y, c)
			rgb = append(rgb, c.R, c.G, c.B)
			alpha = append(alpha, c.A)
			o := color.RGBA{R: uint8(x), G: uint8(y), B: 7, A: 255} //nolint:gosec // test values under 256
			rgba.SetRGBA(3+x, 5+y, o)
			opaqueRGB = append(opaqueRGB, o.R, o.G, o.B)
			g := uint8(x + y) //nolint:gosec // test values under 256
			gray.SetGray(x, y, color.Gray{Y: g})
			grays = append(grays, g)
		}
	}
	for _, tc := range []struct {
		name    string
		img     image.Image
		samples []byte
		alpha   []byte
		gray    bool
	}{
		{"translucent NRGBA", nrgba, rgb, alpha, false},
		{"opaque RGBA off the origin", rgba, opaqueRGB, nil, false},
		{"gray", gray, grays, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src, err := compose.EncodeImage(context.Background(), tc.img)
			if err != nil {
				t.Fatal(err)
			}
			if src.Width != w || src.Height != h || src.Gray != tc.gray || src.JPEG != nil {
				t.Fatalf("source %d by %d gray %v", src.Width, src.Height, src.Gray)
			}
			if !bytes.Equal(src.Samples, oneShot(t, tc.samples)) {
				t.Error("samples differ from the one-shot compression")
			}
			switch {
			case tc.alpha == nil && src.Alpha != nil:
				t.Error("an opaque picture carries an alpha")
			case tc.alpha != nil && !bytes.Equal(src.Alpha, oneShot(t, tc.alpha)):
				t.Error("alpha differs from the one-shot compression")
			}
		})
	}
}

// TestEncodeImageOpaqueByPixels locks that a picture whose type cannot
// say it is opaque, but whose pixels all are, carries no alpha.
func TestEncodeImageOpaqueByPixels(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	src, err := compose.EncodeImage(context.Background(), struct{ image.Image }{img})
	if err != nil {
		t.Fatal(err)
	}
	if src.Alpha != nil {
		t.Error("an opaque picture carries an alpha")
	}
}

func TestImageSourceDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 5, 3))
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 17) //nolint:gosec // test values wrap on purpose
	}
	src, err := compose.EncodeImage(context.Background(), img)
	if err != nil {
		t.Fatal(err)
	}
	got, err := src.Decode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n, ok := got.(*image.NRGBA)
	if !ok || !bytes.Equal(n.Pix, img.Pix) || n.Rect != img.Rect {
		t.Errorf("decoded %T %v, want the pixels encoded", got, got.Bounds())
	}

	gray := image.NewGray(image.Rect(0, 0, 3, 2))
	copy(gray.Pix, []byte{1, 2, 3, 4, 5, 6})
	src, err = compose.EncodeImage(context.Background(), gray)
	if err != nil {
		t.Fatal(err)
	}
	got, err = src.Decode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := got.(*image.Gray); !ok || !bytes.Equal(g.Pix, gray.Pix) {
		t.Errorf("decoded %T, want the gray pixels encoded", got)
	}

	short := &compose.ImageSource{Width: 9, Height: 9, Samples: oneShot(t, []byte{1, 2, 3})}
	if _, err := short.Decode(context.Background()); !errors.Is(err, compose.ErrInvalidImage) {
		t.Errorf("short samples: %v, want ErrInvalidImage", err)
	}
	long := &compose.ImageSource{Width: 1, Height: 1, Gray: true, Samples: oneShot(t, []byte{1, 2})}
	if _, err := long.Decode(context.Background()); !errors.Is(err, compose.ErrInvalidImage) {
		t.Errorf("long samples: %v, want ErrInvalidImage", err)
	}
}

// boundsOnly reports bounds without holding pixels.
type boundsOnly image.Rectangle

func (boundsOnly) ColorModel() color.Model   { return color.RGBAModel }
func (b boundsOnly) Bounds() image.Rectangle { return image.Rectangle(b) }
func (boundsOnly) At(int, int) color.Color   { return color.Black }

func TestEncodeImageRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, err := compose.EncodeImage(ctx, nil); !errors.Is(err, compose.ErrInvalidImage) {
		t.Errorf("nil: %v", err)
	}
	if _, err := compose.EncodeImage(ctx, boundsOnly(image.Rect(0, 0, 0, 4))); !errors.Is(err, compose.ErrInvalidImage) {
		t.Errorf("empty: %v", err)
	}
	if _, err := compose.EncodeImage(ctx, boundsOnly(image.Rect(0, 0, 1<<16, 1<<15))); !errors.Is(err, compose.ErrLimitExceeded) {
		t.Errorf("huge: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := compose.EncodeImage(canceled, image.NewNRGBA(image.Rect(0, 0, 2, 2))); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}

func FuzzJPEGImage(f *testing.F) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 3, 2)), nil); err != nil {
		f.Fatal(err)
	}
	f.Add(buf.Bytes())
	f.Add(buf.Bytes()[:20])
	f.Fuzz(func(t *testing.T, data []byte) {
		src, err := compose.JPEGImage(context.Background(), data)
		if err != nil {
			if !errors.Is(err, compose.ErrInvalidImage) && !errors.Is(err, compose.ErrLimitExceeded) {
				t.Fatalf("unwrapped error %v", err)
			}
			return
		}
		if src.Width <= 0 || src.Height <= 0 || !bytes.Equal(src.JPEG, data) {
			t.Fatalf("source %d by %d", src.Width, src.Height)
		}
	})
}

// TestEncodeImageBudget pins the picture's two charges: its decoded pixels,
// released once it is encoded, then its compressed samples, held.
func TestEncodeImageBudget(t *testing.T) {
	t.Parallel()
	ctx, mem, done := limits.WithBudget(context.Background(), limits.Limits{})
	defer done()
	if err := compose.ChargeImage(ctx, 100, 100, color.NRGBAModel); err != nil || mem.Held() != 40_000 {
		t.Fatalf("charge: %v, held %d", err, mem.Held())
	}
	src, err := compose.EncodeImage(ctx, image.NewNRGBA(image.Rect(0, 0, 100, 100)))
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(len(src.Samples) + len(src.Alpha)); mem.Held() != want {
		t.Errorf("held %d after encoding, want the %d compressed bytes", mem.Held(), want)
	}
	img, err := src.Decode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	compose.ReleaseImage(ctx, img)
	if want := int64(len(src.Samples) + len(src.Alpha)); mem.Held() != want {
		t.Errorf("held %d after a decode released, want %d", mem.Held(), want)
	}
	small, _, done2 := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 1000})
	defer done2()
	if _, err := src.Decode(small); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("decode past the budget: %v", err)
	}
}
