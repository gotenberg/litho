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

// forbiddenActionTypes lists action types that PDF/A forbids in annotation,
// catalog, and page action dictionaries (ISO 19005-2/3/4 §6.6.1 with
// Corrigendum 2 plus ISO 32000-2 additions that PDF/A does not admit).
var forbiddenActionTypes = map[string]bool{
	"Launch":      true,
	"Sound":       true,
	"Movie":       true,
	"ResetForm":   true,
	"ImportData":  true,
	"JavaScript":  true,
	"Hide":        true,
	"Rendition":   true,
	"Trans":       true,
	"GoTo3DView":  true,
	"SetState":    true,
	"NOP":         true,
	"SetOCGState": true,
}

func matchJavaScript(v compliance.Violation) bool {
	return descriptionContains(v, "JavaScript") ||
		clausePrefix(v, "6.6.1", "6.5.1")
}

func fixJavaScript(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	var fixes []string

	// Remove forbidden entries from catalog /AA.
	if aaObj, ok := catalogDict["AA"]; ok {
		catalogFixes, newCatalog := removeForbiddenFromAA(ctx, doc, catalogDict, aaObj, "catalog")
		if newCatalog != nil {
			catalogDict = newCatalog
			fixes = append(fixes, catalogFixes...)
		}
	}

	// Remove /OpenAction if it references a forbidden action type.
	if oaObj, ok := catalogDict["OpenAction"]; ok {
		if isForbiddenAction(ctx, doc, oaObj) {
			newCatalog := make(ast.PdfDict, len(catalogDict))
			maps.Copy(newCatalog, catalogDict)
			delete(newCatalog, "OpenAction")
			catalogDict = newCatalog
			fixes = append(fixes, "removed forbidden /OpenAction from catalog")
		}
	}

	// Remove /Names/JavaScript from catalog.
	if namesObj, ok := catalogDict["Names"]; ok {
		if namesDict, err := document.ResolveDict(ctx, doc, namesObj); err == nil && namesDict != nil {
			if _, ok := namesDict["JavaScript"]; ok {
				newNames := make(ast.PdfDict, len(namesDict))
				maps.Copy(newNames, namesDict)
				delete(newNames, "JavaScript")
				namesRef := alloc.Alloc(newNames)

				newCatalog := make(ast.PdfDict, len(catalogDict))
				maps.Copy(newCatalog, catalogDict)
				newCatalog["Names"] = namesRef
				catalogDict = newCatalog
				fixes = append(fixes, "removed /Names/JavaScript from catalog")
			}
		}
	}

	if len(fixes) > 0 {
		alloc.AllocAt(catalogRef.ObjectNum, catalogDict)
	}

	// Walk pages to remove forbidden actions from page /AA and annotations.
	pageFixes, err := fixForbiddenActionsOnPages(ctx, alloc, doc, catalogDict)
	if err != nil {
		return fixes, fmt.Errorf("fixing forbidden actions on pages: %w", err)
	}
	fixes = append(fixes, pageFixes...)

	// Outlines live outside the page tree but can still carry forbidden
	// /A actions (PDF/A-2 §6.6.1 covers outline items too).
	outlineFixes := fixForbiddenActionsOnOutlines(ctx, alloc, doc, catalogDict)
	fixes = append(fixes, outlineFixes...)

	return fixes, nil
}

// fixForbiddenActionsOnOutlines walks the outline tree and strips /A entries
// whose action subtype is forbidden by ISO 19005-2 §6.6.1.
func fixForbiddenActionsOnOutlines(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) []string {
	outlinesObj, ok := catalog["Outlines"]
	if !ok {
		return nil
	}
	visited := make(map[int]bool)
	var fixes []string
	walkOutlineItem(ctx, alloc, doc, outlinesObj, visited, &fixes, 0)
	return fixes
}

