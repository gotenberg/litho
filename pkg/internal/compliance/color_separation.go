package compliance

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// deviceNComponentRule returns the rule for DeviceN component count limit.
func deviceNComponentRule(part int) Rule {
	// PDF/A-1 §6.1.12 t9; PDF/A-2/3 §6.1.13 t9. Part 4 reuses the part-1 id
	// (no distinct PDF/A-4 DeviceN-colourant rule).
	switch part {
	case 2:
		return Rule_19005_2_6_1_13_t09
	case 3:
		return Rule_19005_3_6_1_13_t09
	default:
		return Rule_19005_1_6_1_12_t09
	}
}

func separationConsistencyRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_4_4_t01, Rule_19005_3_6_2_4_4_t01, Rule_19005_4_6_2_4_4_t01)
}

// deviceNConsistencyRule returns the rule for DeviceN colour space consistency
// based on the PDF/A part.
func deviceNConsistencyRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_4_4_t02, Rule_19005_3_6_2_4_4_t02, Rule_19005_4_6_2_4_4_t02)
}

// checkSeparationDeviceN validates Separation and DeviceN colour space
// consistency per §6.2.4.4. It ensures that the same colorant name always maps
// to the same alternate colour space across the entire document.
func checkSeparationDeviceN(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, results *[]Violation) {
	// colorantAlternates tracks colorant name -> first alternate space seen.
	colorantAlternates := make(map[string]string)
	// colorantTintFuncs tracks colorant name -> first tint transform (resolved).
	colorantTintFuncs := make(map[string]ast.PdfObject)

	pages := collectPages(ctx, doc, catalog)
	for i, page := range pages {
		if ctx.Err() != nil {
			return
		}

		resources := resolveResources(ctx, doc, page)
		if resources == nil {
			continue
		}

		scanSeparationDeviceNInResources(ctx, doc, resources, part, i, colorantAlternates, colorantTintFuncs, results)

		// Also scan annotation appearance streams on this page.
		annotsArr, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annotsArr {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			apDict, _ := document.ResolveDict(ctx, doc, annot["AP"])
			if apDict == nil {
				continue
			}
			for _, apKey := range []ast.PdfName{"N", "R", "D"} {
				apStream, err := document.ResolveStream(ctx, doc, apDict[apKey])
				if err != nil {
					doc.Logger.Debug("compliance.color: cannot resolve appearance stream", "error", err)
					continue
				}
				if apStream.Dict == nil {
					continue
				}
				apRes, _ := document.ResolveDict(ctx, doc, apStream.Dict["Resources"])
				if apRes != nil {
					scanSeparationDeviceNInResources(ctx, doc, apRes, part, i, colorantAlternates, colorantTintFuncs, results)
				}
			}
		}
	}

	doc.Logger.Debug("compliance.color: Separation/DeviceN check complete",
		"uniqueColorants", len(colorantAlternates))
}

// scanSeparationDeviceNInResources scans a resources dictionary for Separation
// and DeviceN colour space arrays and validates colorant-to-alternate consistency.
func scanSeparationDeviceNInResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, colorantAlternates map[string]string, colorantTintFuncs map[string]ast.PdfObject, results *[]Violation) {
	visited := make(map[string]bool)
	scanSeparationDeviceNRecursive(ctx, doc, resources, part, pageIdx, colorantAlternates, colorantTintFuncs, results, 0, visited)
}

// scanSeparationDeviceNRecursive scans resources (and Form XObject sub-resources)
// for Separation and DeviceN colour space arrays.
func scanSeparationDeviceNRecursive(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, colorantAlternates map[string]string, colorantTintFuncs map[string]ast.PdfObject, results *[]Violation, depth int, visited map[string]bool) {
	if depth > maxFormXObjectDepth || ctx.Err() != nil {
		return
	}

	csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err == nil && csDict != nil {
		for name, csObj := range csDict {
			if ctx.Err() != nil {
				return
			}

			arr, err := document.ResolveArray(ctx, doc, csObj)
			if err != nil || len(arr) < 2 {
				continue
			}

			csType, err := document.ResolveName(ctx, doc, arr[0])
			if err != nil {
				doc.Logger.Debug("compliance.color: cannot resolve color space type name", "error", err)
				continue
			}

			switch string(csType) {
			case "Separation":
				checkSeparationCS(ctx, doc, arr, part, pageIdx, string(name), colorantAlternates, colorantTintFuncs, results)
			case "DeviceN":
				checkDeviceNCS(ctx, doc, arr, part, pageIdx, string(name), colorantAlternates, colorantTintFuncs, results)
			}
		}
	}

	// Recurse into Form XObject resources.
	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return
	}
	for _, xobjObj := range xobjDict {
		ref, ok := xobjObj.(ast.PdfIndirectRef)
		if ok {
			key := fmt.Sprintf("%d", ref.ObjectNum)
			if visited[key] {
				continue
			}
			visited[key] = true
		}
		stream, err := document.ResolveStream(ctx, doc, xobjObj)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve XObject stream", "error", err)
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		if subtype != "Form" {
			continue
		}
		subRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if subRes != nil {
			scanSeparationDeviceNRecursive(ctx, doc, subRes, part, pageIdx, colorantAlternates, colorantTintFuncs, results, depth+1, visited)
		}
	}
}

