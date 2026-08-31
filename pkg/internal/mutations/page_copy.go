package mutations

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// copyPageForOutput copies one source page into alloc for a split or merge and
// returns its new indirect reference.
//
// It handles two constructs a naive CopyRef of the page reference would drop:
//
//   - A page inlined directly in a /Kids array has no indirect reference; its
//     dict is copied and allocated a new object instead.
//   - A page inherits Resources, MediaBox, CropBox, and Rotate from ancestor
//     /Pages nodes (ISO 32000-1 §7.7.3.4). The output builds a flat single-level
//     page tree with no ancestor to inherit from, so these resolved attributes
//     are materialised onto the copied page. Without the Resources
//     materialisation an inheriting page loses its fonts and its text becomes
//     unextractable; without the CropBox one it is displayed at its /MediaBox
//     size instead of the region it was cropped to.
func copyPageForOutput(ctx context.Context, copier *writer.Copier, alloc *writer.ObjAllocator, page *document.Page) (ast.PdfIndirectRef, error) {
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
		if _, has := pageDict[key]; has {
			return nil
		}
		copied, err := copier.CopyObject(ctx, val)
		if err != nil {
			return err
		}
		pageDict[key] = copied
		return nil
	}

	if page.Resources != nil {
		if err := materialize("Resources", page.Resources); err != nil {
			return ast.PdfIndirectRef{}, err
		}
	}
	if page.MediaBox != nil {
		if err := materialize("MediaBox", page.MediaBox); err != nil {
			return ast.PdfIndirectRef{}, err
		}
	}
	if croppedTighter(page) {
		if err := materialize("CropBox", page.CropBox); err != nil {
			return ast.PdfIndirectRef{}, err
		}
	}
	if page.Rotate != 0 {
		if _, has := pageDict["Rotate"]; !has {
			pageDict["Rotate"] = ast.PdfInt(page.Rotate)
		}
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
// pages the caller never asked for. Extracting nine pages of govdocs
// thread_100_100852.pdf wrote all 56 into the output through /Parent: 47 extra
// pages carrying 510 KB of content that read back as plain text, plus the source
// page tree, root node /Count 56 and all. thread_100_100184.pdf leaked 84 pages
// the same way through a single article bead.
//
// Nothing usable is lost. Split and merge build their own flat page tree and
// assign /Parent to every copied page afterwards, and [copyPageForOutput]
// materialises the four attributes a page would otherwise inherit through it.
// The beads go for the reason /StructTreeRoot and /PageLabels are not carried
// either (see copyCatalogEntries): they are keyed to pages the output does not
// have, and with no /Threads in the new catalog to list them, an article that
// survived would be unreachable anyway.
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

// croppedTighter reports whether a page's visible region differs from its
// /MediaBox, which is the only case where a copied page needs its own
// /CropBox.
//
// [document.Page.CropBox] is the EFFECTIVE region and already falls back to the
// /MediaBox (ISO 32000-1, 14.11.2), so materialising it unconditionally would
// write a redundant entry onto every copied page.
func croppedTighter(page *document.Page) bool {
	if page.CropBox == nil {
		return false
	}
	return !sameBox(page.CropBox, page.MediaBox)
}

// sameBox reports whether two rectangles denote the same region.
func sameBox(a, b ast.PdfArray) bool {
	if len(a) != 4 || len(b) != 4 {
		return false
	}
	for i := range a {
		if document.Float(a[i]) != document.Float(b[i]) {
			return false
		}
	}
	return true
}

// materializeInheritedAttrs writes the attributes a page inherits from ancestor
// /Pages nodes (Resources, MediaBox, CropBox, Rotate; ISO 32000-1 §7.7.3.4)
// directly onto dict when dict lacks them, using the resolved values from page.
//
// Callers that flatten the page tree in the source document's own object
// numbering (delete, insert) use this instead of copyPageForOutput: they reuse
// each page object in place and rebuild /Pages as a single flat node, so a page
// that inherited from the old tree has no ancestor left to inherit from. The
// resolved values reference source objects; the full rewrite that follows
// copies them like any other page content.
func materializeInheritedAttrs(dict ast.PdfDict, page *document.Page) {
	if page.Resources != nil {
		if _, has := dict["Resources"]; !has {
			dict["Resources"] = page.Resources
		}
	}
	if page.MediaBox != nil {
		if _, has := dict["MediaBox"]; !has {
			dict["MediaBox"] = page.MediaBox
		}
	}
	if croppedTighter(page) {
		if _, has := dict["CropBox"]; !has {
			dict["CropBox"] = page.CropBox
		}
	}
	if page.Rotate != 0 {
		if _, has := dict["Rotate"]; !has {
			dict["Rotate"] = ast.PdfInt(page.Rotate)
		}
	}
}
