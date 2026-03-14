package image

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/models"
)

// resolveObject is a helper to resolve indirect references.
func resolveObject(doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ref)
	}
	return obj, nil
}

// getInt is a helper to extract an integer from a dictionary safely.
func getInt(doc *document.Document, dict ast.PdfDict, key ast.PdfName) (int, error) {
	obj, ok := dict[key]
	if !ok {
		return 0, fmt.Errorf("missing key /%s", key)
	}
	res, err := resolveObject(doc, obj)
	if err != nil {
		return 0, err
	}
	if val, ok := res.(ast.PdfInt); ok {
		return int(val), nil
	}
	return 0, fmt.Errorf("key /%s is not an integer", key)
}

// getName is a helper to extract a name from a dictionary safely.
func getName(doc *document.Document, dict ast.PdfDict, key ast.PdfName) (string, error) {
	obj, ok := dict[key]
	if !ok {
		return "", fmt.Errorf("missing key /%s", key)
	}
	res, err := resolveObject(doc, obj)
	if err != nil {
		return "", err
	}
	if val, ok := res.(ast.PdfName); ok {
		return string(val), nil
	}
	return "", fmt.Errorf("key /%s is not a name", key)
}

// ExtractImages scans a page's resources for Image XObjects and saves them to disk.
func ExtractImages(doc *document.Document, page *document.Page, outputDir string) ([]models.ImageMeta, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %v", err)
	}

	xobjObj, ok := page.Resources["XObject"]
	if !ok {
		return nil, nil // No XObjects
	}

	xobjResolved, err := resolveObject(doc, xobjObj)
	if err != nil {
		return nil, err
	}

	xobjDict, ok := xobjResolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("/XObject is not a dictionary")
	}

	var savedImages []models.ImageMeta

	for name, obj := range xobjDict {
		resolved, err := resolveObject(doc, obj)
		if err != nil {
			continue
		}

		stream, ok := resolved.(ast.PdfStream)
		if !ok {
			continue
		}

		subtype, err := getName(doc, stream.Dict, "Subtype")
		if err != nil || subtype != "Image" {
			continue
		}

		width, err := getInt(doc, stream.Dict, "Width")
		if err != nil {
			continue
		}

		height, err := getInt(doc, stream.Dict, "Height")
		if err != nil {
			continue
		}

		colorSpace := resolveColorSpace(doc, stream.Dict)
		bitsPerComp, _ := getInt(doc, stream.Dict, "BitsPerComponent")
		if bitsPerComp == 0 {
			bitsPerComp = 8 // Default
		}

		// Determine the filter
		filterName := ""
		if filterObj, ok := stream.Dict["Filter"]; ok {
			filterRes, _ := resolveObject(doc, filterObj)
			if fn, ok := filterRes.(ast.PdfName); ok {
				filterName = string(fn)
			} else if fArr, ok := filterRes.(ast.PdfArray); ok && len(fArr) > 0 {
				if fn, ok := fArr[0].(ast.PdfName); ok {
					filterName = string(fn)
				}
			}
		}

		filename := fmt.Sprintf("image_%s", name)

		meta := models.ImageMeta{
			Filename:         filename,
			Width:            width,
			Height:           height,
			ColorSpace:       colorSpace,
			BitsPerComponent: bitsPerComp,
		}

		if filterName == "DCTDecode" {
			// DCTDecode is raw JPEG data. No decompression needed, save directly.
			outPath := filepath.Join(outputDir, filename+".jpg")
			err = os.WriteFile(outPath, stream.Data, 0o600)
			if err == nil {
				meta.FilePath = outPath
				savedImages = append(savedImages, meta)
			}
		} else if filterName == "FlateDecode" || filterName == "" {
			// Decode the stream
			decodedData, err := filter.DecodeStream(stream)
			if err != nil {
				continue
			}

			// Basic pixel mapping for 8-bit RGB or Grayscale
			if bitsPerComp == 8 {
				switch colorSpace {
				case "DeviceRGB":
					img := image.NewRGBA(image.Rect(0, 0, width, height))
					expectedLen := width * height * 3
					if len(decodedData) >= expectedLen {
						idx := 0
						for y := range height {
							for x := range width {
								r := decodedData[idx]
								g := decodedData[idx+1]
								b := decodedData[idx+2]
								img.Set(x, y, color.RGBA{r, g, b, 255})
								idx += 3
							}
						}
						outPath := filepath.Join(outputDir, filename+".png")
						if err := savePNG(outPath, img); err == nil {
							meta.FilePath = outPath
							savedImages = append(savedImages, meta)
						}
					}
				case "DeviceGray":
					img := image.NewGray(image.Rect(0, 0, width, height))
					expectedLen := width * height
					if len(decodedData) >= expectedLen {
						idx := 0
						for y := range height {
							for x := range width {
								img.Set(x, y, color.Gray{Y: decodedData[idx]})
								idx++
							}
						}
						outPath := filepath.Join(outputDir, filename+".png")
						if err := savePNG(outPath, img); err == nil {
							meta.FilePath = outPath
							savedImages = append(savedImages, meta)
						}
					}
				}
			}
		}
	}

	return savedImages, nil
}

// resolveColorSpace extracts the /ColorSpace from an image XObject dictionary
// and maps it to a usable base color space name. PDF color spaces can be a
// simple /Name or a complex array (e.g. [/ICCBased stream], [/Indexed base ...]).
func resolveColorSpace(doc *document.Document, dict ast.PdfDict) string {
	csObj, ok := dict["ColorSpace"]
	if !ok {
		return "DeviceRGB" // Sensible default when missing entirely
	}

	resolved, err := resolveObject(doc, csObj)
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
		familyObj, err := resolveObject(doc, cs[0])
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
				baseObj, err := resolveObject(doc, cs[1])
				if err == nil {
					if baseName, ok := baseObj.(ast.PdfName); ok {
						return string(baseName)
					}
					// Base could itself be an array (e.g. [/ICCBased ...]).
					if baseArr, ok := baseObj.(ast.PdfArray); ok && len(baseArr) > 0 {
						baseFamily, _ := resolveObject(doc, baseArr[0])
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

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()
	return png.Encode(f, img)
}
