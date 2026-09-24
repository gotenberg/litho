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
	// PDF/A-1 §6.1.12 t9; PDF/A-2/3 §6.1.13 t9. ISO 19005-4 states no
	// DeviceN colourant limit at all, so part 4 has no rule to report:
	// emitting the part-1 id there flagged a construct that part permits.
	switch part {
	case 2:
		return Rule_19005_2_6_1_13_t09
	case 3:
		return Rule_19005_3_6_1_13_t09
	case 4:
		return Rule{}
	default:
		return Rule_19005_1_6_1_12_t09
	}
}

// separationConsistencyRule selects §6.2.4.4 t2: all Separation arrays with the
// same colorant name share alternateSpace and tintTransform.
//
// # Corpus evidence
//
//   - All corpora: emitting t1 here was a 36-cell litho-only mislabel.
func separationConsistencyRule(part int) Rule {
	// §6.2.4.4 t2: all Separation arrays with the same colorant name shall
	// share alternateSpace and tintTransform. t1 is the separate
	// Colorants-dictionary presence rule, a distinct sub-test of the same
	// clause, so this rule must not emit t1.
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_4_4_t02, Rule_19005_3_6_2_4_4_t02, Rule_19005_4_6_2_4_4_t02)
}

// separationColorantsRule selects §6.2.4.4 t1: any spot colour used in a
// DeviceN or NChannel colour space needs an entry in the Colorants dict.
func separationColorantsRule(part int) Rule {
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
func checkSeparationDeviceN(ctx context.Context, doc *document.Document, pages []pageCtx, part int, results *[]Violation) {
	// colorantAlternates tracks colorant name -> first alternate space seen.
	colorantAlternates := make(map[string]string)
	// colorantTintFuncs tracks colorant name -> first tint transform (resolved).
	colorantTintFuncs := make(map[string]ast.PdfObject)

	for i := range pages {
		if ctx.Err() != nil {
			return
		}

		page := pages[i].dict
		resources := resolveInheritedResources(ctx, doc, page)
		if resources == nil {
			continue
		}

		// A pattern or a shading takes part only when the page paints with it,
		// and a colour space only when something selects it. The parsed
		// operations report both, and they are already cached on the page (see
		// pageCtx.parsedOps), so this costs no second parse.
		_, referencedXObjects, contentCSNames, used, inheritedAtDo := scanContentStreamDeviceCS(ctx, pages[i].parsedOps(ctx), collectPatternCSNames(ctx, doc, resources))

		scanSeparationDeviceNInResources(ctx, doc, resources, part, i, colorantAlternates, colorantTintFuncs, results, used, referencedXObjects, contentCSNames, inheritedAtDo)

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
					// An appearance stream no Do invokes begins in the default
					// colour-space state, and its own operations report both
					// what it paints and which colour spaces it selects.
					apUsed, apXObjects, apCSNames, apInherited := nestedScopeUsage(ctx, doc, apStream, apRes, inheritedCSDefault())
					scanSeparationDeviceNInResources(ctx, doc, apRes, part, i, colorantAlternates, colorantTintFuncs, results, apUsed, apXObjects, apCSNames, apInherited)
				}
			}
		}
	}

	doc.Logger.Debug("compliance.color: Separation/DeviceN check complete",
		"uniqueColorants", len(colorantAlternates))
}

// scanSeparationDeviceNInResources scans a resources dictionary for Separation
// and DeviceN colour space arrays and validates colorant-to-alternate consistency.
func scanSeparationDeviceNInResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, colorantAlternates map[string]string, colorantTintFuncs map[string]ast.PdfObject, results *[]Violation, used *usedResources, referencedXObjects, contentCSNames map[string]bool, inherited map[string]inheritedCS) {
	visited := make(map[string]bool)
	scanSeparationDeviceNRecursive(ctx, doc, resources, part, pageIdx, colorantAlternates, colorantTintFuncs, results, 0, visited, used, referencedXObjects, contentCSNames, inherited)
}

