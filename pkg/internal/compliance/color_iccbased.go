package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func iccBasedProfileRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_2_t01, Rule_19005_2_6_2_4_2_t01, Rule_19005_3_6_2_4_2_t01, Rule_19005_4_6_2_4_2_t01)
}

// iccBasedNRule returns the rule that measures /N against the profile's own
// colour space.
func iccBasedNRule(part int) Rule {
	// Only ISO 19005-1, 6.2.3.2 test 2 states this requirement. Parts 2 to 4
	// have no equivalent: their 6.2.4.2 test 1 judges whether the profile
	// conforms to the ICC specifications, which iccBasedProfileRule already
	// answers for, and their remaining 6.2.4.2 tests judge CMYK overprint and
	// OutputIntent identity. Reporting a component-count mismatch under test 1
	// there labelled it with a requirement it does not break.
	return ruleByPart(part, Rule_19005_1_6_2_3_2_t02, Rule{}, Rule{}, Rule{})
}

// iccBasedIdentityRule returns the rule for ICCBased profiles identical to
// the OutputIntent.
func iccBasedIdentityRule(part int) Rule {
	// The "ICCBased profile identical to the OutputIntent" prohibition exists
	// ONLY in PDF/A-4 (6.2.4.2 t3). Parts 1-3 have no such rule (their 6.2.4.2
	// is just ICCInputProfile t1 + overprint t2), so firing there mislabelled
	// the overprint rule and was a false positive on otherwise-compliant files.
	return ruleByPart(part, Rule{}, Rule{}, Rule{}, Rule_19005_4_6_2_4_2_t03)
}

// checkICCBasedProfiles validates ICCBased colour space profiles per
// §6.2.3.2 (part 1) or §6.2.4.2 (parts 2-4).
// checkICCBasedProfiles takes the already-resolved pages so it can reuse their
// parsed operations; the page dictionaries are the same ones [collectPages]
// yields, in the same order.
func checkICCBasedProfiles(ctx context.Context, doc *document.Document, catalog ast.PdfDict, pages []pageCtx, part int, results *[]Violation) {
	// Collect the OutputIntent ICC profile info for identity comparison (parts 2+).
	var outputIntentRef ast.PdfIndirectRef
	var outputIntentHdr iccProfileHeader
	var outputIntentData []byte
	if part >= 2 {
		outputIntentRef, outputIntentHdr, outputIntentData = resolveOutputIntentICCInfo(ctx, doc, catalog)
	}

	for i := range pages {
		page := pages[i].dict
		if ctx.Err() != nil {
			return
		}

		resources := resolveInheritedResources(ctx, doc, page)
		if resources == nil {
			continue
		}

		// For page-level OutputIntents (PDF/A-4 allows them), use the page's
		// OutputIntent ICC info if the catalog-level one is absent.
		pageOIRef, pageOIHdr, pageOIData := outputIntentRef, outputIntentHdr, outputIntentData
		if part >= 2 && len(outputIntentData) == 0 {
			pageOIRef, pageOIHdr, pageOIData = resolveOutputIntentICCInfo(ctx, doc, page)
		}

		// Get page blending CS ICC ref for identity comparison.
		blendingCSRef := resolvePageBlendingICCRef(ctx, doc, page)

		// Collect CS names actually used by cs/CS operators in the content
		// stream. Parse failure yields nil ops; checks run on whatever is
		// available.
		usedCSNames := collectUsedCSNames(pages[i].parsedOps(ctx))

		// A pattern or a shading takes part only when the page paints with it,
		// which the same parsed operations report.
		_, referencedXObjects, _, used, inheritedAtDo := scanContentStreamDeviceCS(ctx, pages[i].parsedOps(ctx), collectPatternCSNames(ctx, doc, resources))

		checkICCBasedInResources(ctx, doc, resources, part, i, pageOIRef, pageOIHdr, pageOIData, blendingCSRef, usedCSNames, results)

		// Recursively scan Form XObject and annotation sub-resources.
		visited := make(map[string]bool)
		scanICCBasedRecursive(ctx, doc, resources, part, i, pageOIRef, pageOIHdr, pageOIData, blendingCSRef, results, 0, visited, used, referencedXObjects, inheritedAtDo)

		// Scan annotation appearance streams.
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
			for _, key := range []ast.PdfName{"N", "R", "D"} {
				apObj, ok := apDict[key]
				if !ok {
					continue
				}
				stream, err := document.ResolveStream(ctx, doc, apObj)
				if err != nil {
					doc.Logger.Debug("compliance.color: cannot resolve appearance stream", "error", err)
					continue
				}
				subRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
				if subRes != nil {
					formBlendRef := resolveFormBlendingRef(ctx, doc, stream, blendingCSRef)
					// An appearance stream no Do invokes begins in the default
					// colour-space state, so its own operations report which
					// patterns and shadings it paints and which colour spaces
					// it selects.
					apUsed, apXObjects, apCSNames, apInherited := nestedScopeUsage(ctx, doc, stream, subRes, inheritedCSDefault())
					checkICCBasedInResources(ctx, doc, subRes, part, i, pageOIRef, pageOIHdr, pageOIData, formBlendRef, apCSNames, results)
					scanICCBasedRecursive(ctx, doc, subRes, part, i, pageOIRef, pageOIHdr, pageOIData, formBlendRef, results, 1, visited, apUsed, apXObjects, apInherited)
				}
			}
		}
	}
}

