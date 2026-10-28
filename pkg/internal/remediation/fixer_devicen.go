package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/intconv"
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

// fixDeviceNColorants completes the /Colorants of every DeviceN/NChannel
// colour space compliance judges under ISO 19005-2/3, 6.2.4.4 t1 and ISO
// 19005-4, 6.2.4.4 t1, wherever it stands ([deviceNWalk]): in the /ColorSpace
// resources of a page and of the forms, tiling patterns, Type 3 fonts and
// annotation appearances below it, as a shading's colour space, as an image
// XObject's and as the base of an /Indexed space. Each missing colorant gets
// the document's own Separation of that name, or a minimal /Separation backed
// by a degenerate Type 2 function. That keeps the document compliant without
// altering visible output beyond what the existing tint transform already
// produced.
//
// Completing /Colorants adds an entry to the space's attributes dictionary
// and leaves its components as they are (ISO 32000-1, 8.6.6.5), so an image's
// samples and an /Indexed lookup table read the same, and the walk rewrites
// the spaces they are laid out for too.
//
// # Corpus evidence
//
//   - All corpora: inventing a degenerate transform for a name already
//     defined elsewhere traded test 1 for test 2 on four corpus files.
//   - pdfjs issue17065.pdf: the DeviceN of the shading pattern its page
//     inherits kept 6.2.4.4 t1 after a PDF/A-2b or PDF/A-4 remediation, on
//     litho and veraPDF alike.
//   - govdocs thread_300_300242.pdf, thread_400_400015.pdf,
//     thread_400_400575.pdf and thread_400_400932.pdf: shadings use a DeviceN
//     short of colorants, which a PDF/A-2b remediation already completed, and
//     their outputs do not change. thread_400_400932.pdf's shadings name the
//     indirect DeviceN its page's /ColorSpace resources hold.
//   - pdfjs colorspace_atan.pdf, colorspace_cos.pdf and colorspace_sin.pdf:
//     an image's DeviceN kept 6.2.4.4 t1 after a PDF/A-2b remediation.
//   - pdfjs issue9940.pdf: the DeviceN base of an /Indexed space kept 6.2.4.4
//     t1 after a PDF/A-2b remediation.
//   - litho offlimits_1b.pdf: the DeviceN of an image an uncoloured tiling
//     pattern's cell paints, directly on page 1 and through a form on page 3,
//     kept 6.2.4.4 t1 after a PDF/A-2b remediation, on litho and veraPDF
//     alike.
func fixDeviceNColorants(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	// A colorant this fixer synthesises must agree with the definition the
	// document already uses for that ink: ISO 19005-2, 6.2.4.4 test 2 requires
	// every Separation of the same name to share an alternate space and a tint
	// transform. Inventing a degenerate transform for a name already defined
	// elsewhere trades test 1 for test 2. The first walk records every
	// Separation the second one can reach, in the order it reaches them.
	canon := make(map[ast.PdfName]ast.PdfArray)
	collect := &deviceNWalk{
		alloc:   alloc,
		doc:     doc,
		visited: make(map[int]bool),
		samples: true,
		space: func(ctx context.Context, entry ast.PdfObject) (ast.PdfArray, bool) {
			recordSeparations(ctx, alloc, doc, liveArray(ctx, alloc, doc, entry), canon)
			return nil, false
		},
	}
	if err := collect.walkPages(ctx); err != nil {
		return nil, err
	}

	complete := &deviceNWalk{
		alloc:   alloc,
		doc:     doc,
		visited: make(map[int]bool),
		samples: true,
		space: func(ctx context.Context, entry ast.PdfObject) (ast.PdfArray, bool) {
			arr := liveArray(ctx, alloc, doc, entry)
			if len(arr) < 2 {
				return nil, false
			}
			if name, _ := arr[0].(ast.PdfName); name != "DeviceN" && name != "NChannel" {
				return nil, false
			}
			return buildPatchedDeviceN(ctx, alloc, doc, arr, canon)
		},
	}
	if err := complete.walkPages(ctx); err != nil {
		return nil, err
	}
	if complete.count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("populated /Colorants for %d DeviceN colour space(s)", complete.count)}, nil
}

// recordSeparations records into canon the Separation the colour space arr
// is, or each Separation the /Colorants of the DeviceN it is holds, when canon
// has none of that name yet.
func recordSeparations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, arr ast.PdfArray, canon map[ast.PdfName]ast.PdfArray) {
	record := func(arr ast.PdfArray) {
		if name, sep, ok := extractSeparation(arr); ok {
			if _, seen := canon[name]; !seen {
				canon[name] = sep
			}
		}
	}
	record(arr)
	if len(arr) < 5 {
		return
	}
	if name, _ := arr[0].(ast.PdfName); name != "DeviceN" && name != "NChannel" {
		return
	}
	attrs := liveDictValue(ctx, alloc, doc, arr[4])
	colorants := liveDictValue(ctx, alloc, doc, attrs["Colorants"])
	for _, name := range sortedDictKeys(colorants) {
		record(liveArray(ctx, alloc, doc, colorants[name]))
	}
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
		return intconv.Clamp(n)
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
