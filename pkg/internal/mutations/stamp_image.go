package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// ImageStampOptions describes a single image overlay applied by
// [StampImage]. It is the internal form of the public ImageStamp API.
type ImageStampOptions struct {
	// Data is the raw image file bytes. Supported formats are JPEG
	// (pass-through via /DCTDecode) and PNG (decoded and re-compressed
	// with /FlateDecode; any alpha channel becomes a /SMask entry on the
	// main image XObject).
	Data []byte
	// MimeType must be "image/jpeg" or "image/png".
	MimeType string
	// X and Y position the bottom-left of the image in points.
	X, Y float64
	// Width and Height set the target rectangle in points. If both are
	// zero, the image is rendered at one PDF point per pixel. If exactly
	// one is zero, it is computed to preserve the aspect ratio.
	Width, Height float64
	// Opacity is 0.0-1.0; zero is treated as fully opaque.
	Opacity float64
	// Rotation is the counter-clockwise rotation in degrees around (X, Y).
	Rotation float64
}

// StampImage draws an image overlay on each selected page.
func StampImage(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	opts ImageStampOptions,
	pageNums []int,
) error {
	if err := finiteValues(map[string]float64{
		"x": opts.X, "y": opts.Y, "width": opts.Width, "height": opts.Height,
		"opacity": opts.Opacity, "rotation": opts.Rotation,
	}); err != nil {
		return err
	}
	if len(pageNums) == 0 {
		pageNums = make([]int, len(pages))
		for i := range pages {
			pageNums[i] = i + 1
		}
	}

	var (
		imgStream ast.PdfStream
		pxW, pxH  int
		err       error
	)
	switch opts.MimeType {
	case "image/jpeg":
		imgStream, pxW, pxH, err = buildJPEGXObject(opts.Data)
	case "image/png":
		imgStream, pxW, pxH, err = buildPNGXObject(alloc, opts.Data)
	default:
		return fmt.Errorf("unsupported image mime type %q", opts.MimeType)
	}
	if err != nil {
		return fmt.Errorf("building image XObject: %w", err)
	}

	imgRef := alloc.Alloc(imgStream)
	imgResName := ast.PdfName(fmt.Sprintf("StampImg%d", imgRef.ObjectNum))

	w, h := opts.Width, opts.Height
	switch {
	case w == 0 && h == 0:
		w = float64(pxW)
		h = float64(pxH)
	case w == 0:
		w = h * float64(pxW) / float64(pxH)
	case h == 0:
		h = w * float64(pxH) / float64(pxW)
	}

	extGSName, extGSRef := buildExtGState(alloc, opts.Opacity)

	var ops strings.Builder
	ops.WriteString("q ")
	ops.WriteString(extGStateInvocation(extGSName))
	ops.WriteString(buildTransformMatrix(opts.X, opts.Y, w, h, opts.Rotation))
	// string(): PdfName.String already adds the slash, and "//Name Do" names
	// no XObject at all, so the stamp never drew.
	fmt.Fprintf(&ops, "/%s Do Q", string(imgResName))

	resources := []stampResource{
		{category: "XObject", name: imgResName, ref: imgRef},
	}
	if extGSName != "" {
		resources = append(resources, stampResource{
			category: "ExtGState", name: extGSName, ref: extGSRef,
		})
	}

	return applyStampToPages(ctx, doc, alloc, pages, pageNums, resources, ops.String())
}

// buildJPEGXObject wraps JPEG bytes as an Image XObject stream. The bytes
// are stored unchanged (the PDF /DCTDecode filter reads the same format),
// so embedding is a zero-copy operation aside from the dict wrapper.
func buildJPEGXObject(data []byte) (ast.PdfStream, int, int, error) {
	w, h, cs, err := readJPEGDimensions(data)
	if err != nil {
		return ast.PdfStream{}, 0, 0, err
	}
	var colorSpace ast.PdfName
	switch cs {
	case 1:
		colorSpace = "DeviceGray"
	case 3:
		colorSpace = "DeviceRGB"
	case 4:
		colorSpace = "DeviceCMYK"
	default:
		return ast.PdfStream{}, 0, 0, fmt.Errorf("unsupported jpeg component count %d", cs)
	}
	dict := ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(w),
		"Height":           ast.PdfInt(h),
		"ColorSpace":       colorSpace,
		"BitsPerComponent": ast.PdfInt(8),
		"Filter":           ast.PdfName("DCTDecode"),
	}
	return ast.NewStream(dict, data), w, h, nil
}

