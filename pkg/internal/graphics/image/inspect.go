package image

import (
	"context"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractImages scans a page's own resources for Image XObjects and returns
// their metadata without decoding or saving pixel data. This is used by the OCR
// triage heuristic to quickly evaluate whether a page is a scanned image, which
// is why it stays shallow: a scanned page states its image at the top level, and
// descending would change which pages the heuristic sends to OCR.
//
// Use [ExtractImagesRecursive] to answer "which images does this page show",
// which includes the ones drawn through a form XObject.
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

// maxXObjectDepth bounds the descent into nested form XObjects. It is an
// anti-DoS guard, not spec-mandated: ISO 32000-1, 8.10 puts no limit on
// nesting, and a crafted file whose form invokes itself would otherwise
// recurse without end. The visited set already breaks a cycle; the depth caps
// a merely deep chain.
const maxXObjectDepth = 16

// ExtractImagesRecursive returns the images a page shows, including those drawn
// through a form XObject.
//
// ISO 32000-1, 8.10.1 lets a page draw its content through a form XObject, and
// producers put images there routinely. [ExtractImages] reports only the page's
// own resources, which is what the OCR heuristic wants, so a caller listing a
// page's images saw none of the nested ones: across the pdfjs and govdocs
// corpora that under-reported 583 pages.
//
// A form invoked from several places is walked once, so its images are reported
// once. An image named twice in one resource dictionary stays two entries: the
// listing is keyed by resource name, and both names are real placements.
func ExtractImagesRecursive(ctx context.Context, doc *document.Document, page *document.Page) []models.ImageMeta {
	seen := make(map[int]bool)
	return collectImages(ctx, doc, page.Resources, seen, 0)
}

// collectImages walks a resource dictionary, reporting its Image XObjects and
// descending into its form XObjects.
func collectImages(ctx context.Context, doc *document.Document, resources ast.PdfDict, seen map[int]bool, depth int) []models.ImageMeta {
	if depth > maxXObjectDepth || ctx.Err() != nil {
		return nil
	}
	xobjObj, ok := resources["XObject"]
	if !ok {
		return nil
	}
	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil || xobjDict == nil {
		return nil
	}

	// Sorted, so the listing does not depend on Go's map order.
	names := make([]string, 0, len(xobjDict))
	for name := range xobjDict {
		names = append(names, string(name))
	}
	sort.Strings(names)

	var images []models.ImageMeta
	for _, nameStr := range names {
		obj := xobjDict[ast.PdfName(nameStr)]
		stream, err := document.ResolveStream(ctx, doc, obj)
		if err != nil || stream.Dict == nil {
			continue
		}
		subtype, err := resolveName(ctx, doc, stream.Dict, "Subtype")
		if err != nil {
			continue
		}

		switch subtype {
		case "Image":
			if meta, ok := imageMeta(ctx, doc, nameStr, stream); ok {
				images = append(images, meta)
			}
		case "Form":
			// Only a form is remembered. One invoked from several places holds
			// the same images each time, while an image named twice in the same
			// resource dictionary is two placements and stays two entries, which
			// is what the resource-name listing has always reported.
			if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
				if seen[ref.ObjectNum] {
					continue
				}
				seen[ref.ObjectNum] = true
			}
			nested, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
			if err != nil || nested == nil {
				continue
			}
			images = append(images, collectImages(ctx, doc, nested, seen, depth+1)...)
		}
	}
	return images
}

// imageMeta reads the metadata an Image XObject states.
func imageMeta(ctx context.Context, doc *document.Document, name string, stream ast.PdfStream) (models.ImageMeta, bool) {
	width, err := resolveInt(ctx, doc, stream.Dict, "Width")
	if err != nil {
		return models.ImageMeta{}, false
	}
	height, err := resolveInt(ctx, doc, stream.Dict, "Height")
	if err != nil {
		return models.ImageMeta{}, false
	}
	bitsPerComp, _ := resolveInt(ctx, doc, stream.Dict, "BitsPerComponent")
	if bitsPerComp == 0 {
		bitsPerComp = 8
	}
	return models.ImageMeta{
		Filename:         name,
		Width:            width,
		Height:           height,
		ColorSpace:       resolveColorSpace(ctx, doc, stream.Dict),
		BitsPerComponent: bitsPerComp,
		Open:             doc.OpenFunc(stream),
	}, true
}
