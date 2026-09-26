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

// matchDeviceNColorants matches PDF/A-2/3/4 §6.2.4.4 violations about
// DeviceN/NChannel spot colorants missing from the Colorants attribute
// dict.
func matchDeviceNColorants(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.4.4") {
		return false
	}
	return contextContains(v, "Colorants dictionary") || descriptionContains(v, "Colorants dictionary")
}

// fixDeviceNColorants walks the /Resources/ColorSpace entries of every page,
// inherited ones included, and the shadings its /Shading and /Pattern
// resources name ([patchPageShadingDeviceN]), looking for DeviceN/NChannel
// colour spaces whose /Attributes/Colorants dict is missing entries for one or
// more spot colorants. Each missing colorant gets a minimal /Separation colour
// space backed by a degenerate Type 2 function. That keeps the document
// compliant without altering visible output beyond what the existing tint
// transform already produced.
//
// # Corpus evidence
//
//   - All corpora: inventing a degenerate transform for a name already
//     defined elsewhere traded test 1 for test 2 on four corpus files.
func fixDeviceNColorants(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
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

	// A colorant this fixer synthesises must agree with the definition the
	// document already uses for that ink: ISO 19005-2, 6.2.4.4 test 2 requires
	// every Separation of the same name to share an alternate space and a tint
	// transform. Inventing a degenerate transform for a name already defined
	// elsewhere trades test 1 for test 2.
	canon := make(map[ast.PdfName]ast.PdfArray)
	for _, pe := range pageEntries {
		collectSeparations(ctx, alloc, doc, pe, canon)
		collectShadingSeparations(ctx, alloc, doc, pe, canon)
	}

	visited := make(map[int]bool)
	var total int
	for _, pe := range pageEntries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		total += patchPageDeviceN(ctx, alloc, doc, pe, canon, visited)
		total += patchPageShadingDeviceN(ctx, alloc, doc, pe, canon, visited)
	}
	if total == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("populated /Colorants for %d DeviceN colour space(s)", total)}, nil
}

// patchPageDeviceN walks the /ColorSpace entries of the /Resources a page
// resolves its names against, its own or the ones it inherits (see
// [resourcesHolder]), and rewrites any DeviceN/NChannel array that is missing
// /Colorants. The page or ancestor holding the /Resources is re-allocated with
// updated Resources when the fix has to ripple through inline containers.
func patchPageDeviceN(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, canon map[ast.PdfName]ast.PdfArray, visited map[int]bool) int {
	holder, ok := resourcesHolder(ctx, alloc, doc, pe)
	if !ok {
		return 0
	}
	resObj := holder.dict["Resources"]
	// Both containers are read alloc-first because an inline repair copies them
	// and writes the copy back: output-intent rewrites an indirect /Resources
	// with the /ColorSpace dictionary inlined into it, and name-utf8 rewrites
	// any indirect dictionary, both ahead of devicen. Reading the document
	// would discard that write, or follow a /ColorSpace reference the pending
	// /Resources no longer holds and patch an object the output never reads.
	resDict := liveDictValue(ctx, alloc, doc, resObj)
	if resDict == nil {
		return 0
	}
	csObj, ok := resDict["ColorSpace"]
	if !ok {
		return 0
	}
	csDict := liveDictValue(ctx, alloc, doc, csObj)
	if csDict == nil {
		return 0
	}

	count := 0
	inlineCS := make(map[ast.PdfName]ast.PdfArray)
	// Sorted iteration so object allocation (and the output bytes) is
	// deterministic across runs.
	for _, key := range sortedDictKeys(csDict) {
		entry := csDict[key]
		ref, isRef := entry.(ast.PdfIndirectRef)
		if isRef {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}
		// Read alloc-first. Colour-space objects are written earlier by
		// output-intent and output-intent-cleanup, and again by graphics,
		// transparency and group-cs, every one of them ahead of devicen in the
		// registry, so rebuilding from the on-disk array below would discard
		// those writes when this one lands. liveArray performs the ref/direct
		// split itself and yields nil when the value is unresolvable.
		arr := liveArray(ctx, alloc, doc, entry)
		if len(arr) < 2 {
			continue
		}
		name, ok := arr[0].(ast.PdfName)
		if !ok || (name != "DeviceN" && name != "NChannel") {
			continue
		}
		newArr, changed := buildPatchedDeviceN(ctx, alloc, doc, arr, canon)
		if !changed {
			continue
		}
		if isRef && ref.ObjectNum > 0 {
			alloc.AllocAt(ref.ObjectNum, newArr)
			count++
			continue
		}
		inlineCS[key] = newArr
		count++
	}
	if len(inlineCS) == 0 {
		return count
	}

	newCSDict := make(ast.PdfDict, len(csDict))
	maps.Copy(newCSDict, csDict)
	for k, v := range inlineCS {
		newCSDict[k] = v
	}
	if csRef, ok := csObj.(ast.PdfIndirectRef); ok && csRef.ObjectNum > 0 {
		alloc.AllocAt(csRef.ObjectNum, newCSDict)
		return count
	}

	newRes := make(ast.PdfDict, len(resDict))
	maps.Copy(newRes, resDict)
	newRes["ColorSpace"] = newCSDict
	if resRef, ok := resObj.(ast.PdfIndirectRef); ok && resRef.ObjectNum > 0 {
		alloc.AllocAt(resRef.ObjectNum, newRes)
		return count
	}

	newHolder := make(ast.PdfDict, len(holder.dict))
	maps.Copy(newHolder, holder.dict)
	newHolder["Resources"] = newRes
	if holder.ref.ObjectNum > 0 {
		alloc.AllocAt(holder.ref.ObjectNum, newHolder)
	}
	return count
}

