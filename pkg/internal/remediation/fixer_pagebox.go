package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
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
// A box no viewer uses (see [document.UsableBox]), because it is not four
// numbers or encloses no area, is repaired as the absent box a viewer reads it
// as, whatever ISO 19005-2, 6.1.13 t11 measures it as, so the repair and a
// rewrite write the same box (see mutations.completeRewrittenPageTree). A crop,
// bleed, trim or art box is dropped for the default ISO 32000-1, Table 30
// gives it. A media box, which has none, is replaced on a page by the first
// size of the document's pages that holds what the page paints, US Letter,
// the page a viewer drew, when none does ([document.MediaBoxDefault]), itself
// brought within the limits above, so the output never shows less of a page
// than a viewer did. A node's is replaced by US Letter, what a viewer drew
// every page under it on, and each page under it stating none gets a media
// box of its own the same way. Read as the rectangle
// t11 measures, its missing corners and the ones that are no number at 0, a
// crop box of [0 0 300 400 9] was written as [0 0 300 400], and a viewer that
// had drawn the page on the media box drew it on the smaller box. Left as
// written, [0 0 300] stayed unrepaired while the validator reported it.
//
// A page that read the default crop box, its media box, because the crop box
// it states or inherits is no box a viewer uses, would read the crop box of a
// node above once that one is dropped, and show another region. Such a page
// states its media box as its crop box instead (see [cropShowsThrough]), as a
// rewrite writes it.
//
// # Corpus evidence
//
//   - pdfjs issue7872.pdf: writes its /MediaBox as an array of indirect
//     references, which reads as zero-sized unless the coordinates are
//     resolved.
//   - litho pageboxes_2b.pdf: the PDF/A-2b remediation wrote page 1's
//     /MediaBox [0 0 600 800 7], /TrimBox [0 /Foo 200 300] and /BleedBox
//     [0 0 300 400 9] as the rectangles of their first four entries, and the
//     pinned pdf.js showed the page at 600 by 800, where it shows the source
//     at US Letter.
//   - pdfjs boundingBox_invalid.pdf: growing a degenerate box at its
//     lower-left corner lost the text of two pages, which litho's own
//     regression gate caught, leaving the file unremediable.
//   - pdfjs boundingBox_invalid.pdf: page 1 states /MediaBox [0 0 0 0], which
//     the pinned pdf.js shows at US Letter. The most common size, 800 by 600,
//     ties page 3's 600 by 800 and comes first, but its text, drawn at
//     (25, 700), falls off it, and the PDF/A-2b remediation refused with the
//     text-loss check. The page takes 600 by 800, which holds it, and the
//     remediation succeeds with its 70 bytes of text.
//   - All corpora: over the PDF/A-2b remediation of the 9 files holding a page
//     with no media box a viewer uses, boundingBox_invalid.pdf is the one
//     output that changes. The others repair no media box, or repair one to
//     the US Letter most of their pages have.
//   - litho defaultbox_2b.pdf: the PDF/A-2b remediation gives the pages with
//     no media box and the one under a node stating [0 0 0 0] A4 each, and
//     the node US Letter.
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

	// The fixer runs only on a document reporting a page boundary violation,
	// so the page tree walk the candidate sizes cost is paid rarely.
	media := &document.MediaBoxDefault{Docs: []*document.Document{doc}, Fallback: opts.MediaBoxFallback}
	choose := func(pe annotPageEntry, page ast.PdfDict) (ast.PdfArray, error) {
		extent, err := mutations.PageDictContentExtent(ctx, alloc, doc, pe.ref, page, nearestResources(pe, page))
		if err != nil {
			return nil, err
		}
		box, err := media.Resolve(ctx, extent)
		if err != nil {
			return nil, err
		}
		if clamped, changed := clampBoxArray(box, opts.PreserveContent); changed {
			box = clamped
		}
		return box, nil
	}

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
			// A box no viewer uses draws the page on its default, and growing
			// it to the 3-unit minimum at its lower-left corner leaves the
			// page's content outside the page, so the text drawn there is
			// lost. Every box except /MediaBox defaults to another box when
			// absent (ISO 32000-1, Table 30), so dropping it is both lossless
			// and what a viewer already shows. A crop box a node above states
			// that would then show through is kept off the page below (see
			// [cropShowsThrough]). A /MediaBox, which has no default, gets a
			// size that holds what the page paints.
			if !document.UsableBox(arr) {
				if key != "MediaBox" {
					delete(newPage, key)
				} else {
					box, err := choose(pe, newPage)
					if err != nil {
						return nil, err
					}
					newPage[key] = box
				}
				modified = true
				continue
			}
			if clamped, changed := clampBoxArray(arr, opts.PreserveContent); changed {
				newPage[key] = clamped
				modified = true
			}
		}
		// A page inheriting a media box no viewer uses, which the node pass
		// writes as US Letter, gets a size of its own.
		if !hasNonNullEntry(newPage, "MediaBox") && !inheritsUsableMediaBox(ctx, doc, pe) {
			box, err := choose(pe, newPage)
			if err != nil {
				return nil, err
			}
			newPage["MediaBox"] = box
			modified = true
		}
		if cropShowsThrough(ctx, doc, pe, newPage) {
			newPage["CropBox"] = slices.Clone(fixedMediaBox(ctx, doc, pe, newPage, opts.PreserveContent))
			modified = true
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
	nodes := clampPageTreeBoxes(ctx, alloc, doc, pagesObj, opts.PreserveContent, document.LetterMediaBox())

	var fixes []string
	if count > 0 {
		fixes = append(fixes, fmt.Sprintf("normalised page boundaries on %d page(s)", count))
	}
	if nodes > 0 {
		fixes = append(fixes, fmt.Sprintf("normalised inherited page boundaries on %d page tree node(s)", nodes))
	}
	return fixes, nil
}