// readJPEGDimensions scans a JPEG byte stream for the SOF (Start Of Frame)
// marker to extract dimensions and component count. It supports the
// baseline and progressive SOF variants (0xC0, 0xC1, 0xC2) and skips
// unrelated segments.
func readJPEGDimensions(data []byte) (width, height, components int, err error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, 0, 0, fmt.Errorf("not a JPEG")
	}
	i := 2
	for i < len(data) {
		if data[i] != 0xFF {
			return 0, 0, 0, fmt.Errorf("invalid JPEG marker at offset %d", i)
		}
		// Skip 0xFF padding bytes.
		for i < len(data) && data[i] == 0xFF {
			i++
		}
		if i >= len(data) {
			break
		}
		marker := data[i]
		i++
		// Standalone markers with no payload.
		if marker == 0xD8 || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		if i+2 > len(data) {
			return 0, 0, 0, fmt.Errorf("truncated JPEG segment length")
		}
		segLen := int(binary.BigEndian.Uint16(data[i : i+2]))
		if i+segLen > len(data) {
			return 0, 0, 0, fmt.Errorf("truncated JPEG segment")
		}
		// SOF0, SOF1, SOF2: baseline, extended, progressive.
		if marker == 0xC0 || marker == 0xC1 || marker == 0xC2 {
			if segLen < 8 {
				return 0, 0, 0, fmt.Errorf("SOF segment too short")
			}
			// Layout: length(2) precision(1) height(2) width(2) components(1).
			height = int(binary.BigEndian.Uint16(data[i+3 : i+5]))
			width = int(binary.BigEndian.Uint16(data[i+5 : i+7]))
			components = int(data[i+7])
			return width, height, components, nil
		}
		i += segLen
	}
	return 0, 0, 0, fmt.Errorf("no SOF marker in JPEG")
}

// buildPNGXObject decodes a PNG into an Image XObject. The color channels
// become the main stream (DeviceRGB, /FlateDecode); if the source image
// has an alpha channel, it is emitted as a separate DeviceGray XObject
// referenced via /SMask for per-pixel transparency.
func buildPNGXObject(alloc *writer.ObjAllocator, data []byte) (ast.PdfStream, int, int, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return ast.PdfStream{}, 0, 0, fmt.Errorf("decoding PNG: %w", err)
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// Convert to NRGBA so we have predictable byte order.
	nrgba := image.NewNRGBA(bounds)
	draw.Draw(nrgba, bounds, img, bounds.Min, draw.Src)

	rgb := make([]byte, 0, w*h*3)
	alpha := make([]byte, 0, w*h)
	hasAlpha := false
	for y := range h {
		for x := range w {
			i := nrgba.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
			rgb = append(rgb, nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2])
			a := nrgba.Pix[i+3]
			alpha = append(alpha, a)
			if a != 0xFF {
				hasAlpha = true
			}
		}
	}

	rgbCompressed, err := flateCompress(rgb)
	if err != nil {
		return ast.PdfStream{}, 0, 0, fmt.Errorf("compressing PNG rgb: %w", err)
	}

	dict := ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(w),
		"Height":           ast.PdfInt(h),
		"ColorSpace":       ast.PdfName("DeviceRGB"),
		"BitsPerComponent": ast.PdfInt(8),
		"Filter":           ast.PdfName("FlateDecode"),
	}

	if hasAlpha {
		alphaCompressed, err := flateCompress(alpha)
		if err != nil {
			return ast.PdfStream{}, 0, 0, fmt.Errorf("compressing PNG alpha: %w", err)
		}
		sMaskDict := ast.PdfDict{
			"Type":             ast.PdfName("XObject"),
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(w),
			"Height":           ast.PdfInt(h),
			"ColorSpace":       ast.PdfName("DeviceGray"),
			"BitsPerComponent": ast.PdfInt(8),
			"Filter":           ast.PdfName("FlateDecode"),
		}
		sMaskRef := alloc.Alloc(ast.NewStream(sMaskDict, alphaCompressed))
		dict["SMask"] = sMaskRef
	}

	return ast.NewStream(dict, rgbCompressed), w, h, nil
}

// flateCompress returns a zlib-compressed copy of data, matching the PDF
// /FlateDecode filter format.
func flateCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