// checkSeparationCS validates a Separation colour space array:
// [/Separation /ColorantName /AlternateSpace /TintTransform]
func checkSeparationCS(ctx context.Context, doc *document.Document, arr ast.PdfArray, part int, pageIdx int, csName string, colorantAlternates map[string]string, colorantTintFuncs map[string]ast.PdfObject, results *[]Violation) {
	if len(arr) < 4 {
		return
	}

	colorantName, err := document.ResolveName(ctx, doc, arr[1])
	if err != nil {
		return
	}

	alternateName := resolveCSName(ctx, doc, arr[2])
	if alternateName == "" {
		return
	}

	cName := string(colorantName)
	rule := separationConsistencyRule(part)
	if rule.Specification == "" {
		// §6.2.4.4 exists for parts 2+ only; a zero Rule must never be
		// emitted (it serialized as a clause-less "-t00" residual).
		return
	}

	if prevAlt, ok := colorantAlternates[cName]; ok {
		// Check alternate space consistency.
		if prevAlt != alternateName {
			*results = append(*results, failf(rule,
				"page %d, ColorSpace %s: Separation colorant %q uses alternate %q but previously used %q",
				pageIdx+1, csName, cName, alternateName, prevAlt)...)
		}
		// Check tint transform consistency.
		if prevTint, hasPrev := colorantTintFuncs[cName]; hasPrev {
			if !tintTransformEqual(ctx, doc, prevTint, arr[3]) {
				*results = append(*results, failf(rule,
					"page %d, ColorSpace %s: Separation colorant %q has different tintTransform",
					pageIdx+1, csName, cName)...)
			}
		}
	} else {
		colorantAlternates[cName] = alternateName
		colorantTintFuncs[cName] = arr[3]
	}
}

// tintTransformEqual compares two tint transform function objects for
// equivalence. Per the spec, the comparison is structural (same dict keys/values
// and stream content), ignoring compression and indirect/direct status.
func tintTransformEqual(ctx context.Context, doc *document.Document, a, b ast.PdfObject) bool {
	// Try as streams (Type 4 PostScript calculator functions).
	sa, errA := document.ResolveStream(ctx, doc, a)
	sb, errB := document.ResolveStream(ctx, doc, b)
	if errA == nil && errB == nil {
		return functionStreamEqual(ctx, doc, sa, sb)
	}

	// Try as dicts (Type 0/2/3 functions).
	da, errA := document.ResolveDict(ctx, doc, a)
	db, errB := document.ResolveDict(ctx, doc, b)
	if errA == nil && errB == nil && da != nil && db != nil {
		return functionDictEqual(ctx, doc, da, db)
	}

	return false
}

// functionStreamEqual compares two function streams (Type 4 PostScript).
func functionStreamEqual(ctx context.Context, doc *document.Document, a, b ast.PdfStream) bool {
	// Compare decoded stream bytes.
	dataA, errA := doc.DecodeStream(ctx, a)
	dataB, errB := doc.DecodeStream(ctx, b)
	if errA != nil || errB != nil {
		return false
	}
	if !bytes.Equal(dataA, dataB) {
		return false
	}
	// Compare function dict keys (FunctionType, Domain, Range, etc.)
	// ignoring stream-specific keys (Length, Filter, DecodeParms).
	return functionDictEqual(ctx, doc, a.Dict, b.Dict)
}

// functionDictEqual compares two function dictionaries, ignoring
// stream-encoding keys (Length, Filter, DecodeParms, DL).
func functionDictEqual(ctx context.Context, doc *document.Document, a, b ast.PdfDict) bool {
	skip := map[ast.PdfName]bool{"Length": true, "Filter": true, "DecodeParms": true, "DL": true}
	keysA := 0
	for k, vA := range a {
		if skip[k] {
			continue
		}
		keysA++
		vB, ok := b[k]
		if !ok {
			return false
		}
		if !pdfObjectEqual(ctx, doc, vA, vB) {
			return false
		}
	}
	keysB := 0
	for k := range b {
		if !skip[k] {
			keysB++
		}
	}
	return keysA == keysB
}

