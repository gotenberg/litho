package mutations

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// webCaptureTrees are the catalog name trees that map Web Capture identifiers to
// content sets (ISO 32000-1, 14.10.4, Table 96).
var webCaptureTrees = []ast.PdfName{"IDS", "URLS"}

// pruneWebCaptureSets drops the pages a removed page contributed from the Web
// Capture content sets that name them.
//
// A content set lists what one capture produced in /O, pages among them (ISO
// 32000-1, Table 356), so a set naming a deleted page keeps that page in the
// output. A set left with nothing leaves the tree, since it describes a capture
// none of whose output survives.
//
// The catalog and the trees are read as earlier stages staged them, from alloc
// first. An attachment queues the catalog's /Names afresh at a new object
// number, and a destination prune may have rewritten it already; reading the
// source's instead left the content sets naming the deleted page. For the same
// reason a /Names dictionary written inline is written back into the catalog
// as alloc holds it now, not as the caller last read it.
//
// # Corpus evidence
//
//   - govdocs thread_400_400742.pdf: held 730 bytes of a deleted page through
//     a content set after the destinations, the structure tree and the article
//     threads had all let go.
//   - All corpora: 8 files carry Web Capture trees, 32 content sets between
//     them, every one at an object number of its own, so none writes a set
//     inline. Deleting page 1 of the 7 with more than one page writes the same
//     bytes whether an inline set is rewritten or not.
func pruneWebCaptureSets(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	catalogRef ast.PdfIndirectRef,
	removed map[int]bool,
) bool {
	catalogDict, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil || catalogDict == nil {
		return false
	}
	namesObj, hasNames := catalogDict["Names"]
	if !hasNames {
		return false
	}
	names, err := resolveDictAllocFirst(ctx, alloc, doc, namesObj)
	if err != nil || names == nil {
		return false
	}

	newNames := make(ast.PdfDict, len(names))
	maps.Copy(newNames, names)
	changed := false
	truncated := false

	for _, key := range webCaptureTrees {
		root, hasTree := names[key]
		if !hasTree {
			continue
		}
		newRoot, treeChanged, treeTruncated := pruneDestNameTree(ctx, alloc, doc, root,
			func(value ast.PdfObject) (ast.PdfObject, bool) {
				return contentSetKept(ctx, alloc, doc, value, removed)
			})
		if treeTruncated {
			truncated = true
		}
		if !treeChanged {
			continue
		}
		if _, rootIsRef := root.(ast.PdfIndirectRef); !rootIsRef {
			newNames[key] = newRoot
			changed = true
		}
	}
	if !changed {
		return truncated
	}
	if ref, isRef := namesObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, newNames)
		return truncated
	}
	newCatalog := make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Names"] = newNames
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	return truncated
}

// contentSetKept filters a content set's /O array down to the objects that
// survived. It returns nil when nothing survives, and otherwise the set to keep
// in the tree (see [nameTreeDropFunc]). A set or /O array with an object number
// of its own is rewritten at that number, and the set comes back as it was; a
// set written inline with its /O comes back rewritten.
func contentSetKept(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	value ast.PdfObject,
	removed map[int]bool,
) (ast.PdfObject, bool) {
	set, err := resolveDictAllocFirst(ctx, alloc, doc, value)
	if err != nil || set == nil {
		return value, false
	}
	objsObj, hasObjs := set["O"]
	if !hasObjs {
		return value, false
	}
	objs, err := resolveArrayAllocFirst(ctx, alloc, doc, objsObj)
	if err != nil {
		return value, false
	}

	kept := make(ast.PdfArray, 0, len(objs))
	for _, entry := range objs {
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && removed[ref.ObjectNum] {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == len(objs) {
		return value, false
	}
	if len(kept) == 0 {
		return nil, false
	}

	if ref, isRef := objsObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, kept)
		return value, false
	}
	newSet := make(ast.PdfDict, len(set))
	maps.Copy(newSet, set)
	newSet["O"] = kept
	if ref, isRef := value.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, newSet)
		return value, false
	}
	return newSet, true
}
