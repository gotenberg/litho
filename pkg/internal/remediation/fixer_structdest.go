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

// matchStructDest matches the PDF/UA-2 intra-document destination rules
// (ISO 14289-2 §8.8): explicit destinations and GoTo targets shall be
// structure destinations.
func matchStructDest(v compliance.Violation) bool {
	return v.Rule == compliance.Rule_14289_2_8_8_t01 || v.Rule == compliance.Rule_14289_2_8_8_t02
}

// maxOutlineFixWalk bounds the outline sibling/child walk. Anti-DoS
// guard, not spec-mandated: real outlines hold far fewer items, and the
// visited set already breaks cycles.
const maxOutlineFixWalk = 10000

// fixStructDest rewrites explicit array destinations as structure
// destinations: a dictionary destination carrying the original array
// under /D plus an /SD array targeting a structure element on the
// destination page (ISO 32000-2 §12.3.2.3). veraPDF's PDDestination
// model accepts a dictionary destination once /SD is present. The
// element is the page's first structure element via /StructParents and
// the ParentTree, falling back to the structure tree root's first kid;
// without a structure tree nothing is rewritten (an /SD pointing
// nowhere would trade one violation for another).
func fixStructDest(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	picker := newStructElemPicker(ctx, alloc, doc, catalogDict)
	if picker == nil {
		return nil, nil
	}

	count := 0

	// Catalog /OpenAction: either a destination array or a GoTo action.
	if oa, ok := catalogDict["OpenAction"]; ok {
		if wrapped, ok2 := picker.wrapDest(ctx, oa); ok2 {
			newCatalog := make(ast.PdfDict, len(catalogDict))
			maps.Copy(newCatalog, catalogDict)
			newCatalog["OpenAction"] = wrapped
			alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
			count++
		} else if fixed := picker.fixGoToAction(ctx, alloc, oa); fixed {
			count++
		}
	}

	// Outline items: /Dest arrays and /A GoTo actions.
	if outlines, err := document.ResolveDict(ctx, doc, catalogDict["Outlines"]); err == nil && outlines != nil {
		count += picker.fixOutlineLevel(ctx, alloc, outlines["First"], make(map[int]bool))
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("rewrote %d destination(s) as structure destinations", count)}, nil
}

// structElemPicker resolves the structure element to target for a given
// destination page.
type structElemPicker struct {
	alloc       *writer.ObjAllocator
	doc         *document.Document
	parentNums  ast.PdfArray
	fallbackRef ast.PdfObject
}

// newStructElemPicker returns nil when the document has no structure
// tree root with at least one kid.
func newStructElemPicker(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict) *structElemPicker {
	stObj, ok := catalogDict["StructTreeRoot"]
	if !ok {
		return nil
	}
	var stDict ast.PdfDict
	if ref, isRef := stObj.(ast.PdfIndirectRef); isRef {
		stDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	} else if d, err := document.ResolveDict(ctx, doc, stObj); err == nil {
		stDict = d
	}
	if stDict == nil {
		return nil
	}

	var fallback ast.PdfObject
	if kObj, ok := stDict["K"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, kObj); err == nil && len(arr) > 0 {
			fallback = arr[0]
		} else if _, isRef := kObj.(ast.PdfIndirectRef); isRef {
			fallback = kObj
		}
	}
	if fallback == nil {
		return nil
	}

	nums, _, _ := existingParentTreeNums(ctx, alloc, doc, stDict)
	return &structElemPicker{alloc: alloc, doc: doc, parentNums: nums, fallbackRef: fallback}
}

