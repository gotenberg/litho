package remediation

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

//go:embed srgb.icc
var sRGBICCProfile []byte

//go:embed cmyk.icc
var cmykICCProfile []byte

// hasMatchingOutputIntent checks if any existing OutputIntent entry has an
// ICC profile with the needed color space (CMYK if needsCMYK, RGB otherwise).
func hasMatchingOutputIntent(ctx context.Context, doc *document.Document, arr ast.PdfArray, needsCMYK bool) bool {
	for _, entry := range arr {
		oiDict, err := document.ResolveDict(ctx, doc, entry)
		if err != nil || oiDict == nil {
			continue
		}
		dpObj, ok := oiDict["DestOutputProfile"]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, dpObj)
		if err != nil {
			continue
		}
		data, err := stream.Bytes(ctx)
		if err != nil || len(data) < 20 {
			continue
		}
		colorSpace := string(data[16:20])
		if needsCMYK && colorSpace == "CMYK" {
			return true
		}
		if !needsCMYK && colorSpace == "RGB " {
			return true
		}
	}
	return false
}

func matchOutputIntent(v compliance.Violation) bool {
	return clausePrefix(v, "6.2.3.2", "6.2.3.3", "6.2.4.3") // device CS without OutputIntent
}

// matchOutputIntentCleanup covers structural OutputIntent violations:
// invalid ICC profile, duplicate DestOutputProfile mismatch, and the
// forbidden DestOutputProfileRef key (PDF/A-2 §6.2.3 / PDF/A-4 §6.2.3).
func matchOutputIntentCleanup(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.3") {
		return false
	}
	return contextContains(v, "ICC profile") ||
		contextContains(v, "DestOutputProfile") ||
		contextContains(v, "DestOutputProfileRef")
}

// fixOutputIntentCleanup normalises the OutputIntents array:
//
//  1. Strips forbidden /DestOutputProfileRef entries.
//  2. Re-points a later entry's /DestOutputProfile to the first entry's
//     value when they diverge, satisfying §6.2.3 test 2.
//  3. Replaces /DestOutputProfile ICC streams that the validator
//     rejects as too short with the embedded sRGB profile.
func fixOutputIntentCleanup(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	oiObj, ok := catalogDict["OutputIntents"]
	if !ok {
		return nil, nil
	}
	arr, err := document.ResolveArray(ctx, doc, oiObj)
	if err != nil || len(arr) == 0 {
		return nil, nil
	}

	var fixes []string
	var firstProfile ast.PdfIndirectRef
	changedArr := false
	newArr := make(ast.PdfArray, len(arr))

	for i, entry := range arr {
		newArr[i] = entry
		ref, isRef := entry.(ast.PdfIndirectRef)
		var entryDict ast.PdfDict
		if isRef && ref.ObjectNum > 0 {
			entryDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		}
		if entryDict == nil {
			if d, err := document.ResolveDict(ctx, doc, entry); err == nil {
				entryDict = d
			}
		}
		if entryDict == nil {
			continue
		}
		entryChanged := false
		updated := make(ast.PdfDict, len(entryDict))
		maps.Copy(updated, entryDict)

		if _, has := updated["DestOutputProfileRef"]; has {
			delete(updated, "DestOutputProfileRef")
			entryChanged = true
			fixes = append(fixes, fmt.Sprintf("removed /DestOutputProfileRef from OutputIntent[%d]", i))
		}

		dpObj, hasDP := updated["DestOutputProfile"]
		if hasDP {
			if dpRef, isDPRef := dpObj.(ast.PdfIndirectRef); isDPRef {
				if i == 0 {
					firstProfile = dpRef
				} else if firstProfile.ObjectNum > 0 && dpRef != firstProfile {
					updated["DestOutputProfile"] = firstProfile
					entryChanged = true
					fixes = append(fixes, fmt.Sprintf("unified /DestOutputProfile on OutputIntent[%d]", i))
				}
			}
			if stream, err := document.ResolveStream(ctx, doc, updated["DestOutputProfile"]); err == nil {
				if data, err := stream.Bytes(ctx); err == nil && len(data) < 128 {
					// Replace with a stock sRGB profile when the existing
					// stream is too short to be a valid ICC header.
					iccStream := ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, sRGBICCProfile)
					iccRef := alloc.Alloc(iccStream)
					updated["DestOutputProfile"] = iccRef
					firstProfile = iccRef
					entryChanged = true
					fixes = append(fixes, fmt.Sprintf("replaced too-short ICC profile on OutputIntent[%d]", i))
				}
			}
		}

		if !entryChanged {
			continue
		}
		if isRef && ref.ObjectNum > 0 {
			alloc.AllocAt(ref.ObjectNum, updated)
		} else {
			newArr[i] = updated
			changedArr = true
		}
	}

	if changedArr {
		newCat := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCat, catalogDict)
		newCat["OutputIntents"] = newArr
		alloc.AllocAt(catalogRef.ObjectNum, newCat)
	}

	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

