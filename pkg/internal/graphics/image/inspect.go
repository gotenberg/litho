package image

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractImages scans a page's resources for Image XObjects and returns their
// metadata without decoding or saving pixel data. This is used by the OCR
// triage heuristic to quickly evaluate whether a page is a scanned image.
func ExtractImages(ctx context.Context, doc *document.Document, page *document.Page) []models.ImageMeta {
	xobjObj, ok := page.Resources["XObject"]
	if !ok {
		return nil
	}

	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil {
		return nil
	}
	if xobjDict == nil {
		return nil
	}

	var images []models.ImageMeta

	for name, obj := range xobjDict {
		stream, err := document.ResolveStream(ctx, doc, obj)
		if err != nil {
			continue
		}
		if stream.Dict == nil {
			continue
		}

		subtype, err := resolveName(ctx, doc, stream.Dict, "Subtype")
		if err != nil || subtype != "Image" {
			continue
		}

		width, err := resolveInt(ctx, doc, stream.Dict, "Width")
		if err != nil {
			continue
		}

		height, err := resolveInt(ctx, doc, stream.Dict, "Height")
		if err != nil {
			continue
		}

		colorSpace := resolveColorSpace(ctx, doc, stream.Dict)
		bitsPerComp, _ := resolveInt(ctx, doc, stream.Dict, "BitsPerComponent")
		if bitsPerComp == 0 {
			bitsPerComp = 8
		}

		images = append(images, models.ImageMeta{
			Filename:         string(name),
			Width:            width,
			Height:           height,
			ColorSpace:       colorSpace,
			BitsPerComponent: bitsPerComp,
			Open:             doc.OpenFunc(stream),
		})
	}

	return images
}