// scanSeparationDeviceNRecursive scans resources (and Form XObject sub-resources)
// for Separation and DeviceN colour space arrays. used names the patterns and
// shadings the surveyed content stream paints, contentCSNames the colour spaces
// it selects, and inherited the colour-space state in effect at each Do.
//
// A nested scope is surveyed from its own operators (see [nestedScopeUsage]),
// so the patterns and shadings it paints are reached. Its contentCSNames stays
// nil, which judges its colour spaces in full: this scan reaches more than it
// did, never less. A scope whose stream will not decode or parse yields no
// survey at all and is judged in full, as an unsurveyed one was.
func scanSeparationDeviceNRecursive(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, colorantAlternates map[string]string, colorantTintFuncs map[string]ast.PdfObject, results *[]Violation, depth int, visited map[string]bool, used *usedResources, referencedXObjects, contentCSNames map[string]bool, inherited map[string]inheritedCS) {
	if formTooDeep(doc, depth) || ctx.Err() != nil {
		return
	}

	csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err == nil && csDict != nil {
		// A colour space merely declared in /Resources/ColorSpace is not one the
		// file uses, and §6.2.4.4 constrains the Separation arrays a file
		// employs. veraPDF was measured to stay silent on a conflicting colorant
		// that no cs/CS selects, and to report it as soon as an operator selects
		// the space, whether or not anything is then painted with it.
		// collectReferencedCSNames is the scoping the device-colour scan
		// already applies, and it also counts a space named by a painted image
		// or shading.
		var referenced map[string]bool
		if contentCSNames != nil {
			referenced = collectReferencedCSNames(ctx, doc, resources, referencedXObjects, contentCSNames)
		}
		for _, name := range sortedNames(csDict) {
			if ctx.Err() != nil {
				return
			}
			if referenced != nil && !referenced[string(name)] {
				continue
			}
			checkSeparationDeviceNArray(ctx, doc, csDict[name], part, pageIdx, string(name), colorantAlternates, colorantTintFuncs, results)
		}
	}

	// A Separation reached through a painted tiling pattern's own resources, or
	// named as a painted shading's /ColorSpace, is still "within a single
	// PDF/A-2 file" (ISO 19005-2 §6.2.4.4), so it takes part in colorant
	// consistency. Scoping the walk to /Resources/ColorSpace and Form XObjects
	// left both unreachable while veraPDF reported them.
	scanSeparationInShadings(ctx, doc, resources, part, pageIdx, colorantAlternates, colorantTintFuncs, results, used)
	scanSeparationInPatterns(ctx, doc, resources, part, pageIdx, colorantAlternates, colorantTintFuncs, results, depth, visited, used)

	// Recurse into Form XObject resources.
	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return
	}
	for _, k := range sortedNames(xobjDict) {
		xobjObj := xobjDict[k]
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
		if subtype == "Image" {
			// An image XObject names its colour space inline rather than
			// through /Resources/ColorSpace, so §6.2.4.4 t1 has to reach it
			// here. veraPDF reports such a failure at
			// .../xObject[0](n 0 obj PDXImage)/imageCS[0](n 0 obj PDDeviceN).
			// Only t1 applies: the colorant-consistency scan (t2) keeps its
			// existing resource-dictionary scope.
			imgCS, err := document.ResolveArray(ctx, doc, stream.Dict["ColorSpace"])
			if err != nil || len(imgCS) < 2 {
				continue
			}
			where := fmt.Sprintf("Image XObject %s", k)
			// An /Indexed image names its DeviceN one level down; see
			// indexedBaseArray.
			if base := indexedBaseArray(ctx, doc, imgCS, 0); base != nil {
				imgCS, where = base, where+" /Indexed base"
			}
			if csType, err := document.ResolveName(ctx, doc, imgCS[0]); err == nil && csType == "DeviceN" {
				checkDeviceNColorants(ctx, doc, imgCS, part, pageIdx, where, results)
			}
			continue
		}
		if subtype != "Form" {
			continue
		}
		subRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if subRes != nil {
			// A form begins from the colour-space state in effect at its Do, so
			// a bare scn inside it can select a pattern the invoking stream
			// chose the space for. Its own operations also report which of its
			// colour spaces it selects, and ISO 19005-2 §6.2.4.4 constrains the
			// Separation arrays a file employs: one the form only declares
			// employs none, and veraPDF reports nothing for it.
			formUsed, formXObjects, formCSNames, formInherited := nestedScopeUsage(ctx, doc, stream, subRes, inheritedCSFor(inherited, string(k)))
			scanSeparationDeviceNRecursive(ctx, doc, subRes, part, pageIdx, colorantAlternates, colorantTintFuncs, results, depth+1, visited, formUsed, formXObjects, formCSNames, formInherited)
		}
	}
}

