package image

import (
	"context"
	"image"
	"log/slog"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractImages scans a page's own resources for Image XObjects and returns
// their metadata without decoding or saving pixel data. The OCR triage
// heuristic uses it to quickly evaluate whether a page is a scanned image, which
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

// ExtractImagesRecursive returns the images a page shows, including those drawn
// through a form XObject or an annotation's appearance.
//
// ISO 32000-1, 8.10.1 lets a page draw its content through a form XObject, and
// producers put images there routinely. [ExtractImages] reports only the page's
// own resources, which is what the OCR heuristic wants, so a caller listing a
// page's images saw none of the nested ones.
//
// An annotation's normal appearance is a form XObject the page draws too
// (ISO 32000-1, 12.5.5), and its resources are its own rather than the page's.
// A page that shows an image only that way would otherwise list no images at
// all while poppler draws one.
//
// A form invoked from several places is walked once, so its images are reported
// once. An image named twice in one resource dictionary stays two entries: the
// listing is keyed by resource name, and both names are real placements.
//
// # Corpus evidence
//
//   - All corpora: across the pdfjs and govdocs corpora, reporting only the
//     page's own resources under-reported 583 pages.
//   - pdfjs firefox_logo.pdf: shows a 512x543 image through an annotation
//     appearance and nothing else, so the page listed no images at all while
//     poppler drew one.
//   - All corpora: reporting only the page's own resources missed the images
//     nested in form XObjects on 583 pages of the pdfjs and govdocs corpora.
func ExtractImagesRecursive(ctx context.Context, doc *document.Document, page *document.Page) []models.ImageMeta {
	var images []models.ImageMeta
	walkPageImages(ctx, doc, page, func(name string, stream ast.PdfStream, resources ast.PdfDict) bool {
		if meta, ok := imageMeta(ctx, doc, name, stream, colorContext{resources: resources, page: page.Dict}); ok {
			images = append(images, meta)
		}
		return true
	})
	return images
}

// imageVisitor receives each image stream a walk reaches, under the resource
// name it is found by, and reports whether the walk goes on.
type imageVisitor func(name string, stream ast.PdfStream, resources ast.PdfDict) bool

// walkPageImages visits the image XObjects a page shows, in the order
// [ExtractImagesRecursive] lists them: its resources, then its annotations'
// normal appearances.
func walkPageImages(ctx context.Context, doc *document.Document, page *document.Page, visit imageVisitor) {
	seen := make(map[int]bool)
	if walkImages(ctx, doc, page.Resources, seen, 0, visit) {
		walkAnnotationImages(ctx, doc, page, seen, visit)
	}
}

// findImage returns the image the page shows under name, the first entry of
// that name [ExtractImagesRecursive] lists, and the resource dictionary that
// names it.
func findImage(ctx context.Context, doc *document.Document, page *document.Page, name string) (ast.PdfStream, ast.PdfDict, bool) {
	var found ast.PdfStream
	var foundIn ast.PdfDict
	ok := false
	walkPageImages(ctx, doc, page, func(n string, stream ast.PdfStream, resources ast.PdfDict) bool {
		if n != name {
			return true
		}
		if _, listed := imageMeta(ctx, doc, n, stream, colorContext{}); !listed {
			return true
		}
		found, foundIn, ok = stream, resources, true
		return false
	})
	return found, foundIn, ok
}

// walkAnnotationImages visits the images an annotation draws through its
// normal appearance stream.
//
// Only /N is walked: /D is the down appearance, shown while the pointer is
// held, and /R the rollover, so neither is what the page shows at rest
// (ISO 32000-1, 12.5.5, Table 168). An annotation the viewer does not draw is
// skipped, which is what the Hidden and NoView flags of Table 165 mean.
func walkAnnotationImages(ctx context.Context, doc *document.Document, page *document.Page, seen map[int]bool, visit imageVisitor) bool {
	annotsObj, ok := page.Dict["Annots"]
	if !ok {
		return true
	}
	annots, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil || annots == nil {
		return true
	}

	for _, annotObj := range annots {
		if ctx.Err() != nil {
			return false
		}
		annotDict, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annotDict == nil {
			continue
		}
		if annotationIsInvisible(ctx, doc, annotDict) {
			continue
		}
		stream, ref, ok := normalAppearance(ctx, doc, annotDict)
		if !ok {
			continue
		}
		// One appearance form shared by several annotations holds the same
		// images each time, the rule a form invoked from several places in the
		// page content already follows.
		if ref.ObjectNum != 0 {
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
		}
		resources, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if err != nil || resources == nil {
			continue
		}
		if !walkImages(ctx, doc, resources, seen, 1, visit) {
			return false
		}
	}
	return true
}

// Annotation flags that keep a viewer from drawing the annotation at all
// (ISO 32000-1, Table 165): bit 2 is Hidden and bit 6 is NoView.
const (
	annotFlagHidden = 1 << 1
	annotFlagNoView = 1 << 5
)

// annotationIsInvisible reports whether the annotation's /F flags keep it off
// the page.
func annotationIsInvisible(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	fObj, ok := annotDict["F"]
	if !ok {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, fObj)
	if err != nil {
		doc.Logger.Debug("resolving annotation flags", slog.Any("err", err))
		return false
	}
	flags, isNum := document.Number(resolved)
	if !isNum {
		return false
	}
	return int(flags)&(annotFlagHidden|annotFlagNoView) != 0
}

// normalAppearance returns the stream an annotation's /AP /N names, and the
// reference it was reached through so a shared appearance is walked once. The
// appearance state in /AS selects the stream when /N holds a dictionary.
func normalAppearance(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) (ast.PdfStream, ast.PdfIndirectRef, bool) {
	var none ast.PdfIndirectRef
	apDict, err := document.ResolveDict(ctx, doc, annotDict["AP"])
	if err != nil || apDict == nil {
		return ast.PdfStream{}, none, false
	}
	nObj, ok := apDict["N"]
	if !ok {
		return ast.PdfStream{}, none, false
	}
	resolved, err := document.ResolveObject(ctx, doc, nObj)
	if err != nil {
		doc.Logger.Debug("resolving annotation appearance", slog.Any("err", err))
		return ast.PdfStream{}, none, false
	}
	switch v := resolved.(type) {
	case ast.PdfStream:
		ref, _ := nObj.(ast.PdfIndirectRef)
		return v, ref, true
	case ast.PdfDict:
		state, ok := annotDict["AS"].(ast.PdfName)
		if !ok {
			// Without /AS the state is ambiguous, unless there is only one.
			if len(v) != 1 {
				return ast.PdfStream{}, none, false
			}
			for name := range v {
				state = name
			}
		}
		stateObj, ok := v[state]
		if !ok {
			return ast.PdfStream{}, none, false
		}
		stateRes, err := document.ResolveObject(ctx, doc, stateObj)
		if err != nil {
			doc.Logger.Debug("resolving annotation appearance state", slog.Any("err", err))
			return ast.PdfStream{}, none, false
		}
		if s, ok := stateRes.(ast.PdfStream); ok {
			ref, _ := stateObj.(ast.PdfIndirectRef)
			return s, ref, true
		}
	}
	return ast.PdfStream{}, none, false
}

// walkImages walks a resource dictionary, visiting its Image XObjects and
// descending into its form XObjects, tiling patterns and soft-mask groups.
//
// depth is how many forms the resources sit below the page: 0 for the page's
// own, 1 for a form the page paints or an annotation's appearance. Resources
// deeper than the document's form budget, limits.Limits.MaxFormDepth, are
// not walked, which bounds a merely deep chain; ISO 32000-1, 8.10 puts no
// limit on nesting. The seen set breaks a cycle.
func walkImages(ctx context.Context, doc *document.Document, resources ast.PdfDict, seen map[int]bool, depth int, visit imageVisitor) bool {
	if ctx.Err() != nil {
		return false
	}
	if depth > doc.Limits().MaxFormDepth {
		return true
	}

	if !walkNestedResourceImages(ctx, doc, resources, seen, depth, visit) {
		return false
	}

	xobjObj, ok := resources["XObject"]
	if !ok {
		return true
	}
	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil || xobjDict == nil {
		return true
	}

	// Sorted, so the listing does not depend on Go's map order.
	names := make([]string, 0, len(xobjDict))
	for name := range xobjDict {
		names = append(names, string(name))
	}
	sort.Strings(names)

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
			if !visit(nameStr, stream, resources) {
				return false
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
			if !walkImages(ctx, doc, nested, seen, depth+1, visit) {
				return false
			}
		}
	}
	return true
}

