package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

const (
	// pdfAPageMin is the minimum page-box extent required by
	// ISO 19005-2 §6.1.13 test 11 and ISO 19005-4 §6.1.9 test 11
	// (3 user-space units).
	pdfAPageMin = 3.0
	// pdfAPageMax caps page-box extent at 14,400 user-space units.
	pdfAPageMax = 14400.0
)

// matchPageBoxes matches PDF/A page-boundary-limit violations.
func matchPageBoxes(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.12", "6.1.13", "6.1.9") {
		return false
	}
	return descriptionContains(v, "page boundaries") || contextContains(v, "MediaBox")
}

// fixPageBoxes normalises every page's /MediaBox, /CropBox, /BleedBox,
// /TrimBox and /ArtBox so each side falls within [pdfAPageMin,
// pdfAPageMax]. Expanding under-sized boxes is lossless, so it always
// runs; shrinking over-sized boxes can crop visible content and only
// runs in aggressive mode.
func fixPageBoxes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}

	var pageEntries []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pageEntries, make(map[int]bool), 0)

	// A degenerate /MediaBox has to be replaced by a real one: unlike every
	// other box it has no default, so it cannot simply be dropped. Prefer a
	// size the document itself already uses, which is far likelier to contain
	// the page's content than the 3-unit minimum.
	fallbackMedia := widestUsableMediaBox(ctx, doc, pageEntries)

	count := 0
	for _, pe := range pageEntries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		modified := false
		newPage := make(ast.PdfDict, len(pe.dict))
		maps.Copy(newPage, pe.dict)
		for _, key := range []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"} {
			arr, ok := newPage[key].(ast.PdfArray)
			if !ok {
				continue
			}
			// A degenerate box encloses nothing, and growing it to the 3-unit
			// minimum at its lower-left corner leaves the page's content
			// outside the page: pdfjs boundingBox_invalid.pdf lost the text of
			// two pages that way, which litho's own regression gate caught,
			// leaving the file unremediable. Every box except /MediaBox
			// defaults to another box when absent (ISO 32000-1, Table 30), so
			// dropping it is both lossless and what the producer meant.
			if isDegenerateBox(arr) {
				if key != "MediaBox" {
					delete(newPage, key)
					modified = true
					continue
				}
				newPage[key] = fallbackMedia
				modified = true
				continue
			}
			if clamped, changed := clampBoxArray(arr, opts.PreserveContent); changed {
				newPage[key] = clamped
				modified = true
			}
		}
		if !modified || pe.ref.ObjectNum == 0 {
			continue
		}
		alloc.AllocAt(pe.ref.ObjectNum, newPage)
		count++
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("normalised page boundaries on %d page(s)", count)}, nil
}

// defaultMediaBox is US Letter, the size assumed when a degenerate /MediaBox
// has to be replaced and the document offers no usable one of its own.
//
//nolint:gochecknoglobals // constant rectangle.
var defaultMediaBox = ast.PdfArray{
	ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(612), ast.PdfReal(792),
}

// widestUsableMediaBox builds a replacement /MediaBox from the largest width
// and the largest height any non-degenerate page box in the document declares,
// falling back to [defaultMediaBox] when there is none.
//
// The two extents are taken independently on purpose. Picking whole box with
// the largest AREA is not enough: boundingBox_invalid.pdf has an 800x600 page
// and a 600x800 one, identical in area, and choosing the first still cropped
// away text drawn at y=700.
// The box is resolved rather than type-asserted: any object may be written as
// an indirect reference (ISO 32000-1, 7.3.10), and a page that writes its
// /MediaBox that way was skipped here. The fallback then came from the default
// instead of the sibling page, which is exactly the cropping this function
// exists to avoid: a page beside an indirect 800x600 box was given 612x792.
func widestUsableMediaBox(ctx context.Context, doc *document.Document, entries []annotPageEntry) ast.PdfArray {
	maxW, maxH := 0.0, 0.0
	for _, pe := range entries {
		arr, err := document.ResolveArray(ctx, doc, pe.dict["MediaBox"])
		if err != nil || len(arr) != 4 || isDegenerateBox(arr) {
			continue
		}
		maxW = math.Max(maxW, math.Abs(document.Float(arr[2])-document.Float(arr[0])))
		maxH = math.Max(maxH, math.Abs(document.Float(arr[3])-document.Float(arr[1])))
	}
	if maxW <= 0 || maxH <= 0 {
		return defaultMediaBox
	}
	return ast.PdfArray{
		ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(maxW), ast.PdfReal(maxH),
	}
}

// isDegenerateBox reports whether a rectangle encloses no area at all, so that
// nothing drawn on the page can fall inside it.
func isDegenerateBox(arr ast.PdfArray) bool {
	if len(arr) != 4 {
		return false
	}
	llx := document.Float(arr[0])
	lly := document.Float(arr[1])
	urx := document.Float(arr[2])
	ury := document.Float(arr[3])
	return urx-llx == 0 || ury-lly == 0
}

// clampBoxArray enforces dimension limits on a PDF rectangle
// [llx lly urx ury]. Width or height smaller than [pdfAPageMin] is
// always expanded at the upper-right corner (safe). Values larger than
// [pdfAPageMax] are shrunk only when preserveContent is false because
// the crop can hide previously-visible drawing.
func clampBoxArray(arr ast.PdfArray, preserveContent bool) (ast.PdfArray, bool) {
	if len(arr) != 4 {
		return arr, false
	}
	llx := document.Float(arr[0])
	lly := document.Float(arr[1])
	urx := document.Float(arr[2])
	ury := document.Float(arr[3])
	// Canonicalise ordering so width/height are non-negative.
	if urx < llx {
		llx, urx = urx, llx
	}
	if ury < lly {
		lly, ury = ury, lly
	}
	w := urx - llx
	h := ury - lly
	originalW, originalH := w, h
	if w < pdfAPageMin {
		urx = llx + pdfAPageMin
		w = pdfAPageMin
	}
	if h < pdfAPageMin {
		ury = lly + pdfAPageMin
		h = pdfAPageMin
	}
	if !preserveContent {
		if w > pdfAPageMax {
			urx = llx + pdfAPageMax
			w = pdfAPageMax
		}
		if h > pdfAPageMax {
			ury = lly + pdfAPageMax
			h = pdfAPageMax
		}
	}
	if w == originalW && h == originalH {
		return arr, false
	}
	return ast.PdfArray{
		ast.PdfReal(llx),
		ast.PdfReal(lly),
		ast.PdfReal(urx),
		ast.PdfReal(ury),
	}, true
}
