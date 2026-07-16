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

// sortedDictKeys returns a colour-space (or colorants) dictionary's keys in
// deterministic order. Canonicalisation picks the first (alt, tintFunc) seen
// per colorant; iterating the dict in Go's randomised map order would make that
// choice, and therefore the remediated bytes, nondeterministic run to run
// (ISO contributing rule: deterministic output). Sorting the keys fixes the
// canonical pick to the same Separation every run.
func sortedDictKeys(d ast.PdfDict) []ast.PdfName {
	keys := make([]ast.PdfName, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// matchSepCanon matches PDF/A-2/3/4 §6.2.4.4 test 2 and ISO 19005-1
// §6.2.3.4 violations where two /Separation arrays with the same
// colorant name disagree on alternate space or tint transform.
func matchSepCanon(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.4.4", "6.2.3.4") {
		return false
	}
	return contextContains(v, "different tintTransform") ||
		contextContains(v, "different alternateSpace") ||
		contextContains(v, "uses alternate") && contextContains(v, "previously used")
}

// fixSepCanon scans every page's /Resources/ColorSpace entries for
// /Separation arrays and canonicalises all occurrences of a given
// colorant name to the first (alternate, tintTransform) pair seen.
// This satisfies PDF/A §6.2.4.4 test 2's same-name-same-definition
// requirement across the document.
func fixSepCanon(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	// Rewriting a Separation's alternate space or tint transform
	// changes rendered colour. That sits outside the content-preserving
	// default, so gate behind PreserveContent.
	if opts.PreserveContent {
		return nil, nil
	}
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}
	var pageEntries []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pageEntries, make(map[int]bool), 0)

	// Collect canonical (alt, tintFunc) per colorant name on the first
	// pass.
	canon := make(map[ast.PdfName]ast.PdfArray)
	for _, pe := range pageEntries {
		collectSeparations(ctx, alloc, doc, pe, canon)
	}
	if len(canon) == 0 {
		return nil, nil
	}

	// Second pass: rewrite every Separation whose alt or tint diverges
	// from the canonical entry.
	count := 0
	visited := make(map[int]bool)
	for _, pe := range pageEntries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		count += rewriteSeparations(ctx, alloc, doc, pe, canon, visited)
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("canonicalised %d Separation array(s)", count)}, nil
}

// collectSeparations records the first (alt, tintFunc) pair it sees per
// Separation colorant name into canon. Also scans nested DeviceN
// Colorants dictionaries.
func collectSeparations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, canon map[ast.PdfName]ast.PdfArray) {
	eachColorSpaceArray(ctx, alloc, doc, pe, func(arr ast.PdfArray) {
		name, altTint, ok := extractSeparation(arr)
		if ok {
			if _, already := canon[name]; !already {
				canon[name] = altTint
			}
		}
	})
}

// rewriteSeparations overwrites every Separation whose (alt, tintFunc)
// doesn't match canon[name]. Returns the number of arrays rewritten.
func rewriteSeparations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, canon map[ast.PdfName]ast.PdfArray, visited map[int]bool) int {
	count := 0
	eachColorSpaceArrayMutable(ctx, alloc, doc, pe, visited, func(arr ast.PdfArray) (ast.PdfArray, bool) {
		name, _, ok := extractSeparation(arr)
		if !ok {
			return nil, false
		}
		canonArr, has := canon[name]
		if !has {
			return nil, false
		}
		// Compare alt (index 2) and tint func ref (index 3). If any
		// differ, replace with canonical.
		if arraysEqual(arr[2:4], canonArr[2:4]) {
			return nil, false
		}
		newArr := make(ast.PdfArray, len(arr))
		copy(newArr, arr)
		newArr[2] = canonArr[2]
		newArr[3] = canonArr[3]
		count++
		return newArr, true
	})
	return count
}

// extractSeparation returns the colorant name, the 4-element Separation
// array, and ok=true when arr is a well-formed /Separation array.
func extractSeparation(arr ast.PdfArray) (ast.PdfName, ast.PdfArray, bool) {
	if len(arr) < 4 {
		return "", nil, false
	}
	sep, ok := arr[0].(ast.PdfName)
	if !ok || sep != "Separation" {
		return "", nil, false
	}
	name, ok := arr[1].(ast.PdfName)
	if !ok {
		return "", nil, false
	}
	return name, arr, true
}

// arraysEqual reports whether two PdfArrays are element-wise equal for
// PdfName and PdfIndirectRef elements (the only kinds extractSeparation
// cares about).
func arraysEqual(a, b ast.PdfArray) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !objectsEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// objectsEqual compares PDF name/indirect-ref pairs. Other PDF types
// (arrays, dicts, strings) are treated as "possibly equal" by reference
// comparison; this is sufficient for Separation alt-space / tintFunc
// slots which canonicalise as names or indirect refs.
func objectsEqual(a, b ast.PdfObject) bool {
	switch av := a.(type) {
	case ast.PdfName:
		if bv, ok := b.(ast.PdfName); ok {
			return av == bv
		}
	case ast.PdfIndirectRef:
		if bv, ok := b.(ast.PdfIndirectRef); ok {
			return av.ObjectNum == bv.ObjectNum
		}
	}
	return a == b
}

