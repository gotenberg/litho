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
)

// forbiddenActionTypes lists the action types the fixer strips from
// annotation, catalog, page and outline actions. Membership only nominates a
// type: [stripsActionType] removes it where the target profile forbids it,
// because the parts disagree on three of them. ISO 19005-1 6.6.1 test 1,
// ISO 19005-2/3 6.5.1 test 1 and ISO 19005-4 6.6.1 test 1 each state a
// whitelist (veraPDF's PDFA-1B.xml, PDFA-2B.xml, PDFA-4.xml, PDFA-4E.xml and
// PDFA-4F.xml):
//
//   - JavaScript is outside the part 1 to 3 whitelists and inside every
//     PDF/A-4 one.
//   - SetOCGState and GoTo3DView are outside every whitelist but PDF/A-4e's,
//     which admits them for the 3D content its Annex B describes.
//
// Every other entry is outside every part's whitelist.
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

// stripsActionType reports whether the fixer removes an action of type s under
// profile: s is one of [forbiddenActionTypes] and the profile's whitelist
// excludes it. The verdict is the validator's own
// ([compliance.IsForbiddenActionType]), so an action the profile permits, such
// as JavaScript under PDF/A-4, is never stripped.
//
// # Corpus evidence
//
//   - litho forbidden_mix.pdf: remediated as PDF/A-4, PDF/A-4e or PDF/A-4f, it
//     lost the /Names/JavaScript tree those profiles permit, because its other
//     forbidden actions ran this fixer. The residual rules were the same
//     either way.
func stripsActionType(s ast.PdfName, profile string) bool {
	return forbiddenActionTypes[string(s)] && compliance.IsForbiddenActionType(s, profile)
}

func matchJavaScript(v compliance.Violation) bool {
	return descriptionContains(v, "JavaScript") ||
		clausePrefix(v, "6.6.1", "6.5.1")
}

func fixJavaScript(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	var fixes []string
	catalogChanged := false

	// Remove forbidden entries from catalog /AA.
	if aaObj, ok := catalogDict["AA"]; ok {
		catalogFixes, newCatalog := removeForbiddenFromAA(ctx, alloc, doc, catalogDict, aaObj, "catalog", profile)
		if newCatalog != nil {
			catalogDict = newCatalog
			catalogChanged = true
			fixes = append(fixes, catalogFixes...)
		}
	}

	// Remove /OpenAction if it references a forbidden action type.
	if oaObj, ok := catalogDict["OpenAction"]; ok {
		if isForbiddenAction(ctx, alloc, doc, oaObj, profile) {
			newCatalog := make(ast.PdfDict, len(catalogDict))
			maps.Copy(newCatalog, catalogDict)
			delete(newCatalog, "OpenAction")
			catalogDict = newCatalog
			catalogChanged = true
			fixes = append(fixes, "removed forbidden /OpenAction from catalog")
		}
	}

	// Remove /Names/JavaScript from catalog. The tree's presence is the
	// violation only where the profile forbids JavaScript actions, which
	// compliance.checkJavaScriptNameTree mirrors: every PDF/A-4 conformance
	// permits them.
	if namesObj, ok := catalogDict["Names"]; ok && stripsActionType("JavaScript", profile) {
		if newCatalog, removed := removeNamesJavaScript(ctx, alloc, doc, catalogDict, namesObj); removed {
			if newCatalog != nil {
				catalogDict = newCatalog
				catalogChanged = true
			}
			fixes = append(fixes, "removed /Names/JavaScript from catalog")
		}
	}

	if catalogChanged {
		alloc.AllocAt(catalogRef.ObjectNum, catalogDict)
	}

	// Walk pages to remove forbidden actions from page /AA and annotations.
	pageFixes, err := fixForbiddenActionsOnPages(ctx, alloc, doc, catalogDict, profile)
	if err != nil {
		return fixes, fmt.Errorf("fixing forbidden actions on pages: %w", err)
	}
	fixes = append(fixes, pageFixes...)

	// Outlines live outside the page tree but can still carry forbidden
	// /A actions (PDF/A-2 §6.5.1 covers outline items too).
	outlineFixes := fixForbiddenActionsOnOutlines(ctx, alloc, doc, catalogDict, profile)
	fixes = append(fixes, outlineFixes...)

	return fixes, nil
}

// removeNamesJavaScript drops the /JavaScript name tree from the catalog's
// /Names dictionary, reading it through the allocator: catalog-cleanup
// precedes this fixer and writes an indirect /Names back at its own object
// number when it removes /AlternatePresentations, so a copy of the document's
// dictionary would resurrect that entry.
//
// An indirect /Names is written back at its own object number and the catalog
// is left alone, so removed reports true with a nil catalog. An inline one
// moves to a new object, and the returned catalog points at it.
func removeNamesJavaScript(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, namesObj ast.PdfObject) (newCatalog ast.PdfDict, removed bool) {
	namesDict := liveDictValue(ctx, alloc, doc, namesObj)
	if namesDict == nil {
		return nil, false
	}
	if _, ok := namesDict["JavaScript"]; !ok {
		return nil, false
	}
	newNames := make(ast.PdfDict, len(namesDict))
	maps.Copy(newNames, namesDict)
	delete(newNames, "JavaScript")

	if ref, ok := namesObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newNames)
		return nil, true
	}
	newCatalog = make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Names"] = alloc.Alloc(newNames)
	return newCatalog, true
}

