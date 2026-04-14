package image

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// resolveInt extracts an integer from a dictionary key, delegating resolution
// to [document.ResolveInt]. Returns an error if the key is missing or not an integer.
func resolveInt(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) (int, error) {
	obj, ok := dict[key]
	if !ok {
		return 0, fmt.Errorf("missing key /%s", key)
	}
	val, err := document.ResolveInt(ctx, doc, obj)
	if err != nil {
		return 0, fmt.Errorf("key /%s: %w", key, err)
	}
	return int(val), nil
}

// resolveName extracts a name string from a dictionary key, delegating
// resolution to [document.ResolveName]. Returns an error if the key is missing.
func resolveName(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) (string, error) {
	obj, ok := dict[key]
	if !ok {
		return "", fmt.Errorf("missing key /%s", key)
	}
	val, err := document.ResolveName(ctx, doc, obj)
	if err != nil {
		return "", fmt.Errorf("key /%s: %w", key, err)
	}
	return string(val), nil
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
		case "ICCBased", "CalRGB":
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
							case "ICCBased", "CalRGB":
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
