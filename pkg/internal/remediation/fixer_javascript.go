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

// stripsActionType reports whether the fixer removes an action of type s under
// profile. The verdict is the validator's own ([compliance.IsForbiddenActionType]):
// a type outside the whitelist ISO 19005-1 6.6.1 test 1, ISO 19005-2/3 6.5.1
// test 1 or ISO 19005-4 6.6.1 test 1 states for the profile's part and
// conformance. An action the profile permits, such as JavaScript under
// PDF/A-4, is never stripped.
//
// The fixer keeps no list of its own. A hand list of forbidden types used to
// nominate what was stripped, and it had drifted from the whitelists: GoToE is
// outside the part 1 whitelist, RichMediaExecute and GoToDp outside the part 1
// to 3 ones, and the validator flagged all three while the fixer kept them.
//
// # Corpus evidence
//
//   - litho forbidden_mix.pdf: remediated as PDF/A-4, PDF/A-4e or PDF/A-4f, it
//     lost the /Names/JavaScript tree those profiles permit, because its other
//     forbidden actions ran this fixer. The residual rules were the same
//     either way.
//   - pdfjs issue17056.pdf, issue8844.pdf, issue18030.pdf: remediated as
//     PDF/A-1b, they kept 30, 1 and 1 GoToE link actions that litho and
//     veraPDF both report under 6.6.1 test 1. Stripped, neither reports any.
//   - verapdf PDF_UA-1__7.11-t01-fail-a.pdf, -fail-b.pdf, -pass-a.pdf and
//     PDF_UA-2__8.14.1-t01-fail-a.pdf: the same, one GoToE action each.
func stripsActionType(s ast.PdfName, profile string) bool {
	return compliance.IsForbiddenActionType(s, profile)
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

	// Form fields carry their own /AA, which the validator judges through the
	// AcroForm field tree rather than through any page. The walk runs before
	// the catalog write because an inline field tree can only be written back
	// through the catalog.
	fieldFixes, newCatalog := fixForbiddenActionsOnFields(ctx, alloc, doc, catalogDict, profile)
	if newCatalog != nil {
		catalogDict = newCatalog
		catalogChanged = true
	}
	fixes = append(fixes, fieldFixes...)

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

		var inlineFixed map[int]ast.PdfDict
		var inlineFixes []string
		for i, annotObj := range annots {
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

			newAnnot, annotFixes := stripAnnotationActions(ctx, alloc, doc, annotDict, pageNum, profile)
			if newAnnot == nil {
				continue
			}
			if annotRef.ObjectNum > 0 {
				alloc.AllocAt(annotRef.ObjectNum, newAnnot)
				fixes = append(fixes, annotFixes...)
				continue
			}
			// ISO 32000-1, 12.5.2 does not require an annotation to be an
			// indirect object. One written inline in /Annots has no object
			// number to address, so the repair was reported and never
			// written. It goes back through the array, and is reported only
			// once that write lands.
			if inlineFixed == nil {
				inlineFixed = make(map[int]ast.PdfDict, 1)
			}
			inlineFixed[i] = newAnnot
			inlineFixes = append(inlineFixes, annotFixes...)
		}
		if installInlineAnnots(ctx, alloc, doc, pe, annotsObj, annots, inlineFixed) > 0 {
			fixes = append(fixes, inlineFixes...)
		}
	}

	return fixes, nil
}

// stripAnnotationActions returns a copy of annotDict without the forbidden
// actions its /A and /AA hold, with the fix descriptions, or nil when it holds
// none.
func stripAnnotationActions(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, annotDict ast.PdfDict, pageNum int, profile string) (ast.PdfDict, []string) {
	var newAnnot ast.PdfDict
	var fixes []string
	clone := func() {
		if newAnnot == nil {
			newAnnot = maps.Clone(annotDict)
		}
	}

	if aObj, hasA := annotDict["A"]; hasA && isForbiddenAction(ctx, alloc, doc, aObj, profile) {
		clone()
		delete(newAnnot, "A")
		fixes = append(fixes, fmt.Sprintf("removed forbidden /A action from annotation on page %d", pageNum))
	}

	if aaObj, hasAA := annotDict["AA"]; hasAA {
		aaFixes, cleaned := removeForbiddenFromAADict(ctx, alloc, doc, aaObj, fmt.Sprintf("annotation on page %d", pageNum), profile)
		if cleaned != nil {
			clone()
			if len(cleaned) == 0 {
				delete(newAnnot, "AA")
			} else {
				newAnnot["AA"] = cleaned
			}
			fixes = append(fixes, aaFixes...)
		}
	}
	return newAnnot, fixes
}

// maxFieldTreeDepth bounds the descent of the AcroForm field tree through /Kids
// in [fieldActionWalk]. Anti-DoS guard, not spec-mandated: ISO 32000-1,
// 12.7.3.1 puts no depth on the field tree, and the visited set is what ends a
// cyclic /Kids. The value is the bound compliance's walkFormFields descends to
// (its maxActionChainDepth), so the fixer reaches every field whose actions
// the validator judges. [maxFieldParentDepth] bounds the upward /Parent walk,
// a different shape.
const maxFieldTreeDepth = 200

