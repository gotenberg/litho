package filter

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
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

		got, err := decodeDCT(buf.Bytes(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// JPEG is lossy — verify we get 4 pixels (1 byte each for gray).
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

		got, err := decodeDCT(buf.Bytes(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// YCbCr decoded to RGB: 4 pixels × 3 bytes.
		if len(got) != 12 {
			t.Fatalf("expected 12 bytes (2x2 RGB), got %d", len(got))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		_, err := decodeDCT(nil, nil)
		if err == nil {
			t.Fatal("expected error for empty input")
		}
	})

	t.Run("corrupt input", func(t *testing.T) {
		_, err := decodeDCT([]byte{0xFF, 0xD8, 0x00, 0x01}, nil)
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
		_, err := decodeDCT(buf.Bytes(), parms)
		if err != nil {
			t.Fatalf("unexpected error with DecodeParms: %v", err)
		}
	})
}
