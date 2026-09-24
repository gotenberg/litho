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
func decodeDCT(data []byte, _parms ast.PdfDict) ([]byte, error) {
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
		// image/jpeg follows the convention of a stand-alone Photoshop JPEG
		// file, whose CMYK samples are stored inverted, and undoes it: every
		// channel it returns is 255 minus the sample the data holds. The
		// DCTDecode filter hands on the samples themselves: ISO 32000-1, 7.4.8
		// defers to the rules of Adobe Technical Note #5116, under which the
		// YUVK transform yields CMYK from YUV as 255 minus RGB and passes K
		// through, and applies no inversion otherwise. A PDF producer that
		// embeds an inverted JPEG says so with a /Decode array. Reading the
		// samples inverted rendered 670 corpus images, 639 of them govdocs,
		// as their negatives against PDF.js: white paper came out black.
		out := make([]byte, 0, w*h*4)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			off := m.PixOffset(bounds.Min.X, y)
			for _, v := range m.Pix[off : off+w*4] {
				out = append(out, 255-v)
			}
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