// nestedScopeUsage surveys one nested content stream's own operators, so the
// patterns and shadings that scope paints are judged rather than skipped.
//
// entry is the colour-space state the scope inherits (ISO 32000-1 §8.10.2): a
// Form XObject begins from the state in effect at the Do that invoked it, while
// a tiling pattern cell and an annotation appearance stream begin from the
// default state (see [inheritedCSDefault]).
//
// A stream that will not decode or parse yields no survey. Returning nil leaves
// such a scope judged exactly as an unsurveyed one was, rather than widening a
// check on bytes nothing could read.
func nestedScopeUsage(ctx context.Context, doc *document.Document, stream ast.PdfStream, resources ast.PdfDict, entry inheritedCS) (*usedResources, map[string]bool, map[string]bool, map[string]inheritedCS) {
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot decode nested content stream", "error", err)
		return nil, nil, nil, nil
	}

	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot parse nested content stream", "error", err)
		return nil, nil, nil, nil
	}

	_, referencedXObjects, contentCSNames, used, inherited := scanContentStreamDeviceCSFrom(ctx, ops, collectPatternCSNames(ctx, doc, resources), entry)

	return used, referencedXObjects, contentCSNames, inherited
}

// scanICCBasedRecursive scans Form XObject sub-resources recursively for
// ICCBased colour spaces.
func scanICCBasedRecursive(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation, depth int, visited map[string]bool, used *usedResources, referencedXObjects map[string]bool, inherited map[string]inheritedCS) {
	if depth > maxFormXObjectDepth || ctx.Err() != nil {
		return
	}

	// An ICCBased colour space reached through a painted pattern's own
	// resources, or named as a painted shading's /ColorSpace, is used by the
	// file and so falls under the same clause. These run before the /XObject
	// lookup below, which returns early on a page that has no XObject at all.
	scanICCBasedInShadings(ctx, doc, resources, part, pageIdx, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results, used)
	scanICCBasedInPatterns(ctx, doc, resources, part, pageIdx, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results, depth, visited, used)

	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return
	}

	for _, k := range sortedNames(xobjDict) {
		xobjObj := xobjDict[k]
		if ctx.Err() != nil {
			return
		}
		// Only an XObject a Do operator invokes is used. One merely declared in
		// /Resources/XObject paints nothing, so its colour space is not a use,
		// which is the scoping [scanXObjectColorSpaces] already applies.
		// Measured: veraPDF reports nothing for a truncated ICC profile on an
		// image no operator invokes, and reports it as soon as a Do does.
		// referencedXObjects is nil for a scope whose operations were not
		// surveyed, which judges that scope in full rather than filtering it
		// against a survey that does not exist.
		if referencedXObjects != nil && !referencedXObjects[string(k)] {
			continue
		}
		// Cycle detection.
		if ref, ok := xobjObj.(ast.PdfIndirectRef); ok {
			key := fmt.Sprintf("icc-%d", ref.ObjectNum)
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
		st, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		// Image XObjects carry an inline /ColorSpace [/ICCBased ref] that the
		// named-resource scan in checkICCBasedInResources never reaches, so
		// validate their ICC profile version here.
		if st == "Image" {
			checkImageICCBasedVersion(ctx, doc, stream, part, pageIdx, k, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
			continue
		}
		if st != "Form" {
			continue
		}
		subRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if subRes == nil {
			continue
		}
		formBlendRef := resolveFormBlendingRef(ctx, doc, stream, blendingCSRef)
		// A form begins from the colour-space state in effect at its Do, so a
		// bare scn inside it can select a pattern the invoking stream chose the
		// space for. Its own operations also report which of its colour spaces
		// it selects: one it merely declares is employed by nothing, and
		// veraPDF reports nothing for it.
		formUsed, formXObjects, formCSNames, formInherited := nestedScopeUsage(ctx, doc, stream, subRes, inheritedCSFor(inherited, string(k)))
		checkICCBasedInResources(ctx, doc, subRes, part, pageIdx, outputIntentRef, outputIntentHdr, outputIntentData, formBlendRef, formCSNames, results)
		scanICCBasedRecursive(ctx, doc, subRes, part, pageIdx, outputIntentRef, outputIntentHdr, outputIntentData, formBlendRef, results, depth+1, visited, formUsed, formXObjects, formInherited)
	}
}

// reportUnreadableICCN reports /N against a profile whose own component count
// cannot be read.
//
// ISO 19005-1 §6.2.3.2 test 2 requires /N to match the number of components in
// the embedded profile. A profile that will not decode, or whose header is
// truncated before the data colour space at byte 16, states no count at all, so
// the requirement stands unmet rather than unmeasured. Only the profile stream
// being unresolvable leaves this silent, because there is then no profile to
// judge (see [checkICCBasedArray]).
func reportUnreadableICCN(nRule Rule, nVal int, pageIdx int, where string, results *[]Violation) {
	if nRule.Specification == "" {
		return
	}
	*results = append(*results, failf(nRule,
		"page %d, %s: /N=%d cannot be matched against an unreadable embedded profile", pageIdx+1, where, nVal)...)
}

// checkICCBasedArray validates one [/ICCBased stream] colour space array.
// where names the site the violation message reports, so a caller can say
// "ColorSpace CS0", "image Im0" or "Shading Sh0" while sharing one validation.
//
// The rules are part-dependent: /N against the profile's own colour space is
// stated only by ISO 19005-1 (see [iccBasedNRule]), and the OutputIntent
// identity prohibition only by ISO 19005-4 (see [iccBasedIdentityRule]).
func checkICCBasedArray(ctx context.Context, doc *document.Document, arr ast.PdfArray, part int, pageIdx int, where string, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation) {
	if len(arr) < 2 {
		return
	}

	stream, err := document.ResolveStream(ctx, doc, arr[1])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve ICCBased stream", "page", pageIdx, "where", where, "error", err)
		*results = append(*results, failf(iccBasedProfileRule(part),
			"page %d, %s: cannot resolve ICCBased profile stream", pageIdx+1, where)...)
		return
	}

	nRule := iccBasedNRule(part)
	nVal, err := document.ResolveInt(ctx, doc, stream.Dict["N"])
	if err != nil {
		if nRule.Specification != "" {
			*results = append(*results, failf(nRule,
				"page %d, %s: ICCBased profile missing /N entry", pageIdx+1, where)...)
		}
		return
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot decode ICCBased stream", "page", pageIdx, "where", where, "error", err)
		*results = append(*results, failf(iccBasedProfileRule(part),
			"page %d, %s: cannot decode ICCBased profile", pageIdx+1, where)...)
		reportUnreadableICCN(nRule, int(nVal), pageIdx, where, results)
		return
	}

	hdr, ok := parseICCHeader(data)
	if !ok {
		*results = append(*results, failf(iccBasedProfileRule(part),
			"page %d, %s: ICC profile header too short", pageIdx+1, where)...)
		reportUnreadableICCN(nRule, int(nVal), pageIdx, where, results)
		return
	}

	if problem := iccInputHeaderProblem(hdr, part); problem != "" {
		*results = append(*results, failf(iccBasedProfileRule(part),
			"page %d, %s: %s", pageIdx+1, where, problem)...)
	}

	expectedN := expectedComponentCount(hdr.colorSpace)
	if nRule.Specification != "" && (expectedN == 0 || int(nVal) != expectedN) {
		*results = append(*results, failf(nRule,
			"page %d, %s: /N=%d does not match ICC colour space %q", pageIdx+1, where, nVal, hdr.colorSpace)...)
	}

	if part >= 2 && hdr.colorSpace == "CMYK" {
		checkICCBasedIdentity(ctx, doc, arr[1], data, part, pageIdx, where, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
	}
}

// checkICCBasedIdentity reports a CMYK ICCBased profile identical to the
// OutputIntent profile or to the transparency blending colour space. Only
// ISO 19005-4 §6.2.4.2 t3 prohibits it; parts 1 to 3 carry no equivalent
// clause, so firing there mislabelled the overprint rule and was a false
// positive on otherwise-compliant files.
func checkICCBasedIdentity(ctx context.Context, doc *document.Document, profileObj ast.PdfObject, data []byte, part int, pageIdx int, where string, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation) {
	iccRef, _ := profileObj.(ast.PdfIndirectRef)
	identical := false

	if len(outputIntentData) > 0 && outputIntentHdr.colorSpace == "CMYK" {
		if iccRef.ObjectNum > 0 &&
			outputIntentRef.ObjectNum > 0 &&
			iccRef.ObjectNum == outputIntentRef.ObjectNum &&
			iccRef.GenNum == outputIntentRef.GenNum {
			identical = true
		}
		if !identical && ICCProfilesEqual(data, outputIntentData) {
			identical = true
		}
	}

	if !identical && blendingCSRef.ObjectNum > 0 {
		if iccRef.ObjectNum > 0 &&
			iccRef.ObjectNum == blendingCSRef.ObjectNum &&
			iccRef.GenNum == blendingCSRef.GenNum {
			identical = true
		}
		if !identical {
			blendStream, err := document.ResolveStream(ctx, doc, blendingCSRef)
			if err == nil {
				blendData, err := doc.DecodeStream(ctx, blendStream)
				if err == nil && ICCProfilesEqual(data, blendData) {
					identical = true
				}
			}
		}
	}

	if identical && part == 4 {
		*results = append(*results, failf(iccBasedIdentityRule(part),
			"page %d, %s: ICCBased profile is identical to OutputIntent", pageIdx+1, where)...)
	}
}

// scanICCBasedInShadings validates the ICCBased colour space a painted shading
// names. A shading dictionary carries /ColorSpace directly (ISO 32000-1
// §8.7.4.3) rather than through /Resources/ColorSpace, so the resource scan
// never reaches it.
//
// Only a shading an sh operator paints counts. Measured against veraPDF: it
// reports the clause for a broken profile on a painted shading and nothing at
// all when no operator paints that shading.
func scanICCBasedInShadings(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation, used *usedResources) {
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
			continue
		}
		arr, err := document.ResolveArray(ctx, doc, sd["ColorSpace"])
		if err != nil || len(arr) < 2 {
			continue
		}
		if t, _ := document.ResolveName(ctx, doc, arr[0]); t != "ICCBased" {
			continue
		}
		checkICCBasedArray(ctx, doc, arr, part, pageIdx, fmt.Sprintf("Shading %s", name), outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
	}
}