// ISO 32000-1 §8.7.3.1, Table 75: /PatternType 1 is a tiling pattern. Per
// §8.7.3.3 its /PaintType 2 is the uncoloured form, whose cell specifies no
// colour of its own.
const (
	tilingPatternType   = 1
	uncolouredPaintType = 2
)

// maxIndexedBaseDepth bounds how far [indexedBaseArray] follows a chain of
// /Indexed bases. Anti-DoS guard, not spec-mandated: ISO 32000-1, 8.6.6.3
// forbids an /Indexed base from being another /Indexed space, so one level is
// the only conformant shape and this merely stops a crafted chain.
const maxIndexedBaseDepth = 4

// indexedBaseArray returns the base colour space array of an /Indexed space,
// following a nested /Indexed base up to [maxIndexedBaseDepth]. Returns nil
// when arr does not name /Indexed, or its base is not an array.
//
// ISO 32000-1, 8.6.6.3 makes that base a colour space in its own right, so the
// constraints on the space apply to it. veraPDF models it the same way and
// anchors ISO 19005-2 §6.2.4.4 t1 at the base of the image's /Indexed space.
// Every caller used to test only the head of the outer array, so a colorant
// defect in the base went unreported.
//
// # Corpus evidence
//
//   - pdfjs issue9940.pdf: paints /Im0 and /Im1 in /Indexed over
//     /DeviceN(/IBM,/None,/None,/None) whose attributes declare no /Colorants
//     entry for IBM. veraPDF anchors §6.2.4.4 t1 at
//     imageCS[0](52 0 obj PDIndexed)/base[0](51 0 obj PDDeviceN), and litho
//     reported nothing.
func indexedBaseArray(ctx context.Context, doc *document.Document, arr ast.PdfArray, depth int) ast.PdfArray {
	if depth > maxIndexedBaseDepth || ctx.Err() != nil || len(arr) < 2 {
		return nil
	}
	csType, err := document.ResolveName(ctx, doc, arr[0])
	if err != nil || csType != "Indexed" {
		return nil
	}
	base, err := document.ResolveArray(ctx, doc, arr[1])
	if err != nil || len(base) < 2 {
		return nil
	}
	if baseType, berr := document.ResolveName(ctx, doc, base[0]); berr == nil && baseType == "Indexed" {
		return indexedBaseArray(ctx, doc, base, depth+1)
	}
	return base
}

// checkSeparationDeviceNArray dispatches one colour-space object to the
// Separation or DeviceN consistency check. where names the site the violation
// message reports.
func checkSeparationDeviceNArray(ctx context.Context, doc *document.Document, csObj ast.PdfObject, part int, pageIdx int, where string, colorantAlternates map[string]string, colorantTintFuncs map[string]ast.PdfObject, results *[]Violation) {
	arr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(arr) < 2 {
		return
	}

	csType, err := document.ResolveName(ctx, doc, arr[0])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve color space type name", "error", err)
		return
	}

	switch string(csType) {
	case "Separation":
		checkSeparationCS(ctx, doc, arr, part, pageIdx, where, colorantAlternates, colorantTintFuncs, results)
	case "DeviceN":
		checkDeviceNCS(ctx, doc, arr, part, pageIdx, where, colorantAlternates, colorantTintFuncs, results)
	case "Indexed":
		// Only §6.2.4.4 t1 follows the base. The colorant-consistency scan
		// (t2) keeps its resource-dictionary scope, exactly as the image
		// XObject site in scanSeparationDeviceNRecursive already states,
		// so an /Indexed base adds no new alternate-space comparisons.
		base := indexedBaseArray(ctx, doc, arr, 0)
		if base == nil {
			return
		}
		if baseType, berr := document.ResolveName(ctx, doc, base[0]); berr == nil && baseType == "DeviceN" {
			checkDeviceNColorants(ctx, doc, base, part, pageIdx, where+" /Indexed base", results)
		}
	}
}

