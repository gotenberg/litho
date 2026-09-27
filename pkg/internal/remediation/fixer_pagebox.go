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
//
// A box that is not four numbers is first read as the rectangle ISO 19005-2,
// 6.1.13 t11 measures ([compliance.PageBoundaryRect]), its missing corners and
// the ones that are no number at 0. A crop box of [0 0 300] so encloses no
// area and is dropped like any degenerate box, and a media box of that shape
// is replaced. Left as written, the box stayed unrepaired while the validator
// reported it.
//
// # Corpus evidence
//
//   - pdfjs issue7872.pdf: writes its /MediaBox as an array of indirect
//     references, which reads as zero-sized unless the coordinates are
//     resolved.
//   - pdfjs boundingBox_invalid.pdf: growing a degenerate box at its
//     lower-left corner lost the text of two pages, which litho's own
//     regression gate caught, leaving the file unremediable.
//   - verapdf 6-1-13-t09-fail-c through -f: walking only the leaves left them
//     unremediable, their box sitting on the "Pages tree" node litho itself
//     names in the violation.
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
			// Measure the coordinates, not the references naming them. A page
			// writing /MediaBox [4 0 R 5 0 R 6 0 R 7 0 R] (ISO 32000-1, 7.3.9)
			// reads as zero-sized otherwise, and a valid page then had its box
			// replaced by the fallback, which crops away everything drawn
			// outside it. The resolved copy drives the decision only: a box
			// needing no change keeps the form the producer wrote.
			arr = document.ResolveRectangle(ctx, doc, arr)
			if measured, differs := compliance.PageBoundaryRect(ctx, doc, arr); differs {
				arr = measured
				newPage[key] = measured
				modified = true
			}
			// A degenerate box encloses nothing, and growing it to the 3-unit
			// minimum at its lower-left corner leaves the page's content
			// outside the page, so the text drawn there is lost. Every box
			// except /MediaBox defaults to another box when absent (ISO
			// 32000-1, Table 30), so dropping it is both lossless and what the
			// producer meant.
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
		// A page written directly in its parent's /Kids is written back
		// there ([writeTreeNode]).
		if modified && writeTreeNode(ctx, alloc, doc, pe, newPage) {
			count++
		}
	}
	// A box on an interior /Pages node is inherited by every page under it
	// (ISO 32000-1, 7.7.3.4, Table 30), so it is just as much a page boundary
	// and 6.1.13 test 11 reports it as one. Walking only the leaves left
	// such a box unremediable, sitting on the "Pages tree" node litho itself
	// names in the violation.
	nodes := clampPageTreeBoxes(ctx, alloc, doc, pagesObj, opts.PreserveContent, fallbackMedia)

	var fixes []string
	if count > 0 {
		fixes = append(fixes, fmt.Sprintf("normalised page boundaries on %d page(s)", count))
	}
	if nodes > 0 {
		fixes = append(fixes, fmt.Sprintf("normalised inherited page boundaries on %d page tree node(s)", nodes))
	}
	return fixes, nil
}

// clampPageTreeBoxes normalises the boxes declared on the interior nodes of the
// page tree, returning how many nodes it rewrote.
func clampPageTreeBoxes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, node ast.PdfObject, preserveContent bool, fallbackMedia ast.PdfArray) int {
	return clampPageTreeNode(ctx, alloc, doc, node, preserveContent, fallbackMedia, make(map[int]bool), 0)
}

// clampPageTreeNode clamps one node's boxes and recurses into its /Kids. Leaf
// pages are skipped: the caller already normalised those. The visited set
// breaks a /Kids cycle and the depth bound caps a pathological tree.
func clampPageTreeNode(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, node ast.PdfObject, preserveContent bool, fallbackMedia ast.PdfArray, visited map[int]bool, depth int) int {
	if depth > maxPageTreeDepth || ctx.Err() != nil {
		return 0
	}
	ref, isRef := node.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum == 0 || visited[ref.ObjectNum] {
		return 0
	}
	visited[ref.ObjectNum] = true

	dict := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	if dict == nil {
		return 0
	}
	if typ, _ := document.ResolveName(ctx, doc, dict["Type"]); typ == "Page" {
		return 0
	}

	count := 0
	if newNode, modified := clampBoxesInDict(ctx, doc, dict, preserveContent, fallbackMedia); modified {
		alloc.AllocAt(ref.ObjectNum, newNode)
		count++
	}

	kids, err := document.ResolveArray(ctx, doc, dict["Kids"])
	if err != nil {
		return count
	}
	for _, kid := range kids {
		count += clampPageTreeNode(ctx, alloc, doc, kid, preserveContent, fallbackMedia, visited, depth+1)
	}
	return count
}

// clampBoxesInDict returns a copy of dict with every page boundary brought
// inside the permitted range, and whether anything changed. It is the same
// normalisation the leaf pass applies, so a box means the same thing wherever
// it is declared.
func clampBoxesInDict(ctx context.Context, doc *document.Document, dict ast.PdfDict, preserveContent bool, fallbackMedia ast.PdfArray) (ast.PdfDict, bool) {
	modified := false
	out := make(ast.PdfDict, len(dict))
	maps.Copy(out, dict)
	for _, key := range []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"} {
		arr, ok := out[key].(ast.PdfArray)
		if !ok {
			continue
		}
		arr = document.ResolveRectangle(ctx, doc, arr)
		if measured, differs := compliance.PageBoundaryRect(ctx, doc, arr); differs {
			arr = measured
			out[key] = measured
			modified = true
		}
		if isDegenerateBox(arr) {
			if key != "MediaBox" {
				delete(out, key)
			} else {
				out[key] = fallbackMedia
			}
			modified = true
			continue
		}
		if clamped, changed := clampBoxArray(arr, preserveContent); changed {
			out[key] = clamped
			modified = true
		}
	}
	return out, modified
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
// The two extents are taken independently on purpose. Picking the whole box
// with the largest AREA is not enough: a landscape page and a portrait page can
// tie on area, and choosing the first crops away content the other extends to.
//
// The box is resolved rather than type-asserted: any object may be written as
// an indirect reference (ISO 32000-1, 7.3.10), and a page that writes its
// /MediaBox that way was skipped here. The fallback then came from the default
// instead of the sibling page, which is exactly the cropping this function
// exists to avoid: a page beside an indirect 800x600 box was given 612x792.
//
// # Corpus evidence
//
//   - pdfjs boundingBox_invalid.pdf: has an 800x600 page and a 600x800 one,
//     identical in area, and choosing the first still cropped away text drawn
//     at y=700.
func widestUsableMediaBox(ctx context.Context, doc *document.Document, entries []annotPageEntry) ast.PdfArray {
	maxW, maxH := 0.0, 0.0
	for _, pe := range entries {
		arr, err := document.ResolveArray(ctx, doc, pe.dict["MediaBox"])
		if err != nil || len(arr) != 4 {
			continue
		}
		arr = document.ResolveRectangle(ctx, doc, arr)
		if isDegenerateBox(arr) {
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