// buildPatchedDeviceN returns a new DeviceN array with a populated
// /Attributes/Colorants sub-dict. Returns (nil, false) when nothing
// needed to change. The caller decides whether to alloc in place or
// splice the new array into a parent dict.
func buildPatchedDeviceN(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, arr ast.PdfArray, canon map[ast.PdfName]ast.PdfArray) (ast.PdfArray, bool) {
	namesArr, err := document.ResolveArray(ctx, doc, arr[1])
	if err != nil || len(namesArr) == 0 {
		return nil, false
	}
	// Mirror the DeviceN alternate space on the synthetic Separations:
	// PDF/A §6.2.4.4 test 2 requires Separations with the same name to
	// agree on alternate space, so reuse arr[2] verbatim rather than
	// hardcoding DeviceCMYK. Substituting a different space made the
	// remediated file fail test 2 whenever the alternate was anything but
	// a bare device name, an ICCBased alternate being the common case.
	var alt ast.PdfObject = ast.PdfName("DeviceCMYK")
	altComponents := 4
	if len(arr) >= 3 && arr[2] != nil {
		alt = arr[2]
		altComponents = colorSpaceComponents(ctx, doc, arr[2])
	}
	var attrsDict ast.PdfDict
	if len(arr) >= 5 {
		if d, err := document.ResolveDict(ctx, doc, arr[4]); err == nil {
			attrsDict = d
		}
	}
	var colorants ast.PdfDict
	if attrsDict != nil {
		if d, err := document.ResolveDict(ctx, doc, attrsDict["Colorants"]); err == nil {
			colorants = d
		}
	}
	if colorants == nil {
		colorants = ast.PdfDict{}
	}

	newColorants := make(ast.PdfDict, len(colorants)+len(namesArr))
	maps.Copy(newColorants, colorants)
	added := false
	for _, n := range namesArr {
		name, ok := n.(ast.PdfName)
		if !ok {
			continue
		}
		// Process colorants and tint-transform slots are PDF-reserved
		// keywords, not spot inks, so skip them.
		if name == "None" || name == "All" || name == "Cyan" || name == "Magenta" ||
			name == "Yellow" || name == "Black" {
			continue
		}
		if _, exists := newColorants[name]; exists {
			continue
		}
		// Reuse the document's own definition when it has one, so the
		// colorant entry cannot disagree with the Separation the content
		// already paints with.
		if existing, defined := canon[name]; defined {
			newColorants[name] = existing
		} else {
			newColorants[name] = syntheticSeparation(alloc, name, alt, altComponents)
		}
		added = true
	}
	if !added {
		return nil, false
	}

	newAttrs := ast.PdfDict{}
	if attrsDict != nil {
		maps.Copy(newAttrs, attrsDict)
	}
	newAttrs["Colorants"] = newColorants

	newArr := make(ast.PdfArray, 0, 5)
	newArr = append(newArr, arr[0], arr[1])
	if len(arr) >= 3 {
		newArr = append(newArr, arr[2])
	}
	if len(arr) >= 4 {
		newArr = append(newArr, arr[3])
	}
	for len(newArr) < 4 {
		newArr = append(newArr, ast.PdfNull{})
	}
	newArr = append(newArr, newAttrs)
	return newArr, true
}

