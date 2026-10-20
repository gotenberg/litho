package mutations

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// ScrubCatalogPrivateData removes the /Private entry of each data dictionary
// the catalog's /PieceInfo holds, and returns the names of the applications
// whose data it removed, sorted.
//
// A page piece dictionary holds a conforming writer's private data
// (ISO 32000-1, 14.5, Table 368): an Acrobat embedded search index, an
// authoring application's copy of the document. Its format is the
// application's own, often compressed, so the scan that proves a redaction
// clean cannot read a copy of the removed text inside it, and nothing renders
// it. The data describes the document before the edit: 14.5 has an
// application treat it as stale once the document's modification date passes
// its /LastModified.
//
// The dictionaries stay, with /LastModified and every other entry: ISO
// 19005-4, 6.1.3 lets the trailer carry /Info only when the catalog carries
// /PieceInfo, and dropping it would make a conforming PDF/A-4 file
// non-conforming (see [pageCopyKeys]). A page's /PieceInfo goes whole with the
// page's redaction instead; another page's, and a form XObject's, describe
// content the redaction did not touch and stay.
func ScrubCatalogPrivateData(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return nil, fmt.Errorf("resolving the catalog: %w", err)
	}
	pieceObj, has := catalog["PieceInfo"]
	if !has {
		return nil, nil
	}
	piece, err := resolveDictAllocFirst(ctx, alloc, doc, pieceObj)
	if err != nil || piece == nil {
		// A /PieceInfo that is no dictionary holds no data dictionary to clear;
		// the scan still reads whatever the reference names.
		return nil, nil //nolint:nilerr // A deliberate fallback, see above.
	}

	var scrubbed []string
	newPiece := maps.Clone(piece)
	for _, app := range slices.Sorted(maps.Keys(piece)) {
		dataObj := piece[app]
		data, err := resolveDictAllocFirst(ctx, alloc, doc, dataObj)
		if err != nil || data == nil {
			continue
		}
		if _, hasPrivate := data["Private"]; !hasPrivate {
			continue
		}
		cleared := maps.Clone(data)
		delete(cleared, "Private")
		scrubbed = append(scrubbed, string(app))
		if ref, isRef := dataObj.(ast.PdfIndirectRef); isRef && alloc.AllocAt(ref.ObjectNum, cleared) {
			continue
		}
		newPiece[app] = cleared
	}
	if len(scrubbed) == 0 {
		return nil, nil
	}
	if ref, isRef := pieceObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newPiece)
		return scrubbed, nil
	}
	newCatalog := maps.Clone(catalog)
	newCatalog["PieceInfo"] = newPiece
	if !alloc.AllocAt(catalogRef.ObjectNum, newCatalog) {
		return nil, fmt.Errorf("%w: the catalog has no object number", ErrCatalogUnreadable)
	}
	return scrubbed, nil
}
