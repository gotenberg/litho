package remediation

import (
	"context"
	"fmt"
	"maps"

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
