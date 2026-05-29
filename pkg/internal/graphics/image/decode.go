package image

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

const (
	// maxImagePixels caps how many pixels a single Image XObject may
	// declare before [DecodeImage] refuses to allocate. 100 megapixels
	// covers print-resolution photographs (8K display tops out at
	// ~33 MP) yet rejects adversarial PDFs that declare 100k×100k
	// dimensions to force ~40 GB allocations.
	maxImagePixels = 100_000_000

	// maxImageBytes caps the decoded byte stream. Four bytes per pixel
	// is the worst case (RGBA), so this matches the pixel cap and
	// bounds [io.ReadAll] on the streaming decoder output.
	maxImageBytes = 4 * maxImagePixels
)

// validateImageDimensions rejects degenerate or out-of-policy image
// sizes. Width and height must both be positive and their product must
// fit under [maxImagePixels].
func validateImageDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions: %dx%d", width, height)
	}
	if int64(width)*int64(height) > maxImagePixels {
		return fmt.Errorf("image %dx%d exceeds %d-pixel safety cap", width, height, maxImagePixels)
	}
	return nil
}

// imageComponents returns the samples-per-pixel for the color spaces the
// 8-bit decoder handles, or 0 for anything else.
func imageComponents(colorSpace string) int {
	switch colorSpace {
	case "DeviceRGB":
		return 3
	case "DeviceGray":
		return 1
	case "DeviceCMYK":
		return 4
	}
	return 0
}

// resolveDecodeArray reads the image /Decode array as 2*components
// numbers, or returns nil when it is absent or malformed. Per ISO
// 32000-1 §8.9.5.2 the /Decode array remaps each sample from the raw
// [0, 2^bpc-1] range onto [Dmin, Dmax]; honoring it renders inverted or
// remapped images (e.g. Adobe-inverted CMYK, /Decode [1 0 ...]) with
// correct color instead of the raw samples.
func resolveDecodeArray(ctx context.Context, doc *document.Document, dict ast.PdfDict, components int) []float64 {
	obj, ok := dict["Decode"]
	if !ok {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("resolving image /Decode array", slog.Any("err", err))
		return nil
	}
	if len(arr) != 2*components {
		doc.Logger.Debug("ignoring image /Decode array with unexpected length",
			slog.Int("len", len(arr)), slog.Int("want", 2*components))
		return nil
	}
	dec := make([]float64, len(arr))
	for i, v := range arr {
		dec[i] = document.Float(v)
	}
	return dec
}

// buildDecodeLUTs returns one 256-entry lookup table per component that
// maps an 8-bit sample to its decoded byte. With no /Decode array the
// tables are the exact identity, so decoded output is byte-for-byte
// unchanged for the common case.
func buildDecodeLUTs(dec []float64, components int) [][256]byte {
	luts := make([][256]byte, components)
	for j := range luts {
		if dec == nil {
			for s := 0; s < 256; s++ {
				luts[j][s] = byte(s)
			}
			continue
		}
		dmin, dmax := dec[2*j], dec[2*j+1]
		for s := 0; s < 256; s++ {
			v := dmin + float64(s)*(dmax-dmin)/255.0
			switch {
			case v < 0:
				v = 0
			case v > 1:
				v = 1
			}
			luts[j][s] = byte(v*255 + 0.5)
		}
	}
	return luts
}