// walkNestedResourceImages visits the images reachable from a resource
// dictionary other than through its /XObject entry.
//
// A tiling pattern is a content stream with its own resources (ISO 32000-1,
// 8.7.3.1), and a soft mask names a transparency group XObject in /G that is
// painted to derive the mask (11.6.5.2, Table 144). Both put images on the page
// without ever appearing in the page's /XObject dictionary, so a page that
// paints its images only this way would otherwise list none while poppler draws
// them.
//
// # Corpus evidence
//
//   - pdfjs issue18956.pdf: shows a 728x90 image through an /ExtGState soft
//     mask and nothing else, and listed no images at all while poppler drew it.
//   - pdfjs issue16395.pdf: paints its images through a pattern, and listed no
//     images at all while poppler drew them.
func walkNestedResourceImages(ctx context.Context, doc *document.Document, resources ast.PdfDict, seen map[int]bool, depth int, visit imageVisitor) bool {
	for _, key := range []ast.PdfName{"Pattern", "ExtGState"} {
		entryObj, ok := resources[key]
		if !ok {
			continue
		}
		entries, err := document.ResolveDict(ctx, doc, entryObj)
		if err != nil || entries == nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for name := range entries {
			names = append(names, string(name))
		}
		sort.Strings(names)

		for _, nameStr := range names {
			if ctx.Err() != nil {
				return false
			}
			obj := entries[ast.PdfName(nameStr)]
			if key == "Pattern" {
				if !walkStreamResources(ctx, doc, obj, seen, depth, visit) {
					return false
				}
				continue
			}
			state, err := document.ResolveDict(ctx, doc, obj)
			if err != nil || state == nil {
				continue
			}
			smask, err := document.ResolveDict(ctx, doc, state["SMask"])
			if err != nil || smask == nil {
				// /SMask is /None on most graphics states, which resolves to no
				// dictionary and means no mask at all.
				continue
			}
			if !walkStreamResources(ctx, doc, smask["G"], seen, depth, visit) {
				return false
			}
		}
	}
	return true
}

