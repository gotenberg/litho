package image

import (
	"context"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
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

	// Sort the resource names before walking them. Ranging the dict directly
	// takes Go's randomised map order, so `litho images` answered with a
	// different ordering on every run and any caller diffing two runs saw
	// changes that were not there.
	names := make([]string, 0, len(xobjDict))
	for name := range xobjDict {
		names = append(names, string(name))
	}
	sort.Strings(names)

	for _, nameStr := range names {
		name := ast.PdfName(nameStr)
		obj := xobjDict[name]
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
