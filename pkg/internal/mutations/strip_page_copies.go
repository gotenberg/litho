package mutations

import (
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// pageCopyKeys are the page entries that hold a second copy of what the page
// drew, which a content-stream edit leaves untouched.
//
//   - /Thumb is a producer-baked raster of the whole page as it was
//     (ISO 32000-1, 12.3.4). Blanking image samples rewrites content XObjects
//     and never reaches it, so the thumbnail still shows the words that were
//     removed. It is optional and viewers regenerate or omit it, so dropping it
//     is safe.
//   - /PieceInfo is private application data (ISO 32000-1, 14.5): InDesign and
//     Illustrator blobs that can hold the original text. It is not rendered.
//
// The CATALOG's /PieceInfo is deliberately not in this list. ISO 19005-4,
// 6.1.3 forbids /Info in the trailer unless the catalog carries /PieceInfo, a
// rule litho both checks and repairs, so dropping it would make a conforming
// PDF/A-4 file non-conforming whenever /Info stayed. Clearing that one means
// clearing /Info with it, which is a document-wide decision and not this
// page-scoped edit's to make.
var pageCopyKeys = []ast.PdfName{"Thumb", "PieceInfo"}

// stripPageCopies drops the page entries that keep a copy of the content a
// redaction removed, and reports which it dropped.
//
// Reads the page dictionary alloc-first so an edit an earlier stage queued at
// this object number survives rather than being overwritten, the same way
// [ExcisePageSpans] does.
func stripPageCopies(alloc *writer.ObjAllocator, page *document.Page) []string {
	if page.Ref.ObjectNum == 0 {
		// Nothing to write back to. The caller has already refused such a page
		// before reaching here.
		return nil
	}

	baseDict := latestPageDict(alloc, page)
	dropped := make([]string, 0, len(pageCopyKeys))
	newDict := make(ast.PdfDict, len(baseDict))
	maps.Copy(newDict, baseDict)

	for _, key := range pageCopyKeys {
		if _, present := newDict[key]; present {
			delete(newDict, key)
			dropped = append(dropped, string(key))
		}
	}
	if len(dropped) == 0 {
		return nil
	}
	if !alloc.AllocAt(page.Ref.ObjectNum, newDict) {
		return nil
	}
	return dropped
}
