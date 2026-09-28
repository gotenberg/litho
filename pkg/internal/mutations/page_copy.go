package mutations

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// copyPageForOutput copies one source page of doc into alloc for a split,
// merge or insertion and returns its new indirect reference.
//
// It handles two constructs a naive CopyRef of the page reference would drop:
//
//   - A page inlined directly in a /Kids array has no indirect reference; its
//     dict is copied and allocated a new object instead.
//   - A page inherits Resources, MediaBox, CropBox, and Rotate from ancestor
//     /Pages nodes (ISO 32000-1 §7.7.3.4). The output builds a flat single-level
//     page tree with no ancestor to inherit from, so these resolved attributes
//     are materialised onto the copied page (see [flatResources] for the
//     resources, [cropBoxToWrite] for the crop box). Without the Resources
//     materialisation an inheriting page loses its fonts and its text becomes
//     unextractable; without the CropBox one it is displayed at its /MediaBox
//     size instead of the region it was cropped to.
func copyPageForOutput(ctx context.Context, copier *writer.Copier, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) (ast.PdfIndirectRef, error) {
	var ref ast.PdfIndirectRef
	if page.Ref.ObjectNum != 0 {
		copied, err := copier.CopyRef(ctx, page.Ref)
		if err != nil {
			return ast.PdfIndirectRef{}, err
		}
		ref = copied
	} else {
		copied, err := copier.CopyObject(ctx, page.Dict)
		if err != nil {
			return ast.PdfIndirectRef{}, err
		}
		ref = alloc.Alloc(copied)
	}

	pageDict, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if !ok {
		return ref, nil
	}

	materialize := func(key ast.PdfName, val ast.PdfObject) error {
		if statesEntry(pageDict, key) {
			return nil
		}
		copied, err := copier.CopyObject(ctx, val)
		if err != nil {
			return err
		}
		pageDict[key] = copied
		return nil
	}

	// Judged on the source page, whose entries name objects of doc, where the
	// copy's name objects of alloc. The source page is the one the copier
	// copied: its override when it has one, as a page whose signed signatures
	// were drawn into it states the /Resources the drawing needs (see
	// [bakeSignedSignatures]), which the page it inherited would hide.
	source := page.Dict
	if override, ok := copier.Override(page.Ref.ObjectNum); ok && page.Ref.ObjectNum != 0 {
		if dict, isDict := override.(ast.PdfDict); isDict {
			source = dict
		}
	}
	if res, needed := flatResources(ctx, nil, doc, source, page); needed {
		copied, err := copier.CopyObject(ctx, res)
		if err != nil {
			return ast.PdfIndirectRef{}, err
		}
		pageDict["Resources"] = copied
	}
	if page.MediaBox != nil {
		if err := materialize("MediaBox", page.MediaBox); err != nil {
			return ast.PdfIndirectRef{}, err
		}
	}
	if crop, ok := cropBoxToWrite(page); ok {
		if err := materialize("CropBox", crop); err != nil {
			return ast.PdfIndirectRef{}, err
		}
	}
	if page.Rotate != 0 && !statesEntry(pageDict, "Rotate") {
		pageDict["Rotate"] = ast.PdfInt(page.Rotate)
	}

	return ref, nil
}

// pageCopyLinks are the page entries that reach OTHER pages of the source
// document, which [pageCopyOverrides] cuts before a copy.
//
// /Parent is the page's /Pages node, whose /Kids names every sibling (ISO
// 32000-1, 7.7.3.4). /B is the page's article beads, and each bead chains to the
// next bead of the article through /N, /V and /T (ISO 32000-1, 12.4.3).
var pageCopyLinks = []ast.PdfName{"Parent", "B"}