// scanICCBasedInPatterns validates the ICCBased colour spaces a painted pattern
// reaches: a tiling pattern through its own /Resources, a shading pattern
// through its /Shading dictionary (ISO 32000-1 §8.7.3).
//
// Only a pattern a scn/SCN operator selects counts, and an uncoloured tiling
// pattern is skipped: its cell specifies no colour of its own (ISO 32000-1
// §8.7.3.3). Both conditions were measured against veraPDF, which reports
// nothing for an unselected pattern and nothing for an uncoloured one.
func scanICCBasedInPatterns(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation, depth int, visited map[string]bool, used *usedResources) {
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
			key := fmt.Sprintf("icc-pattern-%d", ref.ObjectNum)
			if visited[key] {
				continue
			}
			visited[key] = true
		}

		stream, err := document.ResolveStream(ctx, doc, patternObj)
		if err != nil {
			// A shading pattern is a dictionary, and names its colour space on
			// the /Shading it paints.
			patternDict, err := document.ResolveDict(ctx, doc, patternObj)
			if err != nil || patternDict == nil {
				continue
			}
			sd := resolveShadingDict(ctx, doc, patternDict["Shading"])
			if sd == nil {
				continue
			}
			arr, arrErr := document.ResolveArray(ctx, doc, sd["ColorSpace"])
			if arrErr != nil || len(arr) < 2 {
				continue
			}
			if t, _ := document.ResolveName(ctx, doc, arr[0]); t == "ICCBased" {
				checkICCBasedArray(ctx, doc, arr, part, pageIdx, fmt.Sprintf("Pattern %s shading", name), outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
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
		// colour-space state, so its own operations report which patterns and
		// shadings it paints and which colour spaces it selects.
		cellUsed, cellXObjects, cellCSNames, cellInherited := nestedScopeUsage(ctx, doc, stream, subRes, inheritedCSDefault())
		checkICCBasedInResources(ctx, doc, subRes, part, pageIdx, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, cellCSNames, results)
		scanICCBasedRecursive(ctx, doc, subRes, part, pageIdx, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results, depth+1, visited, cellUsed, cellXObjects, cellInherited)
	}
}

// checkImageICCBasedVersion validates an image XObject's ICCBased /ColorSpace.
// Image colour spaces are inline ([/ICCBased ref]) rather than named resources,
// so the resource scan in [checkICCBasedInResources] never reaches them.
//
// It reports the same defects as any other ICCBased colour space. Reporting
// only a header problem left an image carrying an unresolvable, undecodable or
// truncated profile unjudged, while veraPDF reports it.
func checkImageICCBasedVersion(ctx context.Context, doc *document.Document, stream ast.PdfStream, part, pageIdx int, name ast.PdfName, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation) {
	arr, err := document.ResolveArray(ctx, doc, stream.Dict["ColorSpace"])
	if err != nil || len(arr) < 2 {
		return
	}
	if t, _ := document.ResolveName(ctx, doc, arr[0]); t != "ICCBased" {
		return
	}
	checkICCBasedArray(ctx, doc, arr, part, pageIdx, fmt.Sprintf("image %s", name), outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
}

// resolveFormBlendingRef extracts the ICCBased blending CS ref from a Form
// XObject's Group CS, falling back to the parent's blending ref.
func resolveFormBlendingRef(ctx context.Context, doc *document.Document, stream ast.PdfStream, parentRef ast.PdfIndirectRef) ast.PdfIndirectRef {
	grp, _ := document.ResolveDict(ctx, doc, stream.Dict["Group"])
	if grp == nil {
		return parentRef
	}
	csObj, ok := grp["CS"]
	if !ok {
		return parentRef
	}
	csArr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(csArr) < 2 {
		return parentRef
	}
	csT, _ := document.ResolveName(ctx, doc, csArr[0])
	if csT != "ICCBased" {
		return parentRef
	}
	if ref, ok := csArr[1].(ast.PdfIndirectRef); ok {
		return ref
	}
	return parentRef
}

// checkICCBasedInResources scans a page's resources for ICCBased colour spaces
// and validates them.
func checkICCBasedInResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, usedCSNames map[string]bool, results *[]Violation) {
	csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err != nil || csDict == nil {
		return
	}

	for _, name := range sortedNames(csDict) {
		csObj := csDict[name]
		if ctx.Err() != nil {
			return
		}

		// Skip CS names not referenced by cs/CS operators in the content stream.
		// A CS declared in Resources but never used is not a violation.
		if usedCSNames != nil && !usedCSNames[string(name)] {
			continue
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

		// For Separation/DeviceN, check the alternate colour space for ICCBased.
		if csType == "Separation" && len(arr) >= 3 {
			altArr, altErr := document.ResolveArray(ctx, doc, arr[2])
			if altErr == nil && len(altArr) >= 2 {
				altType, _ := document.ResolveName(ctx, doc, altArr[0])
				if altType == "ICCBased" {
					checkSingleICCBased(ctx, doc, altArr[1], part, pageIdx, name, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
				}
			}
			continue
		}
		if csType == "DeviceN" && len(arr) >= 4 {
			altArr, altErr := document.ResolveArray(ctx, doc, arr[2])
			if altErr == nil && len(altArr) >= 2 {
				altType, _ := document.ResolveName(ctx, doc, altArr[0])
				if altType == "ICCBased" {
					checkSingleICCBased(ctx, doc, altArr[1], part, pageIdx, name, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
				}
			}
			// A DeviceN /Attributes/Process/ColorSpace (arr[4]) may itself be an
			// ICCBased CMYK space identical to the OutputIntent (ISO 19005-4
			// §6.2.4.2 t3); the alternate scan above never reaches it.
			if len(arr) >= 5 {
				if attrs, _ := document.ResolveDict(ctx, doc, arr[4]); attrs != nil {
					if proc, _ := document.ResolveDict(ctx, doc, attrs["Process"]); proc != nil {
						if procArr, _ := document.ResolveArray(ctx, doc, proc["ColorSpace"]); len(procArr) >= 2 {
							if pt, _ := document.ResolveName(ctx, doc, procArr[0]); pt == "ICCBased" {
								checkSingleICCBased(ctx, doc, procArr[1], part, pageIdx, name, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
							}
						}
					}
				}
			}
			continue
		}

		if csType != "ICCBased" {
			continue
		}

		checkICCBasedArray(ctx, doc, arr, part, pageIdx, fmt.Sprintf("ColorSpace %s", name), outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
	}
}

// checkSingleICCBased validates a single ICCBased profile stream for the CMYK
// identity restriction (§6.2.4.2 t3). Used for ICCBased profiles found inside
// Separation/DeviceN alternate colour spaces.
func checkSingleICCBased(ctx context.Context, doc *document.Document, profileObj ast.PdfObject, part int, pageIdx int, csName ast.PdfName, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation) {
	if part < 2 {
		return
	}

	stream, err := document.ResolveStream(ctx, doc, profileObj)
	if err != nil {
		return
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return
	}
	hdr, ok := parseICCHeader(data)
	if !ok || hdr.colorSpace != "CMYK" {
		return
	}

	iccRef, _ := profileObj.(ast.PdfIndirectRef)
	identical := false

	if len(outputIntentData) > 0 && outputIntentHdr.colorSpace == "CMYK" {
		if iccRef.ObjectNum > 0 && outputIntentRef.ObjectNum > 0 &&
			iccRef.ObjectNum == outputIntentRef.ObjectNum && iccRef.GenNum == outputIntentRef.GenNum {
			identical = true
		}
		if !identical && ICCProfilesEqual(data, outputIntentData) {
			identical = true
		}
	}

	if !identical && blendingCSRef.ObjectNum > 0 {
		if iccRef.ObjectNum > 0 && iccRef.ObjectNum == blendingCSRef.ObjectNum && iccRef.GenNum == blendingCSRef.GenNum {
			identical = true
		}
		if !identical {
			blendStream, blendErr := document.ResolveStream(ctx, doc, blendingCSRef)
			if blendErr == nil {
				blendData, blendErr := doc.DecodeStream(ctx, blendStream)
				if blendErr == nil && ICCProfilesEqual(data, blendData) {
					identical = true
				}
			}
		}
	}

	if identical {
		*results = append(*results, failf(iccBasedIdentityRule(part),
			"page %d, ColorSpace %s alternate: ICCBased CMYK profile identical to OutputIntent", pageIdx+1, csName)...)
	}
}

// resolvePageBlendingICCRef returns the indirect reference to the ICCBased profile
// stream used as the page's transparency blending CS, or a zero ref if absent.
func resolvePageBlendingICCRef(ctx context.Context, doc *document.Document, page ast.PdfDict) ast.PdfIndirectRef {
	groupObj, ok := page["Group"]
	if !ok {
		return ast.PdfIndirectRef{}
	}
	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return ast.PdfIndirectRef{}
	}
	s, _ := document.ResolveName(ctx, doc, group["S"])
	if s != "Transparency" {
		return ast.PdfIndirectRef{}
	}
	csObj, ok := group["CS"]
	if !ok {
		return ast.PdfIndirectRef{}
	}
	arr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(arr) < 2 {
		return ast.PdfIndirectRef{}
	}
	csType, _ := document.ResolveName(ctx, doc, arr[0])
	if csType != "ICCBased" {
		return ast.PdfIndirectRef{}
	}
	if ref, ok := arr[1].(ast.PdfIndirectRef); ok {
		return ref
	}
	return ast.PdfIndirectRef{}
}

// expectedComponentCount returns the expected /N value for a given ICC colour
// space signature. A Lab profile has three components (ISO 15076-1, Table 19),
// and veraPDF's ISO 19005-1, 6.2.3.2 t2 test accepts N 3 for "Lab " as for
// "RGB ", measured on an output intent and on an ICCBased colour space.
func expectedComponentCount(colorSpace string) int {
	switch colorSpace {
	case "GRAY":
		return 1
	case "RGB ", "Lab ":
		return 3
	case "CMYK":
		return 4
	default:
		return 0
	}
}