// walkOutlineItem recurses into /First and /Next sibling chains, bounded
// by [visited] to break cycles in malformed outlines.
func walkOutlineItem(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, visited map[int]bool, fixes *[]string, depth int) {
	if ctx.Err() != nil || depth > maxPageTreeDepth {
		return
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if visited[ref.ObjectNum] {
			return
		}
		visited[ref.ObjectNum] = true
	}

	var dict ast.PdfDict
	if isRef && ref.ObjectNum > 0 {
		dict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if dict == nil {
		resolved, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || resolved == nil {
			return
		}
		dict = resolved
	}

	if aObj, hasA := dict["A"]; hasA && isForbiddenAction(ctx, doc, aObj) {
		newDict := make(ast.PdfDict, len(dict))
		maps.Copy(newDict, dict)
		delete(newDict, "A")
		dict = newDict
		if isRef && ref.ObjectNum > 0 {
			alloc.AllocAt(ref.ObjectNum, dict)
		}
		*fixes = append(*fixes, "removed forbidden /A action from outline item")
	}

	if first, ok := dict["First"]; ok {
		walkOutlineItem(ctx, alloc, doc, first, visited, fixes, depth+1)
	}
	if next, ok := dict["Next"]; ok {
		walkOutlineItem(ctx, alloc, doc, next, visited, fixes, depth+1)
	}
}

// fixForbiddenActionsOnPages walks the page tree and removes forbidden /A and
// /AA entries from page dicts and their annotations.
func fixForbiddenActionsOnPages(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) ([]string, error) {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return nil, nil
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, 0)

	var fixes []string
	for i, pe := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		pageDict := pe.dict
		pageNum := i + 1

		// Remove forbidden entries from page /AA.
		if aaObj, ok := pageDict["AA"]; ok {
			pageFixes, newPage := removeForbiddenFromAA(ctx, doc, pageDict, aaObj, fmt.Sprintf("page %d", pageNum))
			if newPage != nil {
				pageDict = newPage
				if pe.ref.ObjectNum > 0 {
					alloc.AllocAt(pe.ref.ObjectNum, pageDict)
				}
				fixes = append(fixes, pageFixes...)
			}
		}

		// Walk annotations and remove forbidden /A and /AA.
		annotsObj, ok := pageDict["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil || len(annots) == 0 {
			continue
		}

		for _, annotObj := range annots {
			var annotRef ast.PdfIndirectRef
			if r, ok := annotObj.(ast.PdfIndirectRef); ok {
				annotRef = r
			}

			var annotDict ast.PdfDict
			if annotRef.ObjectNum > 0 {
				annotDict = resolveFromAllocOrDoc(ctx, alloc, doc, annotRef)
			}
			if annotDict == nil {
				resolved, err := document.ResolveDict(ctx, doc, annotObj)
				if err != nil || resolved == nil {
					continue
				}
				annotDict = resolved
			}

			changed := false
			newAnnot := make(ast.PdfDict, len(annotDict))
			maps.Copy(newAnnot, annotDict)

			// Remove /A if it references a forbidden action type.
			if aObj, hasA := newAnnot["A"]; hasA {
				if isForbiddenAction(ctx, doc, aObj) {
					delete(newAnnot, "A")
					changed = true
					fixes = append(fixes, fmt.Sprintf("removed forbidden /A action from annotation on page %d", pageNum))
				}
			}

			// Remove forbidden entries from annotation /AA.
			if aaObj, hasAA := newAnnot["AA"]; hasAA {
				aaFixes, cleaned := removeForbiddenFromAADict(ctx, doc, aaObj, fmt.Sprintf("annotation on page %d", pageNum))
				if cleaned != nil {
					if len(cleaned) == 0 {
						delete(newAnnot, "AA")
					} else {
						newAnnot["AA"] = cleaned
					}
					changed = true
					fixes = append(fixes, aaFixes...)
				}
			}

			if changed && annotRef.ObjectNum > 0 {
				alloc.AllocAt(annotRef.ObjectNum, newAnnot)
			}
		}
	}

	return fixes, nil
}

// isForbiddenAction checks if the given object is an action dict with a
// forbidden action type (per ISO 19005-2, clause 6.6.1).
func isForbiddenAction(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return false
	}
	s, ok := d["S"].(ast.PdfName)
	if !ok {
		return false
	}
	if forbiddenActionTypes[string(s)] {
		return true
	}
	// A Named action is forbidden unless its /N is one of the standard named
	// actions (§6.6.1). Mirror the validator (which reuses the same
	// [compliance.AllowedNamedActions] set): only a present, non-standard /N is
	// a violation, so a malformed action without /N is left alone.
	if s == "Named" {
		if n, _ := document.ResolveName(ctx, doc, d["N"]); n != "" {
			return !compliance.AllowedNamedActions[n]
		}
	}
	return false
}

// removeForbiddenFromAA removes entries with forbidden action types from an /AA
// dict on the given parent dict. Returns the fix descriptions and a new parent
// dict if changes were made, or nil if no changes were needed.
func removeForbiddenFromAA(ctx context.Context, doc *document.Document, parentDict ast.PdfDict, aaObj ast.PdfObject, location string) ([]string, ast.PdfDict) {
	aaFixes, cleaned := removeForbiddenFromAADict(ctx, doc, aaObj, location)
	if cleaned == nil {
		return nil, nil
	}

	newParent := make(ast.PdfDict, len(parentDict))
	maps.Copy(newParent, parentDict)
	if len(cleaned) == 0 {
		delete(newParent, "AA")
	} else {
		newParent["AA"] = cleaned
	}
	return aaFixes, newParent
}

// removeForbiddenFromAADict resolves an /AA dict and removes entries whose
// action values have forbidden types. Returns fix descriptions and the cleaned
// dict, or nil if no changes were needed.
func removeForbiddenFromAADict(ctx context.Context, doc *document.Document, aaObj ast.PdfObject, location string) ([]string, ast.PdfDict) {
	aaDict, err := document.ResolveDict(ctx, doc, aaObj)
	if err != nil || aaDict == nil {
		return nil, nil
	}

	var forbidden []ast.PdfName
	for k, v := range aaDict {
		if isForbiddenAction(ctx, doc, v) {
			forbidden = append(forbidden, k)
		}
	}
	if len(forbidden) == 0 {
		return nil, nil
	}

	cleaned := make(ast.PdfDict, len(aaDict))
	maps.Copy(cleaned, aaDict)
	var fixes []string
	for _, k := range forbidden {
		delete(cleaned, k)
		fixes = append(fixes, fmt.Sprintf("removed forbidden /AA/%s action from %s", k, location))
	}
	return fixes, cleaned
}