// pageCopyOverrides returns each page's dictionary without the entries that
// reach its neighbours, for registration with [writer.Copier.WithOverrides]
// before the pages are copied.
//
// The copier follows every reference it meets, so one of these links pulls in
// pages the caller never asked for, along with the source page tree.
//
// Nothing usable is lost. Split and merge build their own flat page tree and
// assign /Parent to every copied page afterwards, and [copyPageForOutput]
// materialises the four attributes a page would otherwise inherit through it.
// The beads go because they are keyed to pages the output does not have: each
// chains to the next bead of its article, and with no /Threads in the new
// catalog to list them, an article that survived would be unreachable anyway.
//
// # Corpus evidence
//
//   - govdocs thread_100_100852.pdf: extracting nine pages wrote all 56 into
//     the output through /Parent: 47 extra pages carrying 510 KB of content
//     that read back as plain text, plus the source page tree, root node
//     /Count 56 and all.
//   - govdocs thread_100_100184.pdf: leaked 84 pages the same way through a
//     single article bead.
func pageCopyOverrides(pages []*document.Page) map[int]ast.PdfObject {
	overrides := make(map[int]ast.PdfObject, len(pages))
	for _, page := range pages {
		if page.Ref.ObjectNum == 0 {
			continue
		}
		linked := false
		for _, key := range pageCopyLinks {
			if _, has := page.Dict[key]; has {
				linked = true
				break
			}
		}
		if !linked {
			continue
		}
		stripped := make(ast.PdfDict, len(page.Dict))
		maps.Copy(stripped, page.Dict)
		for _, key := range pageCopyLinks {
			delete(stripped, key)
		}
		overrides[page.Ref.ObjectNum] = stripped
	}
	return overrides
}

// cropBoxToWrite returns the /CropBox a page leaving its page tree states so it
// shows the region it showed, and false when it needs none.
//
// Only a crop box the page states or inherits crops it. [document.Page.CropBox]
// is the EFFECTIVE region, and falls back to the /MediaBox (ISO 32000-1, Table
// 30) when no node declares a crop box or the one declared is no box a viewer
// uses (see [document.UsableBox]). Materialising it unconditionally wrote a
// redundant entry onto every copied page, and, for a /MediaBox that is no
// rectangle, a /CropBox that is none either: [0 0 612 792 7 0 R] was copied
// whole, the content stream it runs on into included. A crop box enclosing the
// region of a /MediaBox a viewer uses is not written either. The page's
// /MediaBox is written on its own, and a rewrite writes one a viewer does not
// use as the size most pages of the document have (see
// [completeRewrittenPageTree]). The effective crop box of such a page is
// reduced to US Letter, the page a viewer draws it on, so the crop box it
// declares is written instead, and a viewer clips it against the media box the
// output gives the page.
//
// # Corpus evidence
//
//   - litho directstream_2b.pdf: its /MediaBox [0 0 612 792 5 0 R] runs on
//     into its content stream, and split, merge and insert wrote it as the
//     page's /CropBox.
//   - pdfjs issue13783.pdf, poppler-937-0-fuzzed.pdf: the same, for a
//     /MediaBox a damaged page object runs on into the entries after it.
func cropBoxToWrite(page *document.Page) (ast.PdfArray, bool) {
	if !document.UsableBox(page.DeclaredCropBox) || !document.UsableBox(page.CropBox) {
		return nil, false
	}
	if !document.UsableBox(page.MediaBox) {
		return page.DeclaredCropBox, true
	}
	if sameRegion(page.CropBox, page.MediaBox) {
		return nil, false
	}
	return page.CropBox, true
}

// rectangleCorners returns a rectangle of four numbers as its lower-left and
// upper-right corners, whichever pair of opposite corners it names (ISO
// 32000-1, 7.9.5).
func rectangleCorners(box ast.PdfArray) (x0, y0, x1, y1 float64) {
	a, b := document.Float(box[0]), document.Float(box[2])
	c, d := document.Float(box[1]), document.Float(box[3])
	return min(a, b), min(c, d), max(a, b), max(c, d)
}

// sameRegion reports whether two rectangles of four numbers denote the same
// region.
func sameRegion(a, b ast.PdfArray) bool {
	ax0, ay0, ax1, ay1 := rectangleCorners(a)
	bx0, by0, bx1, by1 := rectangleCorners(b)
	return ax0 == bx0 && ay0 == by0 && ax1 == bx1 && ay1 == by1
}

