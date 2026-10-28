package filter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// probeDCT is the probe of DCTDecode (see codecProbes): the lenient decoder's
// marker segments up to the first scan, read by [dct.Check]. A JPEG that
// decodes past its headers is decoded by image/jpeg or the lenient decoder,
// and both refuse every coding the lenient decoder lacks, so the headers
// answer for decodeDCT.
func probeDCT(ctx context.Context, data []byte, _, dict ast.PdfDict, lim limits.Limits) error {
	if len(data) == 0 {
		return errors.New("dct decode: empty input")
	}
	settled, err := dct.Check(ctx, data, dctStatedHeight(dict), lim.ScaleImageCap(dct.DefaultMaxSamples))
	switch {
	case errors.Is(err, dct.ErrUnsupported):
		return fmt.Errorf("%w: DCTDecode: %w", ErrUnsupportedFilter, err)
	case err != nil:
		return fmt.Errorf("dct decode: %w", err)
	case !settled:
		return errUnsettled
	}
	return nil
}

// dctStatedHeight returns the /Height of the image dictionary dict, which
// bounds a frame that declares more lines than the image has, or 0 when it
// states none.
func dctStatedHeight(dict ast.PdfDict) int {
	if v, ok := dict["Height"].(ast.PdfInt); ok {
		return intconv.Clamp(v)
	}
	return 0
}

// decodeDCT implements the DCTDecode filter (ISO 32000-1, 7.4.8).
// It decodes JPEG data and returns raw pixel bytes:
//   - Gray:        1 byte per pixel
//   - RGB/YCbCr:   3 bytes per pixel (R, G, B order)
//   - CMYK:        4 bytes per pixel
//
// dict is the image dictionary, whose /Height bounds a frame that declares
// more lines than the image has. maxSamples caps the frame the lenient decoder
// reads, scaled from the image budget.
//
// A coding the decoders lack, lossless, arithmetic or hierarchical coding
// (ITU-T T.81, Table B.1), samples other than 8 bits (B.2.2), or a count of
// components other than 1, 3 or 4, is refused with [ErrUnsupportedFilter], as
// every other codec refuses a feature it lacks, so the chain skips the filter
// and the diagnostics list it rather than report the data malformed.
//
// # Corpus evidence
//
//   - All corpora: the lenient decoder decodes 15 of the 16 JPEGs
//     image/jpeg refuses, the 16th holding no JPEG data at all, and agrees
//     with image/jpeg, to within 4 levels, on all 6,181 it reads.
//   - All corpora: reading CMYK samples inverted rendered 670 images, 639
//     of them govdocs, as their negatives against PDF.js: white paper came
//     out black.
//   - All corpora: none of the 7,557 DCTDecode streams uses a coding the
//     decoders lack, so none is refused.
func decodeDCT(ctx context.Context, data []byte, parms, dict ast.PdfDict, maxSamples int64) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("dct decode: empty input")
	}

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		// image/jpeg refuses a whole image over damage other readers render
		// through: scan data that ends early, a component identifier the
		// frame repeats, a height given by a DNL marker. The lenient decoder
		// renders it instead, and only here, so every image image/jpeg reads
		// keeps its reading.
		colorTransform := -1
		if v, ok := parms["ColorTransform"].(ast.PdfInt); ok {
			colorTransform = intconv.Clamp(v)
		}
		out, _, _, _, lerr := dct.Decode(ctx, data, colorTransform, dctStatedHeight(dict), maxSamples)
		if lerr == nil {
			return out, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// image/jpeg refuses every coding the lenient decoder lacks as well,
		// so the lenient decoder's refusal is the stream's.
		if errors.Is(lerr, dct.ErrUnsupported) {
			return nil, fmt.Errorf("%w: DCTDecode: %w", ErrUnsupportedFilter, lerr)
		}
		// Both decoders failed, and each names a different fault: a stream
		// image/jpeg refuses over damage the lenient decoder reads through
		// fails there for a reason of its own, and that reason is the one
		// that says why litho rendered nothing.
		return nil, fmt.Errorf("dct decode: %w", errors.Join(
			fmt.Errorf("image/jpeg: %w", err),
			fmt.Errorf("lenient decoder: %w", lerr),
		))
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
		// embeds an inverted JPEG says so with a /Decode array.
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