// scanSeparationInShadings judges the colour space a painted shading names. A
// shading dictionary carries /ColorSpace directly (ISO 32000-1 §8.7.4.3), not
// through /Resources/ColorSpace, so the resource walk never reaches it.
//
// Only a shading an sh operator paints counts. veraPDF reports nothing for one
// that no operator paints, which is the usage-driven scope litho's
// device-colour scan already applies (see [usedResources]).
func scanSeparationInShadings(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, colorantAlternates map[string]string, colorantTintFuncs map[string]ast.PdfObject, results *[]Violation, used *usedResources) {
	if used == nil {
		return
	}

	shDict, err := document.ResolveDict(ctx, doc, resources["Shading"])
	if err != nil || shDict == nil {
		return
	}

	for _, name := range sortedNames(shDict) {
		if ctx.Err() != nil {
			return
		}
		if !used.shadings[string(name)] {
			continue
		}
		sd := resolveShadingDict(ctx, doc, shDict[name])
		if sd == nil {
			doc.Logger.Debug("compliance.color: cannot resolve Shading dict", "name", name)
			continue
		}
		checkSeparationDeviceNArray(ctx, doc, sd["ColorSpace"], part, pageIdx, fmt.Sprintf("Shading %s", name), colorantAlternates, colorantTintFuncs, results)
	}
}