// materializeInheritedAttrs writes the attributes a page inherits from ancestor
// /Pages nodes (Resources, MediaBox, CropBox, Rotate; ISO 32000-1 §7.7.3.4)
// directly onto dict, the page's latest dictionary, when dict does not state
// them, using the values page resolved to in the tree it leaves (see
// [flatResources] for the resources).
//
// Callers that flatten the page tree in the source document's own object
// numbering (delete, insert) use this instead of copyPageForOutput: they reuse
// each page object in place and rebuild /Pages as a single flat node, so a page
// that inherited from the old tree has no ancestor left to inherit from. The
// resolved values reference source objects; the full rewrite that follows
// copies them like any other page content.
func materializeInheritedAttrs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, dict ast.PdfDict, page *document.Page) {
	if res, needed := flatResources(ctx, alloc, doc, dict, page); needed {
		dict["Resources"] = res
	}
	if page.MediaBox != nil && !statesEntry(dict, "MediaBox") {
		dict["MediaBox"] = page.MediaBox
	}
	if crop, ok := cropBoxToWrite(page); ok && !statesEntry(dict, "CropBox") {
		dict["CropBox"] = crop
	}
	if page.Rotate != 0 && !statesEntry(dict, "Rotate") {
		dict["Rotate"] = ast.PdfInt(page.Rotate)
	}
}

// statesEntry reports whether dict states key: holds it with a value other
// than null. ISO 32000-1, 7.3.7 makes a null dictionary value the same as an
// absent entry, so a page whose inheritable attribute is null takes its
// ancestor's (7.7.3.4).
func statesEntry(dict ast.PdfDict, key ast.PdfName) bool {
	v, ok := dict[key]
	if !ok || v == nil {
		return false
	}
	_, isNull := v.(ast.PdfNull)
	return !isNull
}

// pageResourcesEntry returns the /Resources entry the content of page resolves
// its names against, dict being the page's latest dictionary: dict's own when
// it states one, else the one page inherits (see
// [document.Page.InheritedResources]), nil when no node states one.
//
// ISO 32000-1, 7.7.3.4 hands a page stating none its nearest ancestor's, whole.
// [document.Page.Resources] merges the ancestors' key by key instead, which lets
// a page reach a resource the dictionary it inherits hides, so an edit that
// writes a page's resources never starts from it. Any value other than null
// ends the search, a reference to a missing object included, as veraPDF and
// compliance read it (see compliance's pageObject.inheritedHolder).
func pageResourcesEntry(dict ast.PdfDict, page *document.Page) ast.PdfObject {
	if statesEntry(dict, "Resources") {
		return dict["Resources"]
	}
	if page == nil {
		return nil
	}
	return page.InheritedResources
}

// flatResources returns the /Resources a page has to state itself once it
// leaves its page tree for a flat one, where no ancestor is left to inherit
// from, and false when none has to be written. dict is the page's latest
// dictionary, read alloc-first; a nil alloc reads doc alone.
//
// The page reaches in the flat tree exactly what it reached before (see
// [pageResourcesEntry]): one stating a dictionary keeps it, and one inheriting
// a dictionary gets that entry, whole. A page reaching none, because the
// entry it states or inherits is not a dictionary, gets an empty dictionary: a
// reader taking a reference to a missing object as null (ISO 32000-1, 7.3.10)
// would otherwise find no /Resources on it at all, which ISO 32000-1, Table 30
// requires of a page. A page no node states /Resources for stays as it is.
//
// # Corpus evidence
//
//   - litho resinherit_2b.pdf: the flat tree gave the page whose /Resources is
//     null none at all, and the pages inheriting from an intermediate node the
//     key-by-key merge, which reaches the root's font. qpdf reported "Resources
//     is missing or invalid" on the null page and on the page whose /Resources
//     names a missing object.
func flatResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, dict ast.PdfDict, page *document.Page) (ast.PdfObject, bool) {
	entry := pageResourcesEntry(dict, page)
	if entry == nil {
		return nil, false
	}
	if res, err := resolveDictAllocFirst(ctx, alloc, doc, entry); err != nil || res == nil {
		return ast.PdfDict{}, true
	}
	if statesEntry(dict, "Resources") {
		return nil, false
	}
	return entry, true
}