// elemForPage returns a structure element for the destination page:
// the first element under the page's /StructParents ParentTree entry,
// else the structure tree root's first kid.
func (p *structElemPicker) elemForPage(ctx context.Context, pageObj ast.PdfObject) ast.PdfObject {
	pageRef, isRef := pageObj.(ast.PdfIndirectRef)
	if !isRef {
		return p.fallbackRef
	}
	pageDict := resolveFromAllocOrDoc(ctx, p.alloc, p.doc, pageRef)
	if pageDict == nil {
		return p.fallbackRef
	}
	key, err := document.ResolveInt(ctx, p.doc, pageDict["StructParents"])
	if err != nil {
		return p.fallbackRef
	}
	for i := 0; i+1 < len(p.parentNums); i += 2 {
		k, isInt := p.parentNums[i].(ast.PdfInt)
		if !isInt || k != key {
			continue
		}
		if arr, err := document.ResolveArray(ctx, p.doc, p.parentNums[i+1]); err == nil {
			for _, e := range arr {
				if _, isNull := e.(ast.PdfNull); !isNull && e != nil {
					return e
				}
			}
		}
		break
	}
	return p.fallbackRef
}

// wrapDest converts an explicit destination array into a dictionary
// destination with /D (the original array) and /SD (the structure
// destination, same view parameters with the page swapped for a
// structure element). Returns ok=false when obj is not a destination
// array.
func (p *structElemPicker) wrapDest(ctx context.Context, obj ast.PdfObject) (ast.PdfDict, bool) {
	arr, isArr := obj.(ast.PdfArray)
	if !isArr {
		if resolved, err := document.ResolveObject(ctx, p.doc, obj); err == nil {
			arr, isArr = resolved.(ast.PdfArray)
		}
	}
	if !isArr || len(arr) == 0 {
		return nil, false
	}
	sd := make(ast.PdfArray, len(arr))
	copy(sd, arr)
	sd[0] = p.elemForPage(ctx, arr[0])
	return ast.PdfDict{"D": arr, "SD": sd}, true
}

// fixGoToAction rewrites the /D of a GoTo action dict in place.
func (p *structElemPicker) fixGoToAction(ctx context.Context, alloc *writer.ObjAllocator, obj ast.PdfObject) bool {
	ref, isRef := obj.(ast.PdfIndirectRef)
	var dict ast.PdfDict
	if isRef {
		dict = resolveFromAllocOrDoc(ctx, alloc, p.doc, ref)
	} else if d, err := document.ResolveDict(ctx, p.doc, obj); err == nil {
		dict = d
	}
	if dict == nil {
		return false
	}
	if s, _ := document.ResolveName(ctx, p.doc, dict["S"]); s != "GoTo" {
		return false
	}
	dObj, ok := dict["D"]
	if !ok {
		return false
	}
	wrapped, ok := p.wrapDest(ctx, dObj)
	if !ok {
		return false
	}
	if !isRef || ref.ObjectNum <= 0 {
		return false
	}
	newDict := make(ast.PdfDict, len(dict))
	maps.Copy(newDict, dict)
	newDict["D"] = wrapped
	alloc.AllocAt(ref.ObjectNum, newDict)
	return true
}

// fixOutlineLevel walks one outline level, rewriting /Dest arrays and
// /A GoTo destinations, and recursing into children. visited breaks
// crafted /Next and /First cycles.
func (p *structElemPicker) fixOutlineLevel(ctx context.Context, alloc *writer.ObjAllocator, obj ast.PdfObject, visited map[int]bool) int {
	count := 0
	for obj != nil && len(visited) < maxOutlineFixWalk {
		if ctx.Err() != nil {
			return count
		}
		ref, isRef := obj.(ast.PdfIndirectRef)
		if isRef {
			if visited[ref.ObjectNum] {
				return count
			}
			visited[ref.ObjectNum] = true
		}
		item := resolveFromAllocOrDoc(ctx, alloc, p.doc, ref)
		if !isRef || item == nil {
			return count
		}

		dirty := false
		newItem := make(ast.PdfDict, len(item)+1)
		maps.Copy(newItem, item)
		if destObj, hasDest := item["Dest"]; hasDest {
			if wrapped, ok := p.wrapDest(ctx, destObj); ok {
				newItem["Dest"] = wrapped
				dirty = true
			}
		}
		if dirty {
			alloc.AllocAt(ref.ObjectNum, newItem)
			count++
		}
		if aObj, hasA := item["A"]; hasA {
			if p.fixGoToAction(ctx, alloc, aObj) {
				count++
			}
		}

		count += p.fixOutlineLevel(ctx, alloc, item["First"], visited)
		obj = item["Next"]
	}
	return count
}