// checkDeviceNCS validates a DeviceN colour space array:
// [/DeviceN [/names...] /AlternateSpace /TintTransform ...]
func checkDeviceNCS(ctx context.Context, doc *document.Document, arr ast.PdfArray, part int, pageIdx int, csName string, colorantAlternates map[string]string, colorantTintFuncs map[string]ast.PdfObject, results *[]Violation) {
	if len(arr) < 4 {
		return
	}

	names, err := document.ResolveArray(ctx, doc, arr[1])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve DeviceN names array", "page", pageIdx, "cs", csName, "error", err)
		return
	}

	// §6.1.12 t09: DeviceN component limit (PDF/A-1: 8, PDF/A-2+: 32).
	maxComponents := 32
	if part == 1 {
		maxComponents = 8
	}
	if len(names) > maxComponents {
		*results = append(*results, failf(deviceNComponentRule(part),
			"page %d, ColorSpace %s: DeviceN has %d components (max %d)",
			pageIdx+1, csName, len(names), maxComponents)...)
	}

	// Everything below mirrors §6.2.4.4, which exists for parts 2+ only;
	// ISO 19005-1 has no DeviceN consistency rules, and emitting with
	// the zero Rule serialized clause-less "-t00" residuals. The
	// component-limit check above is §6.1.12 t9 and applies to part 1.
	if deviceNConsistencyRule(part).Specification == "" {
		return
	}

	alternateName := resolveCSName(ctx, doc, arr[2])
	if alternateName == "" {
		return
	}

	// Collect colorant names from the names array.
	var colorantNames []string
	for _, nameObj := range names {
		if ctx.Err() != nil {
			return
		}

		colorantName, err := document.ResolveName(ctx, doc, nameObj)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve colorant name", "error", err)
			continue
		}

		cName := string(colorantName)
		colorantNames = append(colorantNames, cName)

		// DeviceN must not use reserved colorant names "All" or "None"
		// as tint transform targets (per §6.2.4.4).
		if cName == "All" || cName == "None" {
			*results = append(*results, failf(deviceNConsistencyRule(part),
				"page %d, ColorSpace %s: DeviceN uses reserved colorant name %q",
				pageIdx+1, csName, cName)...)
			continue
		}

		if prev, ok := colorantAlternates[cName]; ok {
			if prev != alternateName {
				*results = append(*results, failf(deviceNConsistencyRule(part),
					"page %d, ColorSpace %s: DeviceN colorant %q uses alternate %q but previously used %q",
					pageIdx+1, csName, cName, alternateName, prev)...)
			}
		} else {
			colorantAlternates[cName] = alternateName
		}
	}

	// §6.2.4.4 t1: For each spot colorant in DeviceN/NChannel, an entry in
	// the /Colorants dictionary (inside /Attributes) must be present.
	// Process colorants only: skip if all names are process colors.
	processColors := map[string]bool{
		"Cyan": true, "Magenta": true, "Yellow": true, "Black": true,
		"Red": true, "Green": true, "Blue": true,
	}

	var attrsDict ast.PdfDict
	if len(arr) >= 5 {
		attrsDict, _ = document.ResolveDict(ctx, doc, arr[4]) // Best-effort: nil dict skips colorant checks.
	}

	var colorantsDict ast.PdfDict
	if attrsDict != nil {
		colorantsDict, _ = document.ResolveDict(ctx, doc, attrsDict["Colorants"]) // Best-effort: nil dict skips colorant checks.
	}

	for _, cName := range colorantNames {
		if processColors[cName] || cName == "All" || cName == "None" {
			continue
		}
		// Spot colorant: must have a Colorants entry.
		if colorantsDict == nil {
			*results = append(*results, failf(separationConsistencyRule(part),
				"page %d, ColorSpace %s: DeviceN spot colorant %q has no Colorants dictionary",
				pageIdx+1, csName, cName)...)
			break // Report once per DeviceN CS, not per colorant.
		}
		if _, ok := colorantsDict[ast.PdfName(cName)]; !ok {
			*results = append(*results, failf(separationConsistencyRule(part),
				"page %d, ColorSpace %s: DeviceN spot colorant %q not found in Colorants dictionary",
				pageIdx+1, csName, cName)...)
		}
	}

	// §6.2.4.4 t2: Separation arrays in Colorants dictionaries must also be
	// consistent with all other Separation arrays with the same colorant name.
	for cName, cObj := range colorantsDict {
		cArr, err := document.ResolveArray(ctx, doc, cObj)
		if err != nil || len(cArr) < 4 {
			continue
		}
		cType, _ := document.ResolveName(ctx, doc, cArr[0])
		if cType != "Separation" {
			continue
		}
		cAlt := resolveCSName(ctx, doc, cArr[2])
		if cAlt == "" {
			continue
		}
		name := string(cName)
		if prev, ok := colorantAlternates[name]; ok {
			if prev != cAlt {
				*results = append(*results, failf(deviceNConsistencyRule(part),
					"page %d, Colorants dict: Separation %q uses alternate %q but previously used %q",
					pageIdx+1, name, cAlt, prev)...)
			}
			// Also check tint transform consistency.
			if prevTint, hasPrev := colorantTintFuncs[name]; hasPrev {
				if !tintTransformEqual(ctx, doc, prevTint, cArr[3]) {
					*results = append(*results, failf(deviceNConsistencyRule(part),
						"page %d, Colorants dict: Separation %q has different tintTransform",
						pageIdx+1, name)...)
				}
			} else {
				// First Colorants dict entry for this colorant. Store its tint
				// transform so subsequent entries with the same name are compared.
				colorantTintFuncs[name] = cArr[3]
			}
		} else {
			colorantAlternates[name] = cAlt
			colorantTintFuncs[name] = cArr[3]
		}
	}
}