// fixForbiddenActionsOnOutlines walks the outline tree and strips /A entries
// whose action subtype profile forbids (ISO 19005-2 §6.5.1).
func fixForbiddenActionsOnOutlines(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, profile string) []string {
	outlinesObj, ok := catalog["Outlines"]
	if !ok {
		return nil
	}
	visited := make(map[int]bool)
	var fixes []string
	walkOutlineItem(ctx, alloc, doc, outlinesObj, profile, visited, &fixes, 0)
	return fixes
}

// walkOutlineItem recurses into /First and /Next sibling chains, bounded
// by visited to break cycles in malformed outlines.
func walkOutlineItem(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, profile string, visited map[int]bool, fixes *[]string, depth int) {
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

	if aObj, hasA := dict["A"]; hasA && isForbiddenAction(ctx, alloc, doc, aObj, profile) {
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
		walkOutlineItem(ctx, alloc, doc, first, profile, visited, fixes, depth+1)
	}
	if next, ok := dict["Next"]; ok {
		walkOutlineItem(ctx, alloc, doc, next, profile, visited, fixes, depth+1)
	}
}

// fixForbiddenActionsOnPages walks the page tree and removes forbidden /A and
// /AA entries from page dicts and their annotations.
func fixForbiddenActionsOnPages(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, profile string) ([]string, error) {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return nil, nil
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)

	var fixes []string
	for i, pe := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		pageDict := pe.dict
		pageNum := i + 1

		// Remove forbidden entries from page /AA.
		if aaObj, ok := pageDict["AA"]; ok {
			pageFixes, newPage := removeForbiddenFromAA(ctx, alloc, doc, pageDict, aaObj, fmt.Sprintf("page %d", pageNum), profile)
			if newPage != nil {
				pageDict = newPage
				if pe.ref.ObjectNum > 0 {
					alloc.AllocAt(pe.ref.ObjectNum, pageDict)
				}
				fixes = append(fixes, pageFixes...)
			}
		}

		// Walk annotations and remove forbidden /A and /AA. The array is read
		// alloc-first: forbidden-annots precedes this fixer and filters
		// /Annots, and an annotation it dropped must not be rewritten here.
		annotsObj, ok := pageDict["Annots"]
		if !ok {
			continue
		}

		annots := liveArray(ctx, alloc, doc, annotsObj)
		if len(annots) == 0 {
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
				if isForbiddenAction(ctx, alloc, doc, aObj, profile) {
					delete(newAnnot, "A")
					changed = true
					fixes = append(fixes, fmt.Sprintf("removed forbidden /A action from annotation on page %d", pageNum))
				}
			}

			// Remove forbidden entries from annotation /AA.
			if aaObj, hasAA := newAnnot["AA"]; hasAA {
				aaFixes, cleaned := removeForbiddenFromAADict(ctx, alloc, doc, aaObj, fmt.Sprintf("annotation on page %d", pageNum), profile)
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

// isForbiddenAction checks if the given object is an action dict whose type
// profile forbids (see [stripsActionType]). The action is read alloc-first,
// so the verdict judges the action an earlier fixer left.
func isForbiddenAction(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, profile string) bool {
	d := liveDictValue(ctx, alloc, doc, obj)
	if d == nil {
		return false
	}
	s, _ := document.ResolveName(ctx, doc, d["S"])
	if s == "" {
		return false
	}
	if stripsActionType(s, profile) {
		return true
	}
	// A Named action is forbidden unless its /N is one of the standard named
	// actions, in every PDF/A part: ISO 19005-1 6.6.1 test 2, ISO 19005-2/3
	// 6.5.1 test 2, ISO 19005-4 6.6.1 test 2. Mirror the validator (which
	// reuses the same compliance.AllowedNamedActions set): only a present,
	// non-standard /N is a violation, so a malformed action without /N is left
	// alone.
	if s == "Named" && compliance.PDFAPart(profile) != 0 {
		if n, _ := document.ResolveName(ctx, doc, d["N"]); n != "" {
			return !compliance.AllowedNamedActions[n]
		}
	}
	return false
}

// removeForbiddenFromAA removes entries with forbidden action types from an /AA
// dict on the given parent dict. Returns the fix descriptions and a new parent
// dict if changes were made, or nil if no changes were needed.
func removeForbiddenFromAA(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, parentDict ast.PdfDict, aaObj ast.PdfObject, location, profile string) ([]string, ast.PdfDict) {
	aaFixes, cleaned := removeForbiddenFromAADict(ctx, alloc, doc, aaObj, location, profile)
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
// dict, or nil if no changes were needed. The dict is read alloc-first: the
// cleaned copy replaces it in the parent, so a copy of the document's version
// would discard an earlier fixer's edit to an indirect /AA.
func removeForbiddenFromAADict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, aaObj ast.PdfObject, location, profile string) ([]string, ast.PdfDict) {
	aaDict := liveDictValue(ctx, alloc, doc, aaObj)
	if aaDict == nil {
		return nil, nil
	}

	// Sorted: the caller turns these into report lines, and ranging the
	// dictionary ordered them randomly between runs of the same file.
	var forbidden []ast.PdfName
	for _, k := range slices.Sorted(maps.Keys(aaDict)) {
		if isForbiddenAction(ctx, alloc, doc, aaDict[k], profile) {
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
		fixes = append(fixes, fmt.Sprintf("removed forbidden /AA/%s action from %s", string(k), location))
	}
	return fixes, cleaned
}
