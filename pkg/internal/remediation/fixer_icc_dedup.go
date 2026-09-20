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
		// The clause names two profiles, and the page's blending space is the
		// one the OutputIntent cannot stand in for.
		pageProfiles := profileSet{
			outputIntent: oiProfile,
			blending:     blendingProfile(ctx, alloc, doc, pe.dict),
		}
		count += dedupICCInResources(ctx, alloc, doc, pe, pageProfiles.all(), visited)
		count += dedupICCInXObjects(ctx, alloc, doc, pe, pageProfiles, visited)
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
	return iccProfileData(ctx, doc, stream)
}

// matchesAnyProfile reports whether an ICCBased stream carries one of the
// profiles the clause forbids duplicating. The comparison is the detector's,
// so a colour space litho reports is one litho replaces.
func matchesAnyProfile(ctx context.Context, doc *document.Document, stream ast.PdfStream, profiles [][]byte) bool {
	data := iccProfileData(ctx, doc, stream)
	if len(data) == 0 {
		return false
	}
	for _, p := range profiles {
		if compliance.ICCProfilesEqual(data, p) {
			return true
		}
	}
	return false
}

// profileSet holds the two profiles ISO 19005-4, 6.2.4.2 test 3 names. Either
// may be absent.
type profileSet struct {
	outputIntent []byte
	blending     []byte
}

// all returns the profiles that are available, in the order the clause lists
// them.
func (p profileSet) all() [][]byte {
	out := make([][]byte, 0, 2)
	for _, profile := range [][]byte{p.outputIntent, p.blending} {
		if len(profile) > 0 {
			out = append(out, profile)
		}
	}
	return out
}

// blendingProfile returns the ICC profile a transparency group blends in, or
// nil when the dict declares no group or blends in something other than an
// ICCBased space (ISO 32000-1, 11.6.6).
//
// ISO 19005-4, 6.2.4.2 test 3 forbids an ICCBased colour space identical to
// the OutputIntent profile OR to this one, and the two are routinely
// different: an sRGB output intent with a CMYK blending space leaves the
// detector reporting a duplicate the OutputIntent comparison alone cannot see.
func blendingProfile(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, dict ast.PdfDict) []byte {
	group, err := document.ResolveDict(ctx, doc, dict["Group"])
	if err != nil || group == nil {
		return nil
	}
	if s, _ := document.ResolveName(ctx, doc, group["S"]); s != "Transparency" {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, group["CS"])
	if err != nil || len(arr) < 2 {
		return nil
	}
	if csType, _ := document.ResolveName(ctx, doc, arr[0]); csType != "ICCBased" {
		return nil
	}
	ref, _ := arr[1].(ast.PdfIndirectRef)
	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, arr[1])
	if !ok {
		return nil
	}
	return iccProfileData(ctx, doc, stream)
}

// iccProfileData returns the profile a stream carries, with its filters
// applied.
//
// [ast.PdfStream.Bytes] stops before filter decoding, so comparing its result
// compares how a profile happens to be stored rather than what it is: the same
// CMYK profile written at two zlib levels is the same profile, and the detector
// says so, but the fixer then found no duplicate to replace and left the
// document failing ISO 19005-4, 6.2.4.2 test 3 after remediation.
func iccProfileData(ctx context.Context, doc *document.Document, stream ast.PdfStream) []byte {
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("decoding ICC profile stream", "error", err)
		return nil
	}
	return data
}

// dedupICCInResources walks a page's resources and rewrites /CSx =
// [/ICCBased ref] entries whose stream matches one of profiles. The paired
// device colour space is chosen by the profile's component count.
func dedupICCInResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, profiles [][]byte, visited map[int]bool) int {
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
		// A default colour space exists to remap the device space of the same
		// name to a calibrated one (ISO 32000-1, 8.6.5.6), so rewriting
		// /DefaultRGB to /DeviceRGB defines it as the very space it redirects.
		// pdfjs issue20722.pdf lost every character of 77 pages that way, page
		// 1 of 223 entirely, and it undoes the //Default injection this same
		// remediation pass performs to cover device colour usage.
		if isDefaultColourSpaceKey(key) {
			continue
		}
		ref, isRef := entry.(ast.PdfIndirectRef)
		if isRef {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}
		// Read alloc-first. Colour-space objects are written earlier by
		// output-intent and output-intent-cleanup, and again by graphics,
		// transparency and group-cs, all ahead of icc-dedup in the registry.
		// Matching the on-disk profile instead would substitute a colour space
		// the document no longer uses, discarding the earlier write rather than
		// merely missing it. liveArray performs the ref/direct split itself.
		arr := liveArray(ctx, alloc, doc, entry)
		if len(arr) < 2 {
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
		if !matchesAnyProfile(ctx, doc, iccStream, profiles) {
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
func dedupICCInXObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, profiles profileSet, visited map[int]bool) int {
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
		// A form declaring its own transparency group blends in that space
		// instead of the page's, which is how the detector reads it.
		formProfiles := profiles
		if own := blendingProfile(ctx, alloc, doc, stream.Dict); own != nil {
			formProfiles.blending = own
		}
		formPE := annotPageEntry{ref: ref, dict: stream.Dict}
		count += dedupICCInResources(ctx, alloc, doc, formPE, formProfiles.all(), visited)
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

// isDefaultColourSpaceKey reports whether a /ColorSpace resource name is one of
// the default colour space entries of ISO 32000-1, 8.6.5.6. They redirect a
// device space rather than name a space content selects with cs or CS, so the
// device substitution this fixer performs is never valid for them.
func isDefaultColourSpaceKey(key ast.PdfName) bool {
	switch key {
	case "DefaultGray", "DefaultRGB", "DefaultCMYK":
		return true
	default:
		return false
	}
}