func fixOutputIntent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, profile string) ([]string, error) {
	// Skip structurally damaged documents where embedding a large ICC
	// profile risks corruption during serialization.
	if doc.Rebuilt {
		return nil, nil
	}

	// Skip documents with implementation-limit or structural issues that
	// can corrupt newly added objects during serialization.
	for objNum, entry := range doc.Index.Entries {
		if entry.Offset > doc.FileSize() || int64(objNum) > 100000 {
			return nil, nil
		}
	}

	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	// Classify the colorspaces flagged as needing coverage by the
	// violations. When both CMYK and RGB are flagged only one can be
	// covered by the catalog-level OutputIntent; the other needs a
	// Default{RGB,CMYK} injected into resource dicts.
	needsCMYK := false
	needsRGB := false
	for _, v := range violations {
		ctxStr := strings.ToLower(v.Context)
		if strings.Contains(ctxStr, "devicecmyk") {
			needsCMYK = true
		}
		if strings.Contains(ctxStr, "devicergb") {
			needsRGB = true
		}
	}
	if !needsCMYK && !needsRGB {
		needsRGB = true
	}

	// Inspect existing OutputIntents to avoid duplicating coverage.
	hasCMYKOI, hasRGBOI := false, false
	if oiObj, ok := catalogDict["OutputIntents"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, oiObj); err == nil && len(arr) > 0 {
			hasCMYKOI = hasMatchingOutputIntent(ctx, doc, arr, true)
			hasRGBOI = hasMatchingOutputIntent(ctx, doc, arr, false)
		}
	}

	var fixes []string
	oiIsCMYK := hasCMYKOI
	oiIsRGB := hasRGBOI

	// Decide whether to add a new catalog-level OutputIntent. Skip when
	// the document already has one that covers a needed colorspace.
	addOI := false
	addCMYK := false
	if needsCMYK && !hasCMYKOI && !hasRGBOI {
		addOI = true
		addCMYK = true
	} else if needsRGB && !hasRGBOI && !hasCMYKOI {
		addOI = true
		addCMYK = false
	}

	if addOI {
		iccData, nComponents, conditionID := cmykICCProfile, 4, "US Web Coated (SWOP) v2"
		if !addCMYK {
			iccData, nComponents, conditionID = sRGBICCProfile, 3, "sRGB IEC61966-2.1"
		}
		iccStream := ast.NewStream(ast.PdfDict{"N": ast.PdfInt(nComponents)}, iccData)
		iccRef := alloc.Alloc(iccStream)

		sValue := "GTS_PDFA1"
		if strings.HasPrefix(profile, "PDF/A-4") {
			sValue = "GTS_PDFA"
		}
		outputIntent := ast.PdfDict{
			"Type":                      ast.PdfName("OutputIntent"),
			"S":                         ast.PdfName(sValue),
			"OutputConditionIdentifier": ast.PdfString(conditionID),
			"DestOutputProfile":         iccRef,
		}
		oiRef := alloc.Alloc(outputIntent)

		newCatalog := make(ast.PdfDict, len(catalogDict)+1)
		maps.Copy(newCatalog, catalogDict)
		newCatalog["OutputIntents"] = ast.PdfArray{oiRef}
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
		catalogDict = newCatalog
		if addCMYK {
			fixes = append(fixes, "added CMYK OutputIntent to catalog")
			oiIsCMYK = true
		} else {
			fixes = append(fixes, "added sRGB OutputIntent to catalog")
			oiIsRGB = true
		}
	}

	// When an OutputIntent was just added, any existing ICCBased page
	// colorspace that happens to share the profile now violates
	// §6.2.4.2. Dedup them immediately.
	if addOI {
		if newCat := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef); newCat != nil {
			if profile := firstOutputIntentProfile(ctx, alloc, doc, newCat); profile != nil {
				visited := make(map[int]bool)
				var entries []annotPageEntry
				if pagesObj, ok := newCat["Pages"]; ok {
					collectPageRefs(ctx, alloc, doc, pagesObj, &entries, 0)
				}
				for _, pe := range entries {
					dedupICCInResources(ctx, alloc, doc, pe, profile, visited)
					dedupICCInXObjects(ctx, alloc, doc, pe, profile, visited)
				}
			}
		}
	}

	// Inject a Default{RGB,CMYK} for the colorspace the OutputIntent
	// does not cover. Validators only report a single colorspace
	// violation even when a page mixes DeviceCMYK and DeviceRGB, so
	// pre-stage the paired default whenever an OI was added.
	var defaultColor ast.PdfName
	var iccData []byte
	var nComponents int
	switch {
	case oiIsCMYK && !oiIsRGB:
		defaultColor, iccData, nComponents = "DefaultRGB", sRGBICCProfile, 3
	case oiIsRGB && !oiIsCMYK:
		defaultColor, iccData, nComponents = "DefaultCMYK", cmykICCProfile, 4
	default:
		return fixes, nil
	}
	iccStream := ast.NewStream(ast.PdfDict{"N": ast.PdfInt(nComponents)}, iccData)
	iccRef := alloc.Alloc(iccStream)
	csArr := ast.PdfArray{ast.PdfName("ICCBased"), iccRef}
	if n, err := injectDefaultColorSpace(ctx, alloc, doc, catalogDict, defaultColor, csArr); err == nil && n > 0 {
		fixes = append(fixes, fmt.Sprintf("injected /%s into %d page(s)", defaultColor, n))
	}

	return fixes, nil
}

