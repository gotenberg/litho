package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchICCDedup matches ISO 19005-4 §6.2.4.2 test 3 (an /ICCBased
// colour space identical to the document OutputIntent) and the related
// uncalibrated-CS clauses that can only be satisfied after a new
// OutputIntent is injected by output-intent. Running for both keeps
// the downstream dedup pass from missing files where the duplicate
// only emerges after injection.
func matchICCDedup(v compliance.Violation) bool {
	if clausePrefix(v, "6.2.4.2") {
		return contextContains(v, "identical to OutputIntent") ||
			contextContains(v, "identical to the current transparency blending")
	}
	return clausePrefix(v, "6.2.3.2", "6.2.3.3", "6.2.4.3")
}

// fixICCDedup replaces redundant /ICCBased references with their
// matching DeviceGray/DeviceRGB/DeviceCMYK counterpart. PDF/A-4
// §6.2.4.2 test 3 forbids an ICCBased space that duplicates the
// OutputIntent profile; the DeviceN replacement fulfils the same
// semantics without the extra stream.
func fixICCDedup(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	oiProfile := firstOutputIntentProfile(ctx, alloc, doc, catalogDict)
	if oiProfile == nil {
		return nil, nil
	}

	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}
	var pageEntries []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pageEntries, make(map[int]bool), 0)

	visited := make(map[int]bool)
	count := 0
	for _, pe := range pageEntries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		count += dedupICCInResources(ctx, alloc, doc, pe, oiProfile, visited)
		count += dedupICCInXObjects(ctx, alloc, doc, pe, oiProfile, visited)
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("replaced %d redundant ICCBased colour space(s)", count)}, nil
}

// firstOutputIntentProfile returns the decoded bytes of the first
// OutputIntent's DestOutputProfile, or nil when unavailable. Checks
// the allocator overlay before the source document so it picks up a
// remediation-added OutputIntent.
func firstOutputIntentProfile(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) []byte {
	oiObj, ok := catalog["OutputIntents"]
	if !ok {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, oiObj)
	if err != nil || len(arr) == 0 {
		if inline, ok := oiObj.(ast.PdfArray); ok {
			arr = inline
		} else {
			return nil
		}
	}
	oiEntry := arr[0]
	var oiDict ast.PdfDict
	if ref, isRef := oiEntry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			if d, ok := pending.(ast.PdfDict); ok {
				oiDict = d
			}
		}
	}
	if oiDict == nil {
		d, err := document.ResolveDict(ctx, doc, oiEntry)
		if err != nil || d == nil {
			return nil
		}
		oiDict = d
	}
	dpObj := oiDict["DestOutputProfile"]
	var stream ast.PdfStream
	if ref, isRef := dpObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			if s, ok := pending.(ast.PdfStream); ok {
				stream = s
			}
		}
	}
	if stream.Dict == nil {
		s, err := document.ResolveStream(ctx, doc, dpObj)
		if err != nil {
			return nil
		}
		stream = s
	}
	data, err := stream.Bytes(ctx)
	if err != nil {
		return nil
	}
	return data
}

// dedupICCInResources walks a page's resources and rewrites /CSx =
// [/ICCBased ref] entries whose stream matches oiProfile. The paired
// device colour space is chosen by the profile's component count.
func dedupICCInResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, oiProfile []byte, visited map[int]bool) int {
	resObj, ok := pe.dict["Resources"]
	if !ok {
		return 0
	}
	resDict, err := document.ResolveDict(ctx, doc, resObj)
	if err != nil || resDict == nil {
		return 0
	}
	csObj, ok := resDict["ColorSpace"]
	if !ok {
		return 0
	}
	csDict, err := document.ResolveDict(ctx, doc, csObj)
	if err != nil || csDict == nil {
		return 0
	}

	inlineUpdates := make(map[ast.PdfName]ast.PdfObject)
	for key, entry := range csDict {
		ref, isRef := entry.(ast.PdfIndirectRef)
		if isRef {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}
		arr, err := document.ResolveArray(ctx, doc, entry)
		if err != nil || len(arr) < 2 {
			continue
		}
		name, ok := arr[0].(ast.PdfName)
		if !ok || name != "ICCBased" {
			continue
		}
		iccStream, err := document.ResolveStream(ctx, doc, arr[1])
		if err != nil {
			continue
		}
		iccData, err := iccStream.Bytes(ctx)
		if err != nil {
			continue
		}
		if !bytes.Equal(iccData, oiProfile) {
			continue
		}
		device := deviceNameForComponents(iccStream.Dict)
		if device == "" {
			continue
		}
		if isRef && ref.ObjectNum > 0 {
			alloc.AllocAt(ref.ObjectNum, device)
			continue
		}
		inlineUpdates[key] = device
	}

	if len(inlineUpdates) == 0 {
		return 0
	}

	newCS := make(ast.PdfDict, len(csDict))
	maps.Copy(newCS, csDict)
	maps.Copy(newCS, inlineUpdates)
	if csRef, ok := csObj.(ast.PdfIndirectRef); ok && csRef.ObjectNum > 0 {
		alloc.AllocAt(csRef.ObjectNum, newCS)
		return len(inlineUpdates)
	}
	newRes := make(ast.PdfDict, len(resDict))
	maps.Copy(newRes, resDict)
	newRes["ColorSpace"] = newCS
	if resRef, ok := resObj.(ast.PdfIndirectRef); ok && resRef.ObjectNum > 0 {
		alloc.AllocAt(resRef.ObjectNum, newRes)
		return len(inlineUpdates)
	}
	newPage := make(ast.PdfDict, len(pe.dict))
	maps.Copy(newPage, pe.dict)
	newPage["Resources"] = newRes
	if pe.ref.ObjectNum > 0 {
		alloc.AllocAt(pe.ref.ObjectNum, newPage)
	}
	return len(inlineUpdates)
}

// dedupICCInXObjects walks every Form XObject reachable from a page's
// /Resources/XObject sub-dict and applies dedupICCInResources to its
// own /Resources. PDF/A §6.2.4.2 test 3 also covers Form XObject
// ColorSpace entries, not just page-level ones.
func dedupICCInXObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, oiProfile []byte, visited map[int]bool) int {
	resObj, ok := pe.dict["Resources"]
	if !ok {
		return 0
	}
	resDict, err := document.ResolveDict(ctx, doc, resObj)
	if err != nil || resDict == nil {
		return 0
	}
	xoDict, err := document.ResolveDict(ctx, doc, resDict["XObject"])
	if err != nil || xoDict == nil {
		return 0
	}
	count := 0
	for _, entry := range xoDict {
		ref, ok := entry.(ast.PdfIndirectRef)
		if !ok || ref.ObjectNum == 0 {
			continue
		}
		raw, err := doc.Resolve(ctx, ref)
		if err != nil {
			continue
		}
		stream, ok := raw.(ast.PdfStream)
		if !ok {
			continue
		}
		if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); subtype != "Form" {
			continue
		}
		formPE := annotPageEntry{ref: ref, dict: stream.Dict}
		count += dedupICCInResources(ctx, alloc, doc, formPE, oiProfile, visited)
	}
	return count
}

// deviceNameForComponents maps an ICCBased /N count to its matching
// DeviceGray/RGB/CMYK PdfName, or returns an empty name for unknown
// component counts.
func deviceNameForComponents(dict ast.PdfDict) ast.PdfName {
	n, ok := dict["N"].(ast.PdfInt)
	if !ok {
		return ""
	}
	switch n {
	case 1:
		return "DeviceGray"
	case 3:
		return "DeviceRGB"
	case 4:
		return "DeviceCMYK"
	}
	return ""
}