// fixForbiddenActionsOnFields strips the forbidden actions from the /AA of
// every form field the AcroForm /Fields tree lists (ISO 19005-1 6.6.1, ISO
// 19005-2/3 6.5.1, ISO 19005-4 6.6.1: the action-type restrictions reach every
// action in the file). The page walk cannot reach them: a non-terminal field
// is no annotation, and a terminal one merged with its widget is judged there
// under the annotation triggers alone.
//
// Every level is read through the allocator and written back where it lives:
// an indirect field or /Kids array at its own object number, an inline one
// into its parent. It returns the catalog to write when the AcroForm
// dictionary is inline in it and changed, or nil.
//
// # Corpus evidence
//
//   - litho annots_1b.pdf: the non-terminal field "parent" holds a JavaScript
//     action under /AA/K, and at PDF/A-2b the output kept 6.5.1 test 1 and
//     6.4.1 test 2 for it.
//   - pdfjs issue15092.pdf, issue16863.pdf, issue19753.pdf: remediated as
//     PDF/A-2b they kept 3, 8 and 18 field actions under 6.5.1 test 1, and 1,
//     4 and 10 fields under 6.4.1 test 2, as PDF/A-1b the same counts under
//     6.6.1 test 1 and 6.6.2 test 2. The walk clears every one, in litho and
//     in veraPDF, since each /AA held nothing else.
func fixForbiddenActionsOnFields(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, profile string) ([]string, ast.PdfDict) {
	acroFormObj, ok := catalog["AcroForm"]
	if !ok {
		return nil, nil
	}
	acroForm := liveDictValue(ctx, alloc, doc, acroFormObj)
	if acroForm == nil {
		return nil, nil
	}
	fieldsObj, ok := acroForm["Fields"]
	if !ok {
		return nil, nil
	}

	w := fieldActionWalk{alloc: alloc, doc: doc, profile: profile, seen: make(map[int]bool)}
	newFields, changed := w.array(ctx, fieldsObj, true, 0)
	if !changed {
		return w.fixes, nil
	}
	newAcroForm := maps.Clone(acroForm)
	newAcroForm["Fields"] = newFields
	if ref, isRef := acroFormObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newAcroForm)
		return w.fixes, nil
	}
	newCatalog := maps.Clone(catalog)
	newCatalog["AcroForm"] = newAcroForm
	return w.fixes, newCatalog
}

// fieldActionWalk carries the state of one [fixForbiddenActionsOnFields] walk.
// seen processes each field object once, so a shared or cyclic /Kids tree
// cannot force exponential work or loop.
type fieldActionWalk struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	profile string
	seen    map[int]bool
	fixes   []string
}

// array walks one /Fields or /Kids array. It returns the value the parent
// holds and true when that value changed: an indirect array is written at its
// own object number and the parent keeps its reference.
//
// listedByField says whether arrObj is the /Fields array or a form field's
// /Kids, the only lists compliance's walkFormFields draws form fields from.
func (w *fieldActionWalk) array(ctx context.Context, arrObj ast.PdfObject, listedByField bool, depth int) (ast.PdfObject, bool) {
	if ctx.Err() != nil || depth > maxFieldTreeDepth {
		return arrObj, false
	}
	arr := liveArray(ctx, w.alloc, w.doc, arrObj)
	var newArr ast.PdfArray
	for i, item := range arr {
		if ctx.Err() != nil {
			break
		}
		ref, isRef := item.(ast.PdfIndirectRef)
		if isRef {
			if w.seen[ref.ObjectNum] {
				continue
			}
			w.seen[ref.ObjectNum] = true
		}
		field := liveDictValue(ctx, w.alloc, w.doc, item)
		if field == nil {
			continue
		}
		location := "inline form field"
		if isRef {
			location = fmt.Sprintf("form field %d", ref.ObjectNum)
		}
		newField := w.field(ctx, field, location, listedByField, depth)
		if newField == nil {
			continue
		}
		if isRef && ref.ObjectNum > 0 {
			w.alloc.AllocAt(ref.ObjectNum, newField)
			continue
		}
		if newArr == nil {
			newArr = slices.Clone(arr)
		}
		newArr[i] = newField
	}
	if newArr == nil {
		return arrObj, false
	}
	if ref, isRef := arrObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		w.alloc.AllocAt(ref.ObjectNum, newArr)
		return arrObj, false
	}
	return newArr, true
}

// field returns a copy of one field dictionary without the forbidden actions
// its /AA holds and with its rewritten /Kids, or nil when neither changed.
//
// Only a form field's /AA is judged, mirroring compliance's walkFormFields:
// every dictionary /Fields lists, and below a field only the kids carrying a
// non-null partial name /T (ISO 32000-1, 12.7.3.1; a null entry is absent per
// 7.3.7). Any other kid is a widget, whose /AA the page walk judges as an
// annotation's.
func (w *fieldActionWalk) field(ctx context.Context, field ast.PdfDict, location string, listedByField bool, depth int) ast.PdfDict {
	isField := listedByField && (depth == 0 || hasNonNullEntry(field, "T"))

	var newField ast.PdfDict
	if aaObj, hasAA := field["AA"]; hasAA && isField {
		aaFixes, cleaned := removeForbiddenFromAADict(ctx, w.alloc, w.doc, aaObj, location, w.profile)
		if cleaned != nil {
			newField = maps.Clone(field)
			if len(cleaned) == 0 {
				delete(newField, "AA")
			} else {
				newField["AA"] = cleaned
			}
			w.fixes = append(w.fixes, aaFixes...)
		}
	}

	if kidsObj, hasKids := field["Kids"]; hasKids {
		if newKids, changed := w.array(ctx, kidsObj, isField, depth+1); changed {
			if newField == nil {
				newField = maps.Clone(field)
			}
			newField["Kids"] = newKids
		}
	}
	return newField
}

// hasNonNullEntry reports whether dict holds key with a value other than the
// null object, which ISO 32000-1, 7.3.7 makes equivalent to an absent entry.
func hasNonNullEntry(dict ast.PdfDict, key ast.PdfName) bool {
	v, ok := dict[key]
	if !ok {
		return false
	}
	_, isNull := v.(ast.PdfNull)
	return !isNull
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
