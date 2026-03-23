package filter

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// decodeDCT implements the DCTDecode filter (PDF spec 7.4.8).
// It decodes JPEG data and returns raw pixel bytes:
//   - Gray:        1 byte per pixel
//   - RGB/YCbCr:   3 bytes per pixel (R, G, B order)
//   - CMYK:        4 bytes per pixel
func decodeDCT(data []byte, parms ast.PdfDict) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("dct decode: empty input")
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("dct decode: %w", err)
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	switch m := img.(type) {
	case *image.Gray:
		out := make([]byte, 0, w*h)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			off := m.PixOffset(bounds.Min.X, y)
			out = append(out, m.Pix[off:off+w]...)
		}
		return out, nil

	case *image.CMYK:
		out := make([]byte, 0, w*h*4)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			off := m.PixOffset(bounds.Min.X, y)
			out = append(out, m.Pix[off:off+w*4]...)
		}
		return out, nil

	case *image.YCbCr:
		out := make([]byte, 0, w*h*3)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, _ := m.At(x, y).RGBA()
				out = append(out, uint8(r>>8), uint8(g>>8), uint8(b>>8)) //nolint:gosec // RGBA() returns [0,0xFFFF]; >>8 fits uint8
			}
		}
		return out, nil

	default:
		// Generic fallback: convert pixel-by-pixel to RGB.
		out := make([]byte, 0, w*h*3)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				out = append(out, uint8(r>>8), uint8(g>>8), uint8(b>>8)) //nolint:gosec // RGBA() returns [0,0xFFFF]; >>8 fits uint8
			}
		}
		return out, nil
	}
}