// DecodeImage decodes a named Image XObject from a page into a Go image.Image.
// The name parameter is the resource key (e.g., "Im0") under the page's
// /Resources /XObject dictionary.
func DecodeImage(ctx context.Context, doc *document.Document, page *document.Page, name ast.PdfName) (image.Image, error) {
	xobjObj, ok := page.Resources["XObject"]
	if !ok {
		return nil, fmt.Errorf("page has no /XObject resources")
	}

	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil {
		return nil, err
	}
	if xobjDict == nil {
		return nil, fmt.Errorf("/XObject is not a dictionary")
	}

	obj, ok := xobjDict[name]
	if !ok {
		return nil, fmt.Errorf("image %q not found in /XObject", name)
	}

	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	if stream.Dict == nil {
		return nil, fmt.Errorf("image %q is not a stream", name)
	}

	width, err := resolveInt(ctx, doc, stream.Dict, "Width")
	if err != nil {
		return nil, err
	}

	height, err := resolveInt(ctx, doc, stream.Dict, "Height")
	if err != nil {
		return nil, err
	}

	if err := validateImageDimensions(width, height); err != nil {
		return nil, err
	}

	colorSpace := resolveColorSpace(ctx, doc, stream.Dict)
	bitsPerComp, _ := resolveInt(ctx, doc, stream.Dict, "BitsPerComponent")
	if bitsPerComp == 0 {
		bitsPerComp = 8
	}

	// Determine the filter.
	filterName := ""
	if filterObj, ok := stream.Dict["Filter"]; ok {
		filterRes, err := document.ResolveObject(ctx, doc, filterObj)
		if err != nil {
			doc.Logger.Debug("resolving image filter", slog.Any("err", err))
		}
		if fn, ok := filterRes.(ast.PdfName); ok {
			filterName = string(fn)
		} else if fArr, ok := filterRes.(ast.PdfArray); ok && len(fArr) > 0 {
			if fn, ok := fArr[0].(ast.PdfName); ok {
				filterName = string(fn)
			}
		}
	}

	if filterName == "DCTDecode" {
		// Raw JPEG: stream through the filter pipeline without buffering.
		rc, err := doc.NewStreamReader(ctx, stream)
		if err != nil {
			return nil, fmt.Errorf("opening JPEG stream: %w", err)
		}
		defer func() {
			if closeErr := rc.Close(); closeErr != nil {
				doc.Logger.Debug("closing JPEG stream reader", slog.Any("err", closeErr))
			}
		}()
		img, err := jpeg.Decode(rc)
		if err != nil {
			return nil, fmt.Errorf("decoding JPEG: %w", err)
		}
		return img, nil
	}

	// Non-JPEG: stream through filters and read all decoded bytes for pixel
	// construction below. Streaming decompression avoids holding both
	// compressed and decompressed copies in memory simultaneously.
	rc, err := doc.NewStreamReader(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("opening image stream: %w", err)
	}
	// Cap the decoded read so a flate/lzw bomb cannot blow memory
	// before we discover the dimension mismatch below. The +1 makes
	// "exactly maxImageBytes" valid while detecting overflow.
	decodedData, err := io.ReadAll(io.LimitReader(rc, int64(maxImageBytes)+1))
	if closeErr := rc.Close(); closeErr != nil {
		doc.Logger.Debug("closing image stream reader", slog.Any("err", closeErr))
	}
	if err != nil {
		return nil, fmt.Errorf("reading decoded image stream: %w", err)
	}
	if len(decodedData) > maxImageBytes {
		return nil, fmt.Errorf("decoded image stream exceeds %d-byte safety cap", maxImageBytes)
	}

	if bitsPerComp != 8 {
		return nil, fmt.Errorf("unsupported BitsPerComponent %d for image decode", bitsPerComp)
	}

	// Apply the /Decode array (ISO 32000-1 §8.9.5.2) via per-component
	// lookup tables. Absent a /Decode array the tables are the identity,
	// so output is unchanged for images that do not declare one.
	components := imageComponents(colorSpace)
	dec := buildDecodeLUTs(resolveDecodeArray(ctx, doc, stream.Dict, components), components)

	switch colorSpace {
	case "DeviceRGB":
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		expectedLen := width * height * 3
		if len(decodedData) < expectedLen {
			return nil, fmt.Errorf("decoded data too short for RGB image: got %d, want %d", len(decodedData), expectedLen)
		}
		idx := 0
		for y := range height {
			for x := range width {
				r := dec[0][decodedData[idx]]
				g := dec[1][decodedData[idx+1]]
				b := dec[2][decodedData[idx+2]]
				img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
				idx += 3
			}
		}
		return img, nil

	case "DeviceGray":
		img := image.NewGray(image.Rect(0, 0, width, height))
		expectedLen := width * height
		if len(decodedData) < expectedLen {
			return nil, fmt.Errorf("decoded data too short for grayscale image: got %d, want %d", len(decodedData), expectedLen)
		}
		idx := 0
		for y := range height {
			for x := range width {
				img.Set(x, y, color.Gray{Y: dec[0][decodedData[idx]]})
				idx++
			}
		}
		return img, nil

	case "DeviceCMYK":
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		expectedLen := width * height * 4
		if len(decodedData) < expectedLen {
			return nil, fmt.Errorf("decoded data too short for CMYK image: got %d, want %d", len(decodedData), expectedLen)
		}
		idx := 0
		for y := range height {
			for x := range width {
				c := float64(dec[0][decodedData[idx]]) / 255.0
				m := float64(dec[1][decodedData[idx+1]]) / 255.0
				yy := float64(dec[2][decodedData[idx+2]]) / 255.0
				k := float64(dec[3][decodedData[idx+3]]) / 255.0
				r := uint8((1 - c) * (1 - k) * 255)
				g := uint8((1 - m) * (1 - k) * 255)
				b := uint8((1 - yy) * (1 - k) * 255)
				img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
				idx += 4
			}
		}
		return img, nil

	default:
		return nil, fmt.Errorf("unsupported color space %q for image decode", colorSpace)
	}
}