// cropShowsThrough reports whether, once the crop boxes no viewer uses are
// dropped, a crop box a node above pe states would show through on pe, a page
// that read the default in its place: fixed, pe's repaired dictionary, states
// no crop box, and the one pe stated, or the nearest it inherited, was no box
// a viewer uses (see [document.UsableBox]). The default is the media box (ISO
// 32000-1, Table 30), and the one of a node above is another region.
//
// A crop box written as a reference is left as it is, by the leaf pass and the
// node pass alike, and one that is no array is passed over, as the reader
// passes over it (see [document.Page]).
func cropShowsThrough(ctx context.Context, doc *document.Document, pe annotPageEntry, fixed ast.PdfDict) bool {
	if hasNonNullEntry(fixed, "CropBox") {
		return false
	}
	_, ownDropped := pe.dict["CropBox"].(ast.PdfArray)
	readsDefault := ownDropped
	nearest := !ownDropped
	depth := 0
	for node := pe.parent; node != nil && depth <= maxPageTreeDepth; node, depth = node.parent, depth+1 {
		if !hasNonNullEntry(node.dict, "CropBox") {
			continue
		}
		entry := node.dict["CropBox"]
		box, err := document.ResolveArray(ctx, doc, entry)
		if err != nil || box == nil {
			continue
		}
		noBox := !document.UsableBox(document.ResolveRectangle(ctx, doc, box))
		if nearest {
			readsDefault, nearest = noBox, false
		}
		if _, direct := entry.(ast.PdfArray); direct && noBox {
			// The node pass drops it, and the one above shows through.
			continue
		}
		return readsDefault && !noBox
	}
	return false
}

// fixedMediaBox returns the media box pe, a page whose repaired dictionary is
// fixed, has once the page boxes are repaired: its own, repaired already, or
// the one it inherits, repaired as the node pass repairs it (see
// [clampBoxesInDict]). A page given none a viewer uses is drawn on US Letter.
func fixedMediaBox(ctx context.Context, doc *document.Document, pe annotPageEntry, fixed ast.PdfDict, preserveContent bool) ast.PdfArray {
	holder := fixed
	for node, depth := pe.parent, 0; !hasNonNullEntry(holder, "MediaBox") && node != nil && depth <= maxPageTreeDepth; node, depth = node.parent, depth+1 {
		holder = node.dict
	}
	entry := holder["MediaBox"]
	box, err := document.ResolveArray(ctx, doc, entry)
	if err != nil || box == nil {
		return document.LetterMediaBox()
	}
	box = document.ResolveRectangle(ctx, doc, box)
	if _, direct := entry.(ast.PdfArray); !direct {
		return box
	}
	if !document.UsableBox(box) {
		return document.LetterMediaBox()
	}
	if clamped, changed := clampBoxArray(box, preserveContent); changed {
		return clamped
	}
	return box
}

