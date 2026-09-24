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

// fixDeviceNColorants walks every page's /Resources/ColorSpace entries,
// looking for DeviceN/NChannel colour spaces whose /Attributes/Colorants
// dict is missing entries for one or more spot colorants. Each missing
// colorant gets a minimal /Separation colour space backed by a
// degenerate Type 2 function. That keeps the document compliant without
// altering visible output beyond what the existing tint transform
// already produced.
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
	}

	visited := make(map[int]bool)
	var total int
	for _, pe := range pageEntries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := patchPageDeviceN(ctx, alloc, doc, pe, canon, visited)
		total += n
	}
	if total == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("populated /Colorants for %d DeviceN colour space(s)", total)}, nil
}

// patchPageDeviceN walks a single page's /Resources/ColorSpace entries
// and rewrites any DeviceN/NChannel array that is missing /Colorants.
// The page dict is re-allocated with updated Resources when the fix has
// to ripple through inline containers.
func patchPageDeviceN(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, canon map[ast.PdfName]ast.PdfArray, visited map[int]bool) int {
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

	newPage := make(ast.PdfDict, len(pe.dict))
	maps.Copy(newPage, pe.dict)
	newPage["Resources"] = newRes
	if pe.ref.ObjectNum > 0 {
		alloc.AllocAt(pe.ref.ObjectNum, newPage)
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
