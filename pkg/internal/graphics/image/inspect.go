package image

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// InspectImages scans a page's resources for Image XObjects and returns their
// metadata without decoding or saving pixel data. This is used by the OCR
// triage heuristic to quickly evaluate whether a page is a scanned image.
func InspectImages(doc *document.Document, page *document.Page) []models.ImageMeta {
	xobjObj, ok := page.Resources["XObject"]
	if !ok {
		return nil
	}

	xobjResolved, err := document.ResolveObject(doc, xobjObj)
	if err != nil {
		return nil
	}

	xobjDict, ok := xobjResolved.(ast.PdfDict)
	if !ok {
		return nil
	}

	var images []models.ImageMeta

	for name, obj := range xobjDict {
		resolved, err := document.ResolveObject(doc, obj)
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
			bitsPerComp = 8
		}

		images = append(images, models.ImageMeta{
			Filename:         string(name),
			Width:            width,
			Height:           height,
			ColorSpace:       colorSpace,
			BitsPerComponent: bitsPerComp,
		})
	}

	return images
}