// injectDefaultColorSpace walks the page tree plus every reachable Form
// XObject and Pattern resource dict, adding /Default{RGB,CMYK,Gray} where
// missing. Patterns and Form XObjects have their own /Resources that PDF/A
// §6.2.3.3 treats as independent scopes, so a page-only fix leaves
// "used in pattern/form" violations behind.
func injectDefaultColorSpace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, key ast.PdfName, csArr ast.PdfArray) (int, error) {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return 0, nil
	}
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, 0)

	visited := make(map[int]bool)
	count := 0
	for _, pe := range pages {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}
		resolved, ok := pe.dict["Resources"]
		if !ok {
			newRes := ast.PdfDict{"ColorSpace": ast.PdfDict{key: csArr}}
			newPage := make(ast.PdfDict, len(pe.dict)+1)
			maps.Copy(newPage, pe.dict)
			newPage["Resources"] = newRes
			if pe.ref.ObjectNum > 0 {
				alloc.AllocAt(pe.ref.ObjectNum, newPage)
				count++
			}
			continue
		}
		patched := patchResourceColorSpace(ctx, alloc, doc, resolved, key, csArr, visited)
		if patched.changed {
			newPage := make(ast.PdfDict, len(pe.dict)+1)
			maps.Copy(newPage, pe.dict)
			newPage["Resources"] = patched.value
			if pe.ref.ObjectNum > 0 {
				alloc.AllocAt(pe.ref.ObjectNum, newPage)
				count++
			}
		}
	}
	return count, nil
}

// patchResult is the return shape of patchResourceColorSpace: the updated
// resource dict (inline or indirect) and whether anything changed.
type patchResult struct {
	value   ast.PdfObject
	changed bool
}

