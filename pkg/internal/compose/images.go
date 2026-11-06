package compose

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// image returns the XObject of src, writing it on first use: a source
// placed several times, on one page or many, is written once.
func (w *docWriter) image(ctx context.Context, src *ImageSource) (ast.PdfIndirectRef, error) {
	if ref, ok := w.images[src]; ok {
		return ref, nil
	}
	var (
		stream ast.PdfStream
		err    error
	)
	if len(src.JPEG) > 0 {
		stream, err = jpegXObject(src.JPEG)
	} else {
		stream, err = w.pictureXObject(ctx, src.Image)
	}
	if err != nil {
		return ast.PdfIndirectRef{}, err
	}
	ref := w.alloc.Alloc(stream)
	w.images[src] = ref
	return ref, nil
}

// jpegXObject wraps a JPEG file as an image XObject decoded by DCTDecode
// (ISO 32000-1, 7.4.8 and 8.9.5), its bytes as they are. Width, height and
// colour space come from the frame header.
//
// An Adobe-marked CMYK JPEG stores its samples inverted, which DCTDecode
// hands on unchanged: it is not special-cased, so such a file renders as
// its negative. A converter that meets one decodes it and passes the
// picture as [ImageSource.Image].
func jpegXObject(data []byte) (ast.PdfStream, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ast.PdfStream{}, fmt.Errorf("%w: JPEG header: %w", ErrInvalidImage, err)
	}
	cs, err := jpegColorSpace(cfg.ColorModel)
	if err != nil {
		return ast.PdfStream{}, err
	}
	return ast.NewStream(ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(cfg.Width),
		"Height":           ast.PdfInt(cfg.Height),
		"ColorSpace":       cs,
		"BitsPerComponent": ast.PdfInt(8),
		"Filter":           ast.PdfName("DCTDecode"),
	}, data), nil
}

// jpegColorSpace maps the colour model image/jpeg reads from a frame's
// component count to the device space of that count (ISO 32000-1, 8.6.4):
// 1 to DeviceGray, 3 to DeviceRGB, 4 to DeviceCMYK.
func jpegColorSpace(m color.Model) (ast.PdfName, error) {
	switch m {
	case color.GrayModel:
		return "DeviceGray", nil
	case color.YCbCrModel, color.RGBAModel:
		return "DeviceRGB", nil
	case color.CMYKModel:
		return "DeviceCMYK", nil
	default:
		return "", fmt.Errorf("%w: JPEG colour model %T", ErrInvalidImage, m)
	}
}

// pictureXObject writes img losslessly as an 8-bit FlateDecode image
// XObject (ISO 32000-1, 8.9.5): DeviceGray for an [*image.Gray], DeviceRGB
// otherwise, with its alpha, when any pixel is not opaque, as a DeviceGray
// soft mask (11.6.5.3) the image names in /SMask. Colours are written
// straight, not premultiplied, as the soft mask expects.
func (w *docWriter) pictureXObject(ctx context.Context, img image.Image) (ast.PdfStream, error) {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	dict := func(cs ast.PdfName) ast.PdfDict {
		return ast.PdfDict{
			"Type":             ast.PdfName("XObject"),
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(width),
			"Height":           ast.PdfInt(height),
			"ColorSpace":       cs,
			"BitsPerComponent": ast.PdfInt(8),
		}
	}
	if gray, ok := img.(*image.Gray); ok {
		samples := make([]byte, 0, width*height) // fits int: at most maxImagePixels, checked by Validate
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if err := ctx.Err(); err != nil {
				return ast.PdfStream{}, fmt.Errorf("writing image: %w", err)
			}
			off := gray.PixOffset(b.Min.X, y)
			samples = append(samples, gray.Pix[off:off+width]...)
		}
		s, err := flateStream(dict("DeviceGray"), samples)
		if err != nil {
			return ast.PdfStream{}, fmt.Errorf("writing image: %w", err)
		}
		return s, nil
	}

	rgb := make([]byte, 0, 3*width*height) // fits int: at most 3*maxImagePixels, checked by Validate
	alpha := make([]byte, 0, width*height)
	translucent := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return ast.PdfStream{}, fmt.Errorf("writing image: %w", err)
		}
		for x := b.Min.X; x < b.Max.X; x++ {
			c := straight(img, x, y)
			rgb = append(rgb, c.R, c.G, c.B)
			alpha = append(alpha, c.A)
			if c.A != 0xFF {
				translucent = true
			}
		}
	}
	main := dict("DeviceRGB")
	if translucent {
		mask, err := flateStream(dict("DeviceGray"), alpha)
		if err != nil {
			return ast.PdfStream{}, fmt.Errorf("writing image mask: %w", err)
		}
		main["SMask"] = w.alloc.Alloc(mask)
	}
	s, err := flateStream(main, rgb)
	if err != nil {
		return ast.PdfStream{}, fmt.Errorf("writing image: %w", err)
	}
	return s, nil
}

// straight returns the pixel of img at (x, y) with straight alpha, reading
// an [*image.NRGBA] directly.
func straight(img image.Image, x, y int) color.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n.NRGBAAt(x, y)
	}
	r, g, b, a := img.At(x, y).RGBA()
	if a == 0 {
		return color.NRGBA{}
	}
	// RGBA returns 16-bit premultiplied channels; dividing by alpha
	// restores straight ones, each at most 0xFFFF, so the shift leaves a
	// byte.
	un := func(c uint32) uint8 {
		return uint8((c * 0xFFFF / a) >> 8) //nolint:gosec // G115: c <= a, so the quotient is at most 0xFFFF and the shift leaves 8 bits
	}
	return color.NRGBA{R: un(r), G: un(g), B: un(b), A: uint8(a >> 8)} //nolint:gosec // G115: a is at most 0xFFFF, so the shift leaves 8 bits
}
