package image

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// resolveInt extracts an integer from a dictionary key, delegating resolution
// to [document.ResolveInt]. Returns an error if the key is missing or not an integer.
func resolveInt(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) (int, error) {
	obj, ok := dict[key]
	if !ok {
		return 0, fmt.Errorf("missing key %s", key)
	}
	val, err := document.ResolveInt(ctx, doc, obj)
	if err != nil {
		return 0, fmt.Errorf("key %s: %w", key, err)
	}
	return intconv.Clamp(val), nil
}

// resolveName extracts a name string from a dictionary key, delegating
// resolution to [document.ResolveName]. Returns an error if the key is missing.
func resolveName(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) (string, error) {
	obj, ok := dict[key]
	if !ok {
		return "", fmt.Errorf("missing key %s", key)
	}
	val, err := document.ResolveName(ctx, doc, obj)
	if err != nil {
		return "", fmt.Errorf("key %s: %w", key, err)
	}
	return string(val), nil
}

// iccBasedSpace returns the device space an [/ICCBased stream] array stands for.
//
// Per ISO 32000-1, 8.6.5.5, Table 66, the stream's /N gives the number of
// components and shall be 1, 3 or 4, matching DeviceGray, DeviceRGB and
// DeviceCMYK; /Alternate names the space to substitute when the profile cannot
// be used. Answering DeviceRGB for every ICCBased space mislabelled CMYK
// images, and the answer is not cosmetic: the decoder switches on it, so a
// four-component image was read three bytes to the pixel.
//
// An array that states neither keeps the old DeviceRGB answer, which is the
// most common profile and what a caller can do least harm with.
//
// # Corpus evidence
//
//   - pdfjs issue11052.pdf: its CMYK images were mislabelled DeviceRGB.
func iccBasedSpace(ctx context.Context, doc *document.Document, cs ast.PdfArray) string {
	if len(cs) < 2 {
		return "DeviceRGB"
	}
	streamObj, err := document.ResolveObject(ctx, doc, cs[1])
	if err != nil {
		doc.Logger.Debug("resolving ICCBased profile stream", slog.Any("err", err))
		return "DeviceRGB"
	}

	var profile ast.PdfDict
	switch v := streamObj.(type) {
	case ast.PdfStream:
		profile = v.Dict
	case ast.PdfDict:
		profile = v
	default:
		return "DeviceRGB"
	}

	if nObj, ok := profile["N"]; ok {
		n, err := document.ResolveObject(ctx, doc, nObj)
		if err != nil {
			doc.Logger.Debug("resolving ICCBased /N", slog.Any("err", err))
		} else if num, isNum := document.Number(n); isNum {
			switch int(num) {
			case 1:
				return "DeviceGray"
			case 3:
				return "DeviceRGB"
			case 4:
				return "DeviceCMYK"
			}
		}
	}

	if altObj, ok := profile["Alternate"]; ok {
		alt, err := document.ResolveObject(ctx, doc, altObj)
		if err != nil {
			doc.Logger.Debug("resolving ICCBased /Alternate", slog.Any("err", err))
		} else if name, isName := alt.(ast.PdfName); isName {
			switch name {
			case "DeviceGray", "DeviceRGB", "DeviceCMYK":
				return string(name)
			}
		}
	}

	return "DeviceRGB"
}

// resolveColorSpace extracts the /ColorSpace from an image XObject dictionary
// and maps it to a usable base color space name. PDF color spaces can be a
// simple /Name or a complex array (e.g. [/ICCBased stream], [/Indexed base ...]).
func resolveColorSpace(ctx context.Context, doc *document.Document, dict ast.PdfDict) string {
	csObj, ok := dict["ColorSpace"]
	if !ok {
		return "DeviceRGB" // Sensible default when missing entirely
	}

	resolved, err := document.ResolveObject(ctx, doc, csObj)
	if err != nil {
		return "DeviceRGB"
	}

	switch cs := resolved.(type) {
	case ast.PdfName:
		return string(cs)

	case ast.PdfArray:
		if len(cs) == 0 {
			return "DeviceRGB"
		}
		// The first element is the color space family name.
		familyObj, err := document.ResolveObject(ctx, doc, cs[0])
		if err != nil {
			return "DeviceRGB"
		}
		family, ok := familyObj.(ast.PdfName)
		if !ok {
			return "DeviceRGB"
		}

		switch family {
		case "ICCBased":
			return iccBasedSpace(ctx, doc, cs)
		case "CalRGB":
			return "DeviceRGB"
		case "CalGray":
			return "DeviceGray"
		case "Indexed":
			// Second element of the array is the base color space.
			if len(cs) >= 2 {
				baseObj, err := document.ResolveObject(ctx, doc, cs[1])
				if err == nil {
					if baseName, ok := baseObj.(ast.PdfName); ok {
						return string(baseName)
					}
					// Base could itself be an array (e.g. [/ICCBased ...]).
					if baseArr, ok := baseObj.(ast.PdfArray); ok && len(baseArr) > 0 {
						baseFamily, err := document.ResolveObject(ctx, doc, baseArr[0])
						if err != nil {
							doc.Logger.Debug("resolving indexed color space base family", slog.Any("err", err))
						}
						if bf, ok := baseFamily.(ast.PdfName); ok {
							switch bf {
							case "ICCBased":
								return iccBasedSpace(ctx, doc, baseArr)
							case "CalRGB":
								return "DeviceRGB"
							case "CalGray":
								return "DeviceGray"
							}
						}
					}
				}
			}
			return "DeviceRGB"
		case "Separation", "DeviceN":
			return "DeviceRGB" // Treat as RGB for non-visual extraction
		default:
			return string(family)
		}

	default:
		return "DeviceRGB"
	}
}