// colorSpaceComponents returns the number of colour components an alternate
// colour space takes, so a synthetic tint transform can be sized to it. Per
// ISO 32000-1 §8.6.5, DeviceGray and CalGray take 1, DeviceRGB, CalRGB and Lab
// take 3, DeviceCMYK takes 4, and an ICCBased stream declares its own count in
// /N. Unrecognised spaces fall back to 4, matching the previous behaviour: an
// oversized tint transform still renders, and PDF/A only constrains the
// Separation's alternate-space equivalence.
func colorSpaceComponents(ctx context.Context, doc *document.Document, csObj ast.PdfObject) int {
	const defaultComponents = 4

	if name, err := document.ResolveName(ctx, doc, csObj); err == nil && name != "" {
		switch name {
		case "DeviceGray":
			return 1
		case "DeviceRGB":
			return 3
		case "DeviceCMYK":
			return defaultComponents
		}
	}

	arr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(arr) == 0 {
		return defaultComponents
	}
	family, err := document.ResolveName(ctx, doc, arr[0])
	if err != nil {
		doc.Logger.Debug("remediation.devicen: cannot resolve alternate colour space family", "error", err)
		return defaultComponents
	}
	switch family {
	case "CalGray":
		return 1
	case "CalRGB", "Lab":
		return 3
	case "ICCBased":
		if len(arr) < 2 {
			return defaultComponents
		}
		stream, err := document.ResolveStream(ctx, doc, arr[1])
		if err != nil {
			doc.Logger.Debug("remediation.devicen: cannot resolve ICCBased stream", "error", err)
			return defaultComponents
		}
		n, err := document.ResolveInt(ctx, doc, stream.Dict["N"])
		if err != nil || n < 1 || n > defaultComponents {
			return defaultComponents
		}
		return int(n)
	}
	return defaultComponents
}

// syntheticSeparation builds a [/Separation name alt tintFunc] array
// whose tint function outputs a constant component vector sized to
// match alt's component count. The resulting space satisfies PDF/A-2/3/4
// §6.2.4.4 without requiring knowledge of the original spot ink's
// appearance, and mirrors the DeviceN alternate so test 2's equivalence
// check succeeds across Colorants entries in the same file.
func syntheticSeparation(alloc *writer.ObjAllocator, name ast.PdfName, alt ast.PdfObject, components int) ast.PdfArray {
	zeros := make(ast.PdfArray, components)
	maxVals := make(ast.PdfArray, components)
	rangeArr := make(ast.PdfArray, 0, components*2)
	for i := range components {
		zeros[i] = ast.PdfReal(0)
		maxVals[i] = ast.PdfReal(1)
		rangeArr = append(rangeArr, ast.PdfReal(0), ast.PdfReal(1))
	}
	tintFunc := ast.PdfDict{
		"FunctionType": ast.PdfInt(2),
		"Domain":       ast.PdfArray{ast.PdfReal(0), ast.PdfReal(1)},
		"Range":        rangeArr,
		"N":            ast.PdfReal(1),
		"C0":           zeros,
		"C1":           maxVals,
	}
	funcRef := alloc.Alloc(tintFunc)
	return ast.PdfArray{
		ast.PdfName("Separation"),
		name,
		alt,
		funcRef,
	}
}

// shadingPatternType is the /PatternType of a shading pattern (ISO 32000-1,
// 8.7.4.1, Table 76), a dictionary whose /Shading paints the pattern.
const shadingPatternType = 2