// scanSeparationInPatterns judges the Separation and DeviceN spaces a painted
// pattern reaches: a tiling pattern through its own /Resources, a shading
// pattern through its /Shading dictionary (ISO 32000-1 §8.7.3).
//
// Only a pattern a scn/SCN operator selects counts, and an uncoloured tiling
// pattern is skipped: its cell specifies no colour at all (ISO 32000-1
// §8.7.3.3). Measured against veraPDF on one file at both paint types: it
// reports §6.2.4.4 t2 for the colorant inside a /PaintType 1 cell and nothing
// at all for the same cell at /PaintType 2.
func scanSeparationInPatterns(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, colorantAlternates map[string]string, colorantTintFuncs map[string]ast.PdfObject, results *[]Violation, depth int, visited map[string]bool, used *usedResources) {
	if used == nil {
		return
	}

	patDict, err := document.ResolveDict(ctx, doc, resources["Pattern"])
	if err != nil || patDict == nil {
		return
	}

	for _, name := range sortedNames(patDict) {
		if ctx.Err() != nil {
			return
		}
		if !used.patterns[string(name)] {
			continue
		}

		patternObj := patDict[name]
		if ref, ok := patternObj.(ast.PdfIndirectRef); ok {
			key := fmt.Sprintf("pattern %d", ref.ObjectNum)
			if visited[key] {
				continue
			}
			visited[key] = true
		}

		stream, err := document.ResolveStream(ctx, doc, patternObj)
		if err != nil {
			// A shading pattern is a dictionary rather than a stream, and names
			// its colour space on the /Shading it paints.
			patternDict, err := document.ResolveDict(ctx, doc, patternObj)
			if err != nil || patternDict == nil {
				continue
			}
			if sd := resolveShadingDict(ctx, doc, patternDict["Shading"]); sd != nil {
				checkSeparationDeviceNArray(ctx, doc, sd["ColorSpace"], part, pageIdx, fmt.Sprintf("Pattern %s shading", name), colorantAlternates, colorantTintFuncs, results)
			}
			continue
		}

		patternType, _ := document.ResolveInt(ctx, doc, stream.Dict["PatternType"])
		if patternType != tilingPatternType {
			continue
		}
		paintType, _ := document.ResolveInt(ctx, doc, stream.Dict["PaintType"])
		if paintType == uncolouredPaintType {
			continue
		}

		subRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if subRes == nil {
			continue
		}
		// A tiling pattern cell no Do invokes begins in the default
		// colour-space state, and its own operations report both what it paints
		// and which colour spaces it selects.
		cellUsed, cellXObjects, cellCSNames, cellInherited := nestedScopeUsage(ctx, doc, stream, subRes, inheritedCSDefault())
		scanSeparationDeviceNRecursive(ctx, doc, subRes, part, pageIdx, colorantAlternates, colorantTintFuncs, results, depth+1, visited, cellUsed, cellXObjects, cellCSNames, cellInherited)
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

		// §6.2.4.4 constrains only colorant alternate/tintTransform
		// consistency (t2) and Colorants-dict presence (t1); it imposes no
		// constraint on the colorant identifier itself. The reserved names
		// "None" and "All" are valid DeviceN colorants, so litho does not
		// flag them here.

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

	var attrsDict ast.PdfDict
	if len(arr) >= 5 {
		attrsDict, _ = document.ResolveDict(ctx, doc, arr[4]) // Best-effort: nil dict skips colorant checks.
	}

	var colorantsDict ast.PdfDict
	if attrsDict != nil {
		colorantsDict, _ = document.ResolveDict(ctx, doc, attrsDict["Colorants"]) // Best-effort: nil dict skips colorant checks.
	}

	checkDeviceNColorants(ctx, doc, arr, part, pageIdx, fmt.Sprintf("ColorSpace %s", csName), results)

	// §6.2.4.4 t2: Separation arrays in Colorants dictionaries must also be
	// consistent with all other Separation arrays with the same colorant name.
	for _, cName := range sortedNames(colorantsDict) {
		cObj := colorantsDict[cName]
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

// deviceNProcessColorants are the colorant names a DeviceN array may carry
// without a /Colorants entry because they name process colorants rather than
// spot colours. ISO 32000-1 §8.6.6.4 defines the DeviceCMYK process colorant
// names as Cyan, Magenta, Yellow and Black; §8.6.6.5 lets an NChannel
// /Process /Components array name further process colorants, collected
// separately in [deviceNAllowedColorants]. veraPDF's PDDeviceN model exempts
// this exact four-name set, so the additive names Red, Green and Blue are spot
// colours here and do require a /Colorants entry.
//
//nolint:gochecknoglobals // immutable lookup set.
var deviceNProcessColorants = map[string]bool{
	"Cyan":    true,
	"Magenta": true,
	"Yellow":  true,
	"Black":   true,
}

// checkDeviceNColorants validates §6.2.4.4 t1 for one DeviceN colour space
// array: every spot colorant it names needs an entry in the /Colorants
// dictionary of its attributes. where identifies the colour space in the
// violation context.
//
// # Corpus evidence
//
//   - verapdf veraPDF_test_suite_6-2-4-4-t02-fail-a: veraPDF's §6.2.4.4 t1
//     summary carries seven failed checks, one per offending colorant, so
//     reporting every undeclared colorant matches it.
func checkDeviceNColorants(ctx context.Context, doc *document.Document, arr ast.PdfArray, part, pageIdx int, where string, results *[]Violation) {
	// §6.2.4.4 t1 exists for parts 2+ only; a zero Rule must never be
	// emitted (it serializes as a clause-less "-t00" residual).
	rule := separationColorantsRule(part)
	if rule.Specification == "" || len(arr) < 2 {
		return
	}

	names, err := document.ResolveArray(ctx, doc, arr[1])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve DeviceN names array", "context", where, "error", err)
		return
	}

	allowed := deviceNAllowedColorants(ctx, doc, arr)
	for _, nameObj := range names {
		if ctx.Err() != nil {
			return
		}
		colorantName, err := document.ResolveName(ctx, doc, nameObj)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve colorant name", "context", where, "error", err)
			continue
		}
		cName := string(colorantName)
		// ISO 32000-1 §8.6.6.5: None marks a component that never paints, so
		// it names no ink and needs no Colorants entry.
		if cName == "None" || deviceNProcessColorants[cName] || allowed[cName] {
			continue
		}
		// Report every undeclared colorant, not just the first: veraPDF raises
		// one failed check per offending colorant, so stopping here would
		// under-report a file's actual colorant defects.
		*results = append(*results, failf(rule,
			"page %d, %s: DeviceN spot colorant %q has no Colorants entry",
			pageIdx+1, where, cName)...)
	}
}

// deviceNAllowedColorants returns the colorant names a DeviceN array declares
// through its attributes dictionary: the keys of /Colorants plus the process
// colorants named in /Process /Components (ISO 32000-1 §8.6.6.5, Tables 72-73).
func deviceNAllowedColorants(ctx context.Context, doc *document.Document, arr ast.PdfArray) map[string]bool {
	allowed := make(map[string]bool)
	if len(arr) < 5 {
		return allowed
	}
	attrs, err := document.ResolveDict(ctx, doc, arr[4])
	if err != nil || attrs == nil {
		return allowed
	}

	if colorants, err := document.ResolveDict(ctx, doc, attrs["Colorants"]); err == nil {
		for name := range colorants {
			allowed[string(name)] = true
		}
	}

	process, err := document.ResolveDict(ctx, doc, attrs["Process"])
	if err != nil || process == nil {
		return allowed
	}
	components, err := document.ResolveArray(ctx, doc, process["Components"])
	if err != nil {
		return allowed
	}
	for _, compObj := range components {
		name, err := document.ResolveName(ctx, doc, compObj)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve DeviceN process component", "error", err)
			continue
		}
		allowed[string(name)] = true
	}
	return allowed
}
