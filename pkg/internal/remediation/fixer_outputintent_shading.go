package remediation

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// shadingPatternType is the /PatternType of a shading pattern (ISO 32000-1,
// 8.7.4.1, Table 76).
const shadingPatternType = 2

// maxDefaultSpaceNesting bounds how deeply [replaceDeviceFamily] follows one
// colour space into the spaces it holds. Anti-DoS guard, not spec-mandated: it
// mirrors compliance's maxColorSpaceNesting, since ISO 32000-1, 8.6.6 nests at
// most an /Indexed base over a DeviceN whose /Colorants hold Separations over
// an alternate, four levels, so eight leaves every conformant space whole
// while a crafted chain of direct arrays stops.
const maxDefaultSpaceNesting = 8

// defaultSpaceFamily maps a Default colour space key to the device family it
// replaces (ISO 32000-1, 8.6.5.6).
var defaultSpaceFamily = map[ast.PdfName]ast.PdfName{
	"DefaultGray": "DeviceGray",
	"DefaultRGB":  "DeviceRGB",
	"DefaultCMYK": "DeviceCMYK",
}

// patchShadingPatternSpace gives the shading of a shading pattern the Default
// colour space key names in place of the device family it replaces, wherever
// the shading's colour space holds that family: as the space itself, as a
// Separation or DeviceN alternate, as a DeviceN /Colorants entry's alternate
// or process space, or as an /Indexed base. It returns the rewritten pattern
// when the shading is direct, and whether it changed anything.
//
// ISO 32000-1, 8.6.5.6 substitutes a Default space for a device family the
// content stream selects through its own resources, and a shading pattern has
// no resources of its own (8.7.4.1, Table 76), so veraPDF judges its shading's
// device family against the output intent alone (veraPDF-validation 1.30,
// ColorSpaceFactory.getPattern builds GFPDShadingPattern with no resources
// handler). The Default injected beside a new sRGB intent then leaves a
// DeviceCMYK shading failing ISO 19005-2, 6.2.4.3 t3 (ISO 19005-1, 6.2.3.3 t2
// at PDF/A-1), and the DeviceRGB one under a new CMYK intent 6.2.4.3 t2. The
// space this writes is the one the Default holds, so the shading's colour is
// characterised by the same profile as every other use of the family in the
// output. A Separation over the family that a /ColorSpace entry also names
// then keeps one alternate across the document as veraPDF reads them, the
// entry's through the Default it substitutes, so 6.2.4.4 t2 stays silent.
//
// # Corpus evidence
//
//   - litho patternshade_2b.pdf: remediation replaced its CMYK intent with an
//     sRGB one and kept a DeviceCMYK finding on veraPDF and litho at every
//     PDF/A level, in both modes, for the Separation of the shading pattern a
//     tiling cell selects. It is gone at all eleven, and the PDF/A-4 and 4e
//     outputs are conforming.
//   - litho shadedefault_2b.pdf: six DeviceCMYK findings, and from part 2 on
//     a 6.2.4.4 t2 one, stayed at every PDF/A level in both modes; every
//     output is now conforming on both tools but PDF/A-4f's, which keeps 6.9
//     t5.
//   - pdfjs issue19319.pdf: its DeviceN shading pattern takes the
//     /DefaultCMYK at PDF/A-1a and 1b, and no rule moves on litho or veraPDF.
func patchShadingPatternSpace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pattern ast.PdfDict, key ast.PdfName, csArr func() ast.PdfArray, visited map[int]bool) (ast.PdfDict, bool) {
	family, ok := defaultSpaceFamily[key]
	if !ok {
		return nil, false
	}
	if patternType, err := document.ResolveInt(ctx, doc, pattern["PatternType"]); err != nil || patternType != shadingPatternType {
		return nil, false
	}
	shObj, ok := pattern["Shading"]
	if !ok {
		return nil, false
	}
	ref, isRef := shObj.(ast.PdfIndirectRef)
	if !isRef {
		// A direct shading is a dictionary: a stream is always indirect (ISO
		// 32000-1, 7.3.8.1), so a mesh shading never sits here.
		sh, ok := shObj.(ast.PdfDict)
		if !ok {
			return nil, false
		}
		newSh, changed := replaceShadingSpace(ctx, alloc, doc, sh, family, csArr)
		if !changed {
			return nil, false
		}
		newPattern := make(ast.PdfDict, len(pattern))
		maps.Copy(newPattern, pattern)
		newPattern["Shading"] = newSh
		return newPattern, true
	}
	if ref.ObjectNum == 0 || visited[ref.ObjectNum] {
		return nil, false
	}
	visited[ref.ObjectNum] = true
	raw, pending := alloc.Pending()[ref.ObjectNum]
	if !pending {
		resolved, err := doc.Resolve(ctx, ref)
		if err != nil {
			return nil, false
		}
		raw = resolved
	}
	switch sh := raw.(type) {
	case ast.PdfDict:
		newSh, changed := replaceShadingSpace(ctx, alloc, doc, sh, family, csArr)
		if changed {
			alloc.AllocAt(ref.ObjectNum, newSh)
		}
		return nil, changed
	case ast.PdfStream:
		if sh.Dict == nil {
			return nil, false
		}
		newDict, changed := replaceShadingSpace(ctx, alloc, doc, sh.Dict, family, csArr)
		if !changed {
			return nil, false
		}
		body, err := sh.Bytes(ctx)
		if err != nil {
			// The mesh body is unreadable: rewriting the stream would empty it,
			// so the shading keeps its space and the rule keeps firing.
			doc.Logger.Debug("remediation: skipping mesh shading colour space rewrite, body read failed",
				slog.Int("obj", ref.ObjectNum), slog.Any("err", err))
			return nil, false
		}
		alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, body))
		return nil, true
	}
	return nil, false
}