// patchResourceColorSpace ensures the given /Resources object defines
// `key` inside its ColorSpace dict and recurses into every Form XObject
// and Pattern referenced by XObject/Pattern entries. Returns the value
// to assign back to the parent (unchanged when nothing needed patching).
func patchResourceColorSpace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfObject, key ast.PdfName, csArr ast.PdfArray, visited map[int]bool) patchResult {
	var resDict ast.PdfDict
	var resRef ast.PdfIndirectRef
	if ref, ok := resources.(ast.PdfIndirectRef); ok {
		resRef = ref
		if visited[ref.ObjectNum] {
			return patchResult{value: resources}
		}
		visited[ref.ObjectNum] = true
		resDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if resDict == nil {
		if d, err := document.ResolveDict(ctx, doc, resources); err == nil {
			resDict = d
		}
	}
	if resDict == nil {
		return patchResult{value: resources}
	}

	changed := false
	newRes := make(ast.PdfDict, len(resDict)+1)
	maps.Copy(newRes, resDict)

	// Best-effort: an unreadable /ColorSpace resource yields a nil map,
	// handled by the nil check below.
	csMap, _ := document.ResolveDict(ctx, doc, resDict["ColorSpace"])
	if csMap == nil || csMap[key] == nil {
		base := csMap
		if base == nil {
			base = ast.PdfDict{}
		}
		newCS := make(ast.PdfDict, len(base)+1)
		maps.Copy(newCS, base)
		newCS[key] = csArr
		newRes["ColorSpace"] = newCS
		changed = true
	}

	changedDown := recurseColorSpacePatch(ctx, alloc, doc, newRes, "XObject", key, csArr, visited)
	changedDown = recurseColorSpacePatch(ctx, alloc, doc, newRes, "Pattern", key, csArr, visited) || changedDown
	if changedDown {
		changed = true
	}

	if !changed {
		return patchResult{value: resources}
	}
	if resRef.ObjectNum > 0 {
		alloc.AllocAt(resRef.ObjectNum, newRes)
		return patchResult{value: resRef, changed: true}
	}
	return patchResult{value: newRes, changed: true}
}

// recurseColorSpacePatch walks a named resource sub-dict (XObject or
// Pattern) and updates each entry that carries its own /Resources.
func recurseColorSpacePatch(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, newRes ast.PdfDict, name ast.PdfName, key ast.PdfName, csArr ast.PdfArray, visited map[int]bool) bool {
	subObj, ok := newRes[name]
	if !ok {
		return false
	}
	subDict, err := document.ResolveDict(ctx, doc, subObj)
	if err != nil || subDict == nil {
		return false
	}
	changed := false
	for _, entryObj := range subDict {
		ref, isRef := entryObj.(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum == 0 {
			continue
		}
		if visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		// Resolve the raw object: XObject Form and TilingPattern entries
		// are streams, ShadingPattern entries are dicts, so pre-resolve
		// and branch on type.
		rawObj, err := doc.Resolve(ctx, ref)
		if err != nil || rawObj == nil {
			continue
		}
		if stream, isStream := rawObj.(ast.PdfStream); isStream {
			resObj, hasRes := stream.Dict["Resources"]
			if !hasRes {
				continue
			}
			patched := patchResourceColorSpace(ctx, alloc, doc, resObj, key, csArr, visited)
			if !patched.changed {
				continue
			}
			newDict := make(ast.PdfDict, len(stream.Dict)+1)
			maps.Copy(newDict, stream.Dict)
			newDict["Resources"] = patched.value
			raw, err := stream.Bytes(ctx)
			if err != nil {
				// Form XObject body unreadable: skip the Resources
				// patch rather than rewriting the stream with empty
				// content. The validator will keep flagging the
				// underlying issue, but at least the appearance stream
				// stays intact.
				doc.Logger.Debug("remediation: skipping Form XObject Resources rewrite, body read failed",
					slog.Int("obj", ref.ObjectNum), slog.Any("err", err))
				continue
			}
			alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, raw))
			changed = true
			continue
		}
		if target, ok := rawObj.(ast.PdfDict); ok {
			resObj, hasRes := target["Resources"]
			if !hasRes {
				continue
			}
			patched := patchResourceColorSpace(ctx, alloc, doc, resObj, key, csArr, visited)
			if !patched.changed {
				continue
			}
			newTarget := make(ast.PdfDict, len(target)+1)
			maps.Copy(newTarget, target)
			newTarget["Resources"] = patched.value
			alloc.AllocAt(ref.ObjectNum, newTarget)
			changed = true
		}
	}
	return changed
}
