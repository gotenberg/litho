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

	visited := make(map[int]bool)
	var total int
	for _, pe := range pageEntries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := patchPageDeviceN(ctx, alloc, doc, pe, visited)
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
func patchPageDeviceN(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, visited map[int]bool) int {
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
		arr, err := document.ResolveArray(ctx, doc, entry)
		if err != nil || len(arr) < 2 {
			continue
		}
		name, ok := arr[0].(ast.PdfName)
		if !ok || (name != "DeviceN" && name != "NChannel") {
			continue
		}
		newArr, changed := buildPatchedDeviceN(ctx, alloc, doc, arr)
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
func buildPatchedDeviceN(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, arr ast.PdfArray) (ast.PdfArray, bool) {
	namesArr, err := document.ResolveArray(ctx, doc, arr[1])
	if err != nil || len(namesArr) == 0 {
		return nil, false
	}
	// Mirror the DeviceN alternate space on the synthetic Separations:
	// PDF/A §6.2.4.4 test 2 requires Separations with the same name to
	// agree on alternate space, so reuse arr[2] rather than hardcoding
	// DeviceCMYK.
	alt := ast.PdfName("DeviceCMYK")
	altComponents := 4
	if len(arr) >= 3 {
		if n, ok := arr[2].(ast.PdfName); ok {
			alt = n
			switch n {
			case "DeviceGray":
				altComponents = 1
			case "DeviceRGB":
				altComponents = 3
			case "DeviceCMYK":
				altComponents = 4
			}
		}
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
		newColorants[name] = syntheticSeparation(alloc, name, alt, altComponents)
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

// syntheticSeparation builds a [/Separation name alt tintFunc] array
// whose tint function outputs a constant component vector sized to
// match alt's component count. The resulting space satisfies PDF/A-2/3/4
// §6.2.4.4 without requiring knowledge of the original spot ink's
// appearance, and mirrors the DeviceN alternate so test 2's equivalence
// check succeeds across Colorants entries in the same file.
func syntheticSeparation(alloc *writer.ObjAllocator, name, alt ast.PdfName, components int) ast.PdfArray {
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