// replaceShadingSpace returns a copy of a shading dictionary whose
// /ColorSpace holds csArr in place of family, and whether it changed.
func replaceShadingSpace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, sh ast.PdfDict, family ast.PdfName, csArr func() ast.PdfArray) (ast.PdfDict, bool) {
	cs, ok := sh["ColorSpace"]
	if !ok {
		return nil, false
	}
	newCS, changed := replaceDeviceFamily(ctx, alloc, doc, cs, family, csArr, 0)
	if !changed {
		return nil, false
	}
	newSh := make(ast.PdfDict, len(sh))
	maps.Copy(newSh, sh)
	newSh["ColorSpace"] = newCS
	return newSh, true
}

// replaceDeviceFamily returns cs with csArr in place of every family it
// holds as the space itself, a Separation or DeviceN alternate, a DeviceN
// /Colorants entry, a DeviceN process space or an /Indexed base (ISO 32000-1,
// 8.6.6), and whether it changed. A reference along the way is read, never
// written: the rewritten value is inlined, so a space another resource
// dictionary shares keeps the family its own Default replaces there.
func replaceDeviceFamily(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, cs ast.PdfObject, family ast.PdfName, csArr func() ast.PdfArray, depth int) (ast.PdfObject, bool) {
	if depth > maxDefaultSpaceNesting {
		return cs, false
	}
	switch v := cs.(type) {
	case ast.PdfName:
		if v == family {
			return csArr(), true
		}
		return cs, false
	case ast.PdfIndirectRef:
		resolved := liveObject(ctx, alloc, doc, v)
		switch resolved.(type) {
		case ast.PdfName, ast.PdfArray:
			if out, changed := replaceDeviceFamily(ctx, alloc, doc, resolved, family, csArr, depth+1); changed {
				return out, true
			}
		}
		return cs, false
	case ast.PdfArray:
		return replaceInSpaceArray(ctx, alloc, doc, v, family, csArr, depth)
	}
	return cs, false
}

// replaceInSpaceArray is the array case of [replaceDeviceFamily].
func replaceInSpaceArray(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, arr ast.PdfArray, family ast.PdfName, csArr func() ast.PdfArray, depth int) (ast.PdfObject, bool) {
	if len(arr) == 0 {
		return arr, false
	}
	head, _ := document.ResolveName(ctx, doc, arr[0])
	var slots []int
	switch head {
	case "Separation":
		// [/Separation name alternateSpace tintTransform], 8.6.6.4.
		slots = []int{2}
	case "DeviceN":
		// [/DeviceN names alternateSpace tintTransform attributes], 8.6.6.5.
		slots = []int{2}
	case "Indexed":
		// [/Indexed base hival lookup], 8.6.6.3.
		slots = []int{1}
	default:
		return arr, false
	}
	out := slices.Clone(arr)
	changed := false
	for _, i := range slots {
		if i >= len(out) {
			continue
		}
		if v, ok := replaceDeviceFamily(ctx, alloc, doc, out[i], family, csArr, depth+1); ok {
			out[i] = v
			changed = true
		}
	}
	if head == "DeviceN" && len(out) > 4 {
		if attrs, ok := replaceDeviceNAttributes(ctx, alloc, doc, out[4], family, csArr, depth+1); ok {
			out[4] = attrs
			changed = true
		}
	}
	if !changed {
		return arr, false
	}
	return out, true
}

// replaceDeviceNAttributes rewrites the /Colorants entries and the /Process
// /ColorSpace of a DeviceN attributes dictionary (ISO 32000-1, 8.6.6.5, Table
// 71), returning the inlined copy and whether it changed.
func replaceDeviceNAttributes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, attrsObj ast.PdfObject, family ast.PdfName, csArr func() ast.PdfArray, depth int) (ast.PdfObject, bool) {
	attrs := liveDictValue(ctx, alloc, doc, attrsObj)
	if attrs == nil {
		return attrsObj, false
	}
	newAttrs := make(ast.PdfDict, len(attrs))
	maps.Copy(newAttrs, attrs)
	changed := false
	if colorants := liveDictValue(ctx, alloc, doc, attrs["Colorants"]); colorants != nil {
		newColorants := make(ast.PdfDict, len(colorants))
		maps.Copy(newColorants, colorants)
		colorantsChanged := false
		for _, name := range slices.Sorted(maps.Keys(colorants)) {
			if v, ok := replaceDeviceFamily(ctx, alloc, doc, colorants[name], family, csArr, depth+1); ok {
				newColorants[name] = v
				colorantsChanged = true
			}
		}
		if colorantsChanged {
			newAttrs["Colorants"] = newColorants
			changed = true
		}
	}
	if process := liveDictValue(ctx, alloc, doc, attrs["Process"]); process != nil {
		if v, ok := replaceDeviceFamily(ctx, alloc, doc, process["ColorSpace"], family, csArr, depth+1); ok {
			newProcess := make(ast.PdfDict, len(process))
			maps.Copy(newProcess, process)
			newProcess["ColorSpace"] = v
			newAttrs["Process"] = newProcess
			changed = true
		}
	}
	if !changed {
		return attrsObj, false
	}
	return newAttrs, true
}

// liveObject resolves ref as the next fixer should see it: the replacement an
// earlier fixer queued at its object number, or the document's copy. It
// returns nil when neither reads.
func liveObject(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) ast.PdfObject {
	if obj, pending := alloc.Pending()[ref.ObjectNum]; pending {
		return obj
	}
	resolved, err := doc.Resolve(ctx, ref)
	if err != nil {
		return nil
	}
	return resolved
}
