package mutations

import (
	"context"

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
