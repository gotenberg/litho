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
// output: govdocs thread_400_400742.pdf held 730 bytes of it after the
// destinations, the structure tree and the article threads had all let go. A set
// left with nothing leaves the tree, since it describes a capture none of whose
// output survives.
func pruneWebCaptureSets(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	catalogDict ast.PdfDict,
	removed map[int]bool,
) bool {
	namesObj, hasNames := catalogDict["Names"]
	if !hasNames {
		return false
	}
	names, err := document.ResolveDict(ctx, doc, namesObj)
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
		newRoot, dropped, treeTruncated := pruneDestNameTree(ctx, alloc, doc, root,
			func(value ast.PdfObject) bool {
				return contentSetIsEmptied(ctx, alloc, doc, value, removed)
			}, 0)
		if treeTruncated {
			truncated = true
		}
		if !dropped {
			continue
		}
		changed = true
		if _, rootIsRef := root.(ast.PdfIndirectRef); !rootIsRef {
			newNames[key] = newRoot
		}
	}
	if !changed {
		return truncated
	}
	if ref, isRef := namesObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, newNames)
	}
	return truncated
}

// contentSetIsEmptied filters a content set's /O array down to the objects that
// survived, queueing the rewrite, and reports whether nothing is left.
func contentSetIsEmptied(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	value ast.PdfObject,
	removed map[int]bool,
) bool {
	set, err := document.ResolveDict(ctx, doc, value)
	if err != nil || set == nil {
		return false
	}
	objsObj, hasObjs := set["O"]
	if !hasObjs {
		return false
	}
	objs, err := document.ResolveArray(ctx, doc, objsObj)
	if err != nil {
		return false
	}

	kept := make(ast.PdfArray, 0, len(objs))
	for _, entry := range objs {
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && removed[ref.ObjectNum] {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == len(objs) {
		return false
	}
	if len(kept) == 0 {
		return true
	}

	if ref, isRef := objsObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, kept)
		return false
	}
	if ref, isRef := value.(ast.PdfIndirectRef); isRef {
		newSet := make(ast.PdfDict, len(set))
		maps.Copy(newSet, set)
		newSet["O"] = kept
		alloc.AllocAt(ref.ObjectNum, newSet)
	}
	return false
}