// walkStreamResources walks the /Resources of the stream obj names, once per
// stream object.
func walkStreamResources(ctx context.Context, doc *document.Document, obj ast.PdfObject, seen map[int]bool, depth int, visit imageVisitor) bool {
	if obj == nil {
		return true
	}
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		if seen[ref.ObjectNum] {
			return true
		}
		seen[ref.ObjectNum] = true
	}
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil || stream.Dict == nil {
		return true
	}
	nested, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil || nested == nil {
		return true
	}
	return walkImages(ctx, doc, nested, seen, depth+1, visit)
}

// usesJPXDecode reports whether the image stream is JPEG 2000 encoded.
//
// /Filter is a name or an array of them (ISO 32000-1, 7.3.8.2, Table 5), and
// JPXDecode is always the last of a chain, so any position counts.
func usesJPXDecode(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	filterObj, ok := dict["Filter"]
	if !ok {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, filterObj)
	if err != nil {
		doc.Logger.Debug("resolving image filter", slog.Any("err", err))
		return false
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		return v == "JPXDecode"
	case ast.PdfArray:
		for _, entry := range v {
			name, err := document.ResolveObject(ctx, doc, entry)
			if err != nil {
				continue
			}
			if n, isName := name.(ast.PdfName); isName && n == "JPXDecode" {
				return true
			}
		}
	}
	return false
}

// statedBitsPerComponent returns the sample depth an image is to be read at,
// defaulting to 8.
//
// Per ISO 32000-1, Table 89 /BitsPerComponent is optional for a JPXDecode image
// and shall be ignored if present, because the depth belongs to the JPEG 2000
// codestream rather than to the dictionary. Honouring a stated number would
// report a depth the spec says means nothing, and worse, would drive
// [unpackSamples] to stride 8-bit codestream output as though it were packed
// at the stated depth and mis-read the whole image.
//
// One helper rather than the same clause applied in two places, so the reading
// cannot drift between what an image reports and what it decodes as.
//
// # Corpus evidence
//
//   - pdfjs issue12213.pdf: states /BitsPerComponent 4 for a JPXDecode image
//     poppler reads as 8, and honouring it would stride the output as though
//     it were packed 4-bit.
func statedBitsPerComponent(ctx context.Context, doc *document.Document, dict ast.PdfDict) int {
	bpc, _ := resolveInt(ctx, doc, dict, "BitsPerComponent")
	if bpc == 0 || usesJPXDecode(ctx, doc, dict) {
		return 8
	}
	return bpc
}

// imageMeta reads the metadata an Image XObject states. Its Decode function
// reads the colours in cc.
func imageMeta(ctx context.Context, doc *document.Document, name string, stream ast.PdfStream, cc colorContext) (models.ImageMeta, bool) {
	width, err := resolveInt(ctx, doc, stream.Dict, "Width")
	if err != nil {
		return models.ImageMeta{}, false
	}
	height, err := resolveInt(ctx, doc, stream.Dict, "Height")
	if err != nil {
		return models.ImageMeta{}, false
	}
	bitsPerComp := statedBitsPerComponent(ctx, doc, stream.Dict)
	return models.ImageMeta{
		Filename:         name,
		Width:            width,
		Height:           height,
		ColorSpace:       resolveColorSpace(ctx, doc, stream.Dict),
		BitsPerComponent: bitsPerComp,
		Open:             doc.OpenFunc(stream),
		Decode: func(ctx context.Context) (image.Image, error) {
			return decodeImageStream(ctx, doc, ast.PdfName(name), stream, cc)
		},
	}, true
}