// inheritsUsableMediaBox reports whether pe, a page stating no media box,
// inherits one a viewer uses from the nearest node above it stating one (ISO
// 32000-1, 7.7.3.4).
func inheritsUsableMediaBox(ctx context.Context, doc *document.Document, pe annotPageEntry) bool {
	for node, depth := pe.parent, 0; node != nil && depth <= maxPageTreeDepth; node, depth = node.parent, depth+1 {
		if !hasNonNullEntry(node.dict, "MediaBox") {
			continue
		}
		box, err := document.ResolveArray(ctx, doc, node.dict["MediaBox"])
		if err != nil || box == nil {
			return false
		}
		return document.UsableBox(document.ResolveRectangle(ctx, doc, box))
	}
	return false
}

// nearestResources returns the /Resources entry page, the repaired dictionary
// of pe, states, or the nearest node above it states, nil for none.
func nearestResources(pe annotPageEntry, page ast.PdfDict) ast.PdfObject {
	if hasNonNullEntry(page, "Resources") {
		return page["Resources"]
	}
	for node, depth := pe.parent, 0; node != nil && depth <= maxPageTreeDepth; node, depth = node.parent, depth+1 {
		if hasNonNullEntry(node.dict, "Resources") {
			return node.dict["Resources"]
		}
	}
	return nil
}

// clampPageTreeBoxes normalises the boxes declared on the interior nodes of the
// page tree, returning how many nodes it rewrote. A media box no viewer uses
// is replaced by replacement, the default media box.
func clampPageTreeBoxes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, node ast.PdfObject, preserveContent bool, replacement ast.PdfArray) int {
	return clampPageTreeNode(ctx, alloc, doc, node, preserveContent, replacement, make(map[int]bool), 0)
}

// clampPageTreeNode clamps one node's boxes and recurses into its /Kids. Leaf
// pages are skipped: the caller already normalised those. The visited set
// breaks a /Kids cycle and the depth bound caps a pathological tree.
func clampPageTreeNode(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, node ast.PdfObject, preserveContent bool, replacement ast.PdfArray, visited map[int]bool, depth int) int {
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
	if newNode, modified := clampBoxesInDict(ctx, doc, dict, preserveContent, replacement); modified {
		alloc.AllocAt(ref.ObjectNum, newNode)
		count++
	}

	kids, err := document.ResolveArray(ctx, doc, dict["Kids"])
	if err != nil {
		return count
	}
	for _, kid := range kids {
		count += clampPageTreeNode(ctx, alloc, doc, kid, preserveContent, replacement, visited, depth+1)
	}
	return count
}

// clampBoxesInDict returns a copy of dict with every page boundary brought
// inside the permitted range, and whether anything changed. It is the same
// normalisation the leaf pass applies, so a box means the same thing wherever
// it is declared, a media box no viewer uses replaced by replacement.
func clampBoxesInDict(ctx context.Context, doc *document.Document, dict ast.PdfDict, preserveContent bool, replacement ast.PdfArray) (ast.PdfDict, bool) {
	modified := false
	out := make(ast.PdfDict, len(dict))
	maps.Copy(out, dict)
	for _, key := range []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"} {
		arr, ok := out[key].(ast.PdfArray)
		if !ok {
			continue
		}
		arr = document.ResolveRectangle(ctx, doc, arr)
		if !document.UsableBox(arr) {
			if key != "MediaBox" {
				delete(out, key)
			} else {
				out[key] = slices.Clone(replacement)
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
