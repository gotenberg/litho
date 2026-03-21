package image

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

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

	width, err := getInt(ctx, doc, stream.Dict, "Width")
	if err != nil {
		return nil, err
	}

	height, err := getInt(ctx, doc, stream.Dict, "Height")
	if err != nil {
		return nil, err
	}

	colorSpace := resolveColorSpace(ctx, doc, stream.Dict)
	bitsPerComp, _ := getInt(ctx, doc, stream.Dict, "BitsPerComponent")
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
		// Raw JPEG — use Go's image/jpeg decoder.
		data, err := stream.Bytes()
		if err != nil {
			return nil, fmt.Errorf("failed to read JPEG stream data: %w", err)
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to decode JPEG: %w", err)
		}
		return img, nil
	}

	// Decode stream data (FlateDecode, etc.).
	decodedData, err := filter.DecodeStream(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to decode stream: %w", err)
	}

	if bitsPerComp != 8 {
		return nil, fmt.Errorf("unsupported BitsPerComponent %d for image decode", bitsPerComp)
	}

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
				r := decodedData[idx]
				g := decodedData[idx+1]
				b := decodedData[idx+2]
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
				img.Set(x, y, color.Gray{Y: decodedData[idx]})
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
				c := float64(decodedData[idx]) / 255.0
				m := float64(decodedData[idx+1]) / 255.0
				yy := float64(decodedData[idx+2]) / 255.0
				k := float64(decodedData[idx+3]) / 255.0
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