// patchPageShadingDeviceN completes the DeviceN colour spaces the shadings of
// the page's resources use, the ones [patchPageDeviceN] does not reach: a
// shading its /Shading resources name for sh, and the shading of a shading
// pattern its /Pattern resources name for scn (ISO 32000-1, 8.7.4.3 and
// 8.7.4.5). ISO 19005-2, 6.2.4.4 t1 holds every DeviceN a page paints with to
// the /Colorants rule, and compliance judges a shading's as it judges any
// other. It returns how many it completed.
//
// The resources are the ones the page resolves its names against, its own or
// the ones it inherits ([resourcesHolder]), read after patchPageDeviceN has
// written them. A colour space, shading or pattern with an object number of
// its own is written there; a direct one at the first link above it that has
// one, up to the page or ancestor holding the /Resources.
//
// # Corpus evidence
//
//   - pdfjs issue17065.pdf: the DeviceN of the shading pattern its page
//     inherits kept 6.2.4.4 t1 after a PDF/A-2b or PDF/A-4 remediation, on
//     litho and veraPDF alike.
//   - govdocs thread_300_300242.pdf, thread_400_400015.pdf,
//     thread_400_400575.pdf and thread_400_400932.pdf: shadings use a DeviceN
//     short of colorants, which a PDF/A-2b remediation already completed, and
//     their outputs do not change. thread_400_400932.pdf's shadings name the
//     indirect DeviceN its page's /ColorSpace resources hold.
func patchPageShadingDeviceN(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, canon map[ast.PdfName]ast.PdfArray, visited map[int]bool) int {
	if live := resolveFromAllocOrDoc(ctx, alloc, doc, pe.ref); live != nil {
		pe.dict = live
	}
	holder, ok := resourcesHolder(ctx, alloc, doc, pe)
	if !ok {
		return 0
	}
	w := &shadingDeviceNWalk{alloc: alloc, doc: doc, canon: canon, visited: visited}
	work := maps.Clone(holder.dict)
	if pending, dirty := w.resources(ctx, work); dirty && alloc.AllocAt(holder.ref.ObjectNum, work) {
		w.completed += pending
	}
	return w.completed
}

// collectShadingSeparations records into canon the Separations the shadings
// [patchPageShadingDeviceN] completes use, their colour space or a /Colorants
// entry of their DeviceN, as [collectSeparations] records the page's /ColorSpace
// resources' ahead of it. A colorant the repair gives a shading's DeviceN must
// agree with the Separation of the same name another shading paints with (ISO
// 19005-2, 6.2.4.4 t2) as much as with one the page's resources hold.
func collectShadingSeparations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, canon map[ast.PdfName]ast.PdfArray) {
	holder, ok := resourcesHolder(ctx, alloc, doc, pe)
	if !ok {
		return
	}
	res := liveDictValue(ctx, alloc, doc, holder.dict["Resources"])
	var shadings []ast.PdfObject
	named := liveDictValue(ctx, alloc, doc, res["Shading"])
	for _, name := range sortedDictKeys(named) {
		shadings = append(shadings, named[name])
	}
	patterns := liveDictValue(ctx, alloc, doc, res["Pattern"])
	for _, name := range sortedDictKeys(patterns) {
		pattern := liveDictValue(ctx, alloc, doc, patterns[name])
		if patternType, _ := document.ResolveInt(ctx, doc, pattern["PatternType"]); patternType == shadingPatternType {
			shadings = append(shadings, pattern["Shading"])
		}
	}
	record := func(arr ast.PdfArray) {
		if name, sep, ok := extractSeparation(arr); ok {
			if _, seen := canon[name]; !seen {
				canon[name] = sep
			}
		}
	}
	for _, obj := range shadings {
		dict := liveDictValue(ctx, alloc, doc, obj)
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
			if s, isStream := liveStream(ctx, alloc, doc, ref); isStream {
				dict = s.Dict
			}
		}
		cs := liveArray(ctx, alloc, doc, dict["ColorSpace"])
		record(cs)
		if len(cs) < 5 {
			continue
		}
		attrs := liveDictValue(ctx, alloc, doc, cs[4])
		colorants := liveDictValue(ctx, alloc, doc, attrs["Colorants"])
		for _, name := range sortedDictKeys(colorants) {
			record(liveArray(ctx, alloc, doc, colorants[name]))
		}
	}
}

// shadingDeviceNWalk carries one [patchPageShadingDeviceN] run. Each method
// returns the value to put in place of a direct object it rebuilt, with how
// many colour spaces it completed in it, which land with the caller's write;
// it writes an object with a number of its own itself.
type shadingDeviceNWalk struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	canon   map[ast.PdfName]ast.PdfArray
	visited map[int]bool
	// completed counts the colour spaces completed by writes that landed.
	completed int
}