// eachColorSpaceArray walks every colour-space array reachable from the
// page's resources: direct /ColorSpace entries and the /Colorants
// values of DeviceN/NChannel arrays. The visitor is read-only.
func eachColorSpaceArray(ctx context.Context, _ *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, visit func(ast.PdfArray)) {
	resObj, ok := pe.dict["Resources"]
	if !ok {
		return
	}
	resDict, err := document.ResolveDict(ctx, doc, resObj)
	if err != nil || resDict == nil {
		return
	}
	csDict, err := document.ResolveDict(ctx, doc, resDict["ColorSpace"])
	if err != nil || csDict == nil {
		return
	}
	for _, key := range sortedDictKeys(csDict) {
		entry := csDict[key]
		arr, err := document.ResolveArray(ctx, doc, entry)
		if err != nil || len(arr) == 0 {
			continue
		}
		visit(arr)
		// DeviceN arrays nest Separations inside /Attributes/Colorants.
		if first, ok := arr[0].(ast.PdfName); ok && (first == "DeviceN" || first == "NChannel") && len(arr) >= 5 {
			if attrs, err := document.ResolveDict(ctx, doc, arr[4]); err == nil && attrs != nil {
				if colorants, err := document.ResolveDict(ctx, doc, attrs["Colorants"]); err == nil && colorants != nil {
					for _, cKey := range sortedDictKeys(colorants) {
						if cArr, err := document.ResolveArray(ctx, doc, colorants[cKey]); err == nil && len(cArr) > 0 {
							visit(cArr)
						}
					}
				}
			}
		}
	}
}

// eachColorSpaceArrayMutable is the write-capable companion to
// eachColorSpaceArray. When patch returns true the provided array
// replaces the existing entry through the allocator.
func eachColorSpaceArrayMutable(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, visited map[int]bool, patch func(ast.PdfArray) (ast.PdfArray, bool)) {
	resObj, ok := pe.dict["Resources"]
	if !ok {
		return
	}
	resDict, err := document.ResolveDict(ctx, doc, resObj)
	if err != nil || resDict == nil {
		return
	}
	csObj, ok := resDict["ColorSpace"]
	if !ok {
		return
	}
	csDict, err := document.ResolveDict(ctx, doc, csObj)
	if err != nil || csDict == nil {
		return
	}

	rewritten := make(ast.PdfDict, len(csDict))
	maps.Copy(rewritten, csDict)
	changed := false

	for _, key := range sortedDictKeys(csDict) {
		entry := csDict[key]
		ref, isRef := entry.(ast.PdfIndirectRef)
		if isRef {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}
		arr, err := document.ResolveArray(ctx, doc, entry)
		if err != nil || len(arr) == 0 {
			continue
		}

		// Attempt to patch the top-level array itself.
		if newArr, ok := patch(arr); ok {
			if isRef && ref.ObjectNum > 0 {
				alloc.AllocAt(ref.ObjectNum, newArr)
			} else {
				rewritten[key] = newArr
				changed = true
			}
			continue
		}

		// For DeviceN, also patch Colorants Separations.
		if first, ok := arr[0].(ast.PdfName); ok && (first == "DeviceN" || first == "NChannel") && len(arr) >= 5 {
			if attrsRef, isAttrsRef := arr[4].(ast.PdfIndirectRef); isAttrsRef && attrsRef.ObjectNum > 0 {
				if attrs, err := document.ResolveDict(ctx, doc, attrs(attrsRef)); err == nil && attrs != nil {
					patchColorants(ctx, alloc, doc, attrs, patch)
				}
			}
		}
	}
	if !changed {
		return
	}
	if csRef, ok := csObj.(ast.PdfIndirectRef); ok && csRef.ObjectNum > 0 {
		alloc.AllocAt(csRef.ObjectNum, rewritten)
		return
	}
	newRes := make(ast.PdfDict, len(resDict))
	maps.Copy(newRes, resDict)
	newRes["ColorSpace"] = rewritten
	if resRef, ok := resObj.(ast.PdfIndirectRef); ok && resRef.ObjectNum > 0 {
		alloc.AllocAt(resRef.ObjectNum, newRes)
		return
	}
	newPage := make(ast.PdfDict, len(pe.dict))
	maps.Copy(newPage, pe.dict)
	newPage["Resources"] = newRes
	if pe.ref.ObjectNum > 0 {
		alloc.AllocAt(pe.ref.ObjectNum, newPage)
	}
}

// attrs returns the indirect ref unchanged; it exists so patchColorants
// can accept either an inline dict or an indirect ref and resolve
// uniformly.
func attrs(obj ast.PdfObject) ast.PdfObject { return obj }

// patchColorants walks a DeviceN attributes dict's /Colorants sub-dict
// and rewrites each Separation entry the patch callback flags.
func patchColorants(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, attrs ast.PdfDict, patch func(ast.PdfArray) (ast.PdfArray, bool)) {
	colorantsObj, ok := attrs["Colorants"]
	if !ok {
		return
	}
	colorantsDict, err := document.ResolveDict(ctx, doc, colorantsObj)
	if err != nil || colorantsDict == nil {
		return
	}
	changed := false
	newColorants := make(ast.PdfDict, len(colorantsDict))
	maps.Copy(newColorants, colorantsDict)
	for _, k := range sortedDictKeys(colorantsDict) {
		arr, err := document.ResolveArray(ctx, doc, colorantsDict[k])
		if err != nil || len(arr) == 0 {
			continue
		}
		if newArr, ok := patch(arr); ok {
			newColorants[k] = newArr
			changed = true
		}
	}
	if !changed {
		return
	}
	if ref, ok := colorantsObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newColorants)
	}
}
