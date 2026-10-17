package filter

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
)

func TestDecodeDCT(t *testing.T) {
	t.Run("2x2 gray round-trip", func(t *testing.T) {
		img := image.NewGray(image.Rect(0, 0, 2, 2))
		img.SetGray(0, 0, color.Gray{Y: 10})
		img.SetGray(1, 0, color.Gray{Y: 20})
		img.SetGray(0, 1, color.Gray{Y: 30})
		img.SetGray(1, 1, color.Gray{Y: 40})

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}

		got, err := decodeDCT(context.Background(), buf.Bytes(), nil, nil, dct.DefaultMaxSamples)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// JPEG is lossy. Verify the decode yields 4 pixels (1 byte each for gray).
		if len(got) != 4 {
			t.Fatalf("expected 4 bytes (2x2 gray), got %d", len(got))
		}
	})

	t.Run("2x2 RGB round-trip", func(t *testing.T) {
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		img.Set(1, 0, color.RGBA{R: 0, G: 255, B: 0, A: 255})
		img.Set(0, 1, color.RGBA{R: 0, G: 0, B: 255, A: 255})
		img.Set(1, 1, color.RGBA{R: 255, G: 255, B: 0, A: 255})

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
			t.Fatal(err)
		}

		got, err := decodeDCT(context.Background(), buf.Bytes(), nil, nil, dct.DefaultMaxSamples)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// YCbCr decoded to RGB: 4 pixels × 3 bytes.
		if len(got) != 12 {
			t.Fatalf("expected 12 bytes (2x2 RGB), got %d", len(got))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		_, err := decodeDCT(context.Background(), nil, nil, nil, dct.DefaultMaxSamples)
		if err == nil {
			t.Fatal("expected error for empty input")
		}
	})

	t.Run("corrupt input", func(t *testing.T) {
		_, err := decodeDCT(context.Background(), []byte{0xFF, 0xD8, 0x00, 0x01}, nil, nil, dct.DefaultMaxSamples)
		if err == nil {
			t.Fatal("expected error for corrupt JPEG")
		}
	})

	t.Run("with DecodeParms", func(t *testing.T) {
		// Verify DecodeParms doesn't cause panic even if present.
		img := image.NewGray(image.Rect(0, 0, 1, 1))
		img.SetGray(0, 0, color.Gray{Y: 128})

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, nil); err != nil {
			t.Fatal(err)
		}

		parms := ast.PdfDict{
			"ColorTransform": ast.PdfInt(1),
		}
		_, err := decodeDCT(context.Background(), buf.Bytes(), parms, nil, dct.DefaultMaxSamples)
		if err != nil {
			t.Fatalf("unexpected error with DecodeParms: %v", err)
		}
	})
}

// TestDecodeDCT_CMYKAsStored hands on the samples an Adobe-marked CMYK JPEG
// stores. testdata/cmyk_adobe.jpg was written by Pillow from a 16x8 image whose
// left half is CMYK (10, 60, 200, 30) and right half (250, 5, 90, 160); Pillow,
// like Photoshop, stores CMYK inverted, so the data holds 255 minus each. PDF.js
// reads the same stored values: embedded without a /Decode array the image
// renders near black, and with /Decode [1 0 1 0 1 0 1 0] as the orange of the
// left half.
func TestDecodeDCT_CMYKAsStored(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/cmyk_adobe.jpg")
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeDCT(context.Background(), data, nil, nil, dct.DefaultMaxSamples)
	if err != nil {
		t.Fatalf("decodeDCT: %v", err)
	}
	if len(got) != 16*8*4 {
		t.Fatalf("%d bytes, want %d", len(got), 16*8*4)
	}
	for _, tc := range []struct {
		x    int
		want [4]int
	}{
		{0, [4]int{245, 195, 55, 225}},
		{15, [4]int{5, 250, 165, 95}},
	} {
		p := got[tc.x*4 : tc.x*4+4]
		for c := range 4 {
			// JPEG is lossy: allow the rounding of the inverse DCT.
			if d := int(p[c]) - tc.want[c]; d < -2 || d > 2 {
				t.Errorf("pixel %d = %v, want %v", tc.x, p, tc.want)
				break
			}
		}
	}
}

// TestDecodeDCT_Lenient renders the JPEGs image/jpeg refuses over damage
// other readers draw through: data cut off inside the scan, and a frame
// declaring 65535 lines of an image whose dictionary states 16.
func TestDecodeDCT_Lenient(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 64, 16))
	for x := range 64 {
		for y := range 16 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 16), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()

	sos := bytes.Index(data, []byte{0xFF, 0xDA})
	cut := data[:sos+(len(data)-sos)/2]
	if got, err := decodeDCT(context.Background(), cut, nil, nil, dct.DefaultMaxSamples); err != nil || len(got) != 64*16*3 {
		t.Errorf("truncated: %d bytes, %v; want %d", len(got), err, 64*16*3)
	}

	tall := bytes.Clone(data)
	sof := bytes.Index(tall, []byte{0xFF, 0xC0})
	tall[sof+5], tall[sof+6] = 0xFF, 0xFF
	dict := ast.PdfDict{"Height": ast.PdfInt(16)}
	if got, err := decodeDCT(context.Background(), tall, nil, dict, dct.DefaultMaxSamples); err != nil || len(got) != 64*16*3 {
		t.Errorf("placeholder height: %d bytes, %v; want %d", len(got), err, 64*16*3)
	}
}

// TestDecodeDCT_BothDecodersFail reports both refusals when neither decoder
// reads the data. The error once carried image/jpeg's alone, so a stream the
// lenient decoder also refused showed only the fault it had been called in to
// read through.
func TestDecodeDCT_BothDecodersFail(t *testing.T) {
	t.Parallel()

	_, err := decodeDCT(context.Background(), []byte("not JPEG data at all"), nil, nil, dct.DefaultMaxSamples)
	if err == nil {
		t.Fatal("decodeDCT accepted data that holds no JPEG")
	}
	var formatErr jpeg.FormatError
	if !errors.As(err, &formatErr) {
		t.Errorf("error %q does not carry image/jpeg's refusal", err)
	}
	if !errors.Is(err, dct.ErrMalformed) {
		t.Errorf("error %q does not carry the lenient decoder's refusal", err)
	}
	if !strings.Contains(err.Error(), "lenient decoder") {
		t.Errorf("error %q does not name the lenient decoder", err)
	}
}