// resources completes the shadings the /Shading and /Pattern resources that
// owner, the working copy of the page or ancestor holding them, holds under
// /Resources. It returns how many completions land with the caller's write of
// owner, which it updated when dirty is set.
func (w *shadingDeviceNWalk) resources(ctx context.Context, owner ast.PdfDict) (int, bool) {
	pending, dirty := 0, false
	for _, category := range []ast.PdfName{"Shading", "Pattern"} {
		// Read again for each category: the first may have rebuilt the
		// /Resources the second writes through.
		res := liveDictValue(ctx, w.alloc, w.doc, owner["Resources"])
		entries := liveDictValue(ctx, w.alloc, w.doc, res[category])
		var inline ast.PdfDict
		n := 0
		for _, name := range sortedDictKeys(entries) {
			if ctx.Err() != nil {
				return pending, dirty
			}
			var value ast.PdfObject
			var p int
			if category == "Shading" {
				value, p = w.shading(ctx, entries[name])
			} else {
				value, p = w.pattern(ctx, entries[name])
			}
			if p == 0 {
				continue
			}
			if inline == nil {
				inline = make(ast.PdfDict)
			}
			inline[name] = value
			n += p
		}
		if len(inline) == 0 {
			continue
		}
		rebuilt := maps.Clone(entries)
		maps.Copy(rebuilt, inline)
		written, ownerDirty := writeResourceCategory(w.alloc, owner, res, category, rebuilt)
		if written {
			w.completed += n
		}
		if ownerDirty {
			pending += n
			dirty = true
		}
	}
	return pending, dirty
}

// pattern completes the DeviceN the shading of the shading pattern obj uses. A
// tiling pattern paints with the colour spaces of its own /Resources, which
// this walk does not enter.
func (w *shadingDeviceNWalk) pattern(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, int) {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef && w.visited[ref.ObjectNum] {
		return obj, 0
	}
	// A tiling pattern is a stream, which liveDictValue does not read.
	dict := liveDictValue(ctx, w.alloc, w.doc, obj)
	if dict == nil {
		return obj, 0
	}
	if patternType, _ := document.ResolveInt(ctx, w.doc, dict["PatternType"]); patternType != shadingPatternType {
		return obj, 0
	}
	if isRef {
		w.visited[ref.ObjectNum] = true
	}
	shading, p := w.shading(ctx, dict["Shading"])
	if p == 0 {
		return obj, 0
	}
	rebuilt := maps.Clone(dict)
	rebuilt["Shading"] = shading
	if !isRef {
		return rebuilt, p
	}
	if w.alloc.AllocAt(ref.ObjectNum, rebuilt) {
		w.completed += p
	}
	return obj, 0
}

// shading completes the DeviceN the shading obj, a dictionary or a stream for
// the types 4 to 7 (ISO 32000-1, 8.7.4.5.1), uses. A stream is written back
// with its data.
func (w *shadingDeviceNWalk) shading(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, int) {
	ref, isRef := obj.(ast.PdfIndirectRef)
	var dict ast.PdfDict
	var stream ast.PdfStream
	isStream := false
	if isRef {
		if w.visited[ref.ObjectNum] {
			return obj, 0
		}
		w.visited[ref.ObjectNum] = true
		if s, ok := liveStream(ctx, w.alloc, w.doc, ref); ok {
			stream, dict, isStream = s, s.Dict, true
		}
	}
	if dict == nil {
		dict = liveDictValue(ctx, w.alloc, w.doc, obj)
	}
	if dict == nil {
		return obj, 0
	}
	cs, p := w.colourSpace(ctx, dict["ColorSpace"])
	if p == 0 {
		return obj, 0
	}
	rebuilt := maps.Clone(dict)
	rebuilt["ColorSpace"] = cs
	if !isRef {
		return rebuilt, p
	}
	var replacement ast.PdfObject = rebuilt
	if isStream {
		replacement = ast.NewLazyStream(rebuilt, stream.Bytes)
	}
	if w.alloc.AllocAt(ref.ObjectNum, replacement) {
		w.completed += p
	}
	return obj, 0
}

// colourSpace completes the DeviceN colour space obj, when it is one that
// needs it ([buildPatchedDeviceN]).
func (w *shadingDeviceNWalk) colourSpace(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, int) {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef && w.visited[ref.ObjectNum] {
		return obj, 0
	}
	arr := liveArray(ctx, w.alloc, w.doc, obj)
	if len(arr) < 2 {
		return obj, 0
	}
	if name, _ := arr[0].(ast.PdfName); name != "DeviceN" && name != "NChannel" {
		return obj, 0
	}
	if isRef {
		w.visited[ref.ObjectNum] = true
	}
	patched, changed := buildPatchedDeviceN(ctx, w.alloc, w.doc, arr, w.canon)
	if !changed {
		return obj, 0
	}
	if !isRef {
		return patched, 1
	}
	if w.alloc.AllocAt(ref.ObjectNum, patched) {
		w.completed++
	}
	return obj, 0
}
