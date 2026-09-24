package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkXObjectGroup validates XObject restrictions for PDF/A conformance.
// It checks reference XObjects, form XObject forbidden keys, image /Alternates,
// image /OPI, and image /Interpolate constraints across all pages.
func checkXObjectGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, profile := vctx.doc, vctx.profile

	if vctx.ua {
		return nil
	}

	part := vctx.part
	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.xobjects: starting checks", "profile", profile, "part", part)

	var results []Violation
	visited := make(map[string]bool)

	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}

		res := vctx.pages[i].resources
		if res == nil {
			continue
		}

		// Collect Do operator targets from the page content stream so only
		// XObjects that are actually referenced (used) are checked.
		_, referencedXObjects, _, used, _ := scanContentStreamDeviceCS(ctx, vctx.pages[i].parsedOps(ctx), collectPatternCSNames(ctx, doc, res))

		scanXObjectResources(ctx, doc, res, i+1, part, profile, &results, visited, 0, referencedXObjects)
		scanPatternImages(ctx, doc, res, used, i+1, part, profile, &results, visited)

		// Check inline images for Interpolate = true.
		results = append(results, checkInlineImageInterpolate(vctx.pages[i].parsedOps(ctx), i+1, part)...)
	}

	doc.Logger.Debug("compliance.xobjects: checks complete", "violations", len(results))
	return results
}

// scanPatternImages applies the XObject rules to what a page's tiling patterns
// draw. A tiling pattern carries its own /Resources and content stream (ISO
// 32000-1, 8.7.3.1), which paint wherever a scn/SCN selects the pattern, so the
// images and forms that content invokes are used as surely as a page's own.
// veraPDF reaches them through the pattern the page selects and states the
// image rules there, ISO 19005-2, 6.2.8 t3 among them. A pattern the page
// declares but never selects paints nothing and is not judged, as veraPDF does
// not judge it.
//
// # Corpus evidence
//
//   - pdfjs bug1771477.pdf: draws an image with /Interpolate true inside a
//     tiling pattern.
func scanPatternImages(ctx context.Context, doc *document.Document, res ast.PdfDict, used *usedResources, pageNum, part int, profile string, results *[]Violation, visited map[string]bool) {
	patterns, err := document.ResolveDict(ctx, doc, res["Pattern"])
	if err != nil || patterns == nil || used == nil {
		return
	}
	for _, name := range sortedNames(patterns) {
		if ctx.Err() != nil {
			return
		}
		if !used.patterns[string(name)] {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, patterns[name])
		if err != nil {
			continue // a shading pattern: a dictionary with no content of its own
		}
		if patternType, _ := document.ResolveInt(ctx, doc, stream.Dict["PatternType"]); patternType != 1 {
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("compliance.xobjects: cannot decode tiling pattern", "pattern", name, "error", err)
			continue
		}
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("compliance.xobjects: cannot parse tiling pattern", "pattern", name, "error", err)
			continue
		}
		if patternRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); patternRes != nil {
			_, referenced, _, _, _ := scanContentStreamDeviceCS(ctx, ops, collectPatternCSNames(ctx, doc, patternRes))
			scanXObjectResources(ctx, doc, patternRes, pageNum, part, profile, results, visited, 0, referenced)
		}
		*results = append(*results, checkInlineImageInterpolate(ops, pageNum, part)...)
	}
}

// checkInlineImageInterpolate checks parsed BI operations for inline images
// with /I true or /Interpolate true. Uses parsed operations instead of raw
// byte scanning.
//
// # Corpus evidence
//
//   - pdfjs issue14256.pdf: writes both /I and /Interpolate on one inline
//     image.
func checkInlineImageInterpolate(ops []content.Operation, pageNum, part int) []Violation {
	if len(ops) == 0 {
		return nil
	}

	// Select the appropriate rule.
	var rule Rule
	switch part {
	case 1:
		rule = Rule_19005_1_6_2_4_t03
	case 2:
		rule = Rule_19005_2_6_2_8_t03
	case 3:
		rule = Rule_19005_3_6_2_8_t03
	case 4:
		rule = Rule_19005_4_6_2_7_1_t03
	default:
		return nil
	}

	for _, op := range ops {
		if op.Operator != "BI" || len(op.Operands) == 0 {
			continue
		}
		dict, ok := op.Operands[0].(ast.PdfDict)
		if !ok {
			continue
		}
		// /I is the abbreviation of /Interpolate (ISO 32000-1, 8.9.7, Table
		// 93). A dictionary that writes both is judged by /I, whichever comes
		// first: veraPDF passes /I false /Interpolate true and fails
		// /Interpolate false /I true.
		val, exists := dict["I"]
		if !exists {
			val = dict["Interpolate"]
		}
		if b, ok := val.(ast.PdfBool); ok && bool(b) {
			return failf(rule, "page %d: inline image has Interpolate=true", pageNum)
		}
	}
	return nil
}

// scanXObjectResources iterates XObject resources, validates each, and recurses
// into Form XObject sub-resources. At depth 0 (page level), only XObjects
// referenced by Do operators are checked. Unreferenced XObjects are skipped.
//
// # Corpus evidence
//
//   - govdocs thread_400_400019.pdf: veraPDF reports a soft mask's
//     /Interpolate 15 times against 57 plain XObjects (72 findings from
//     veraPDF, 57 from litho).
func scanXObjectResources(ctx context.Context, doc *document.Document, res ast.PdfDict, pageNum int, part int, profile string, results *[]Violation, visited map[string]bool, depth int, referencedNames map[string]bool) {
	if depth > maxFormXObjectDepth || ctx.Err() != nil {
		return
	}

	xobjectDict, err := document.ResolveDict(ctx, doc, res["XObject"])
	if err != nil || xobjectDict == nil {
		return
	}

	for _, name := range sortedNames(xobjectDict) {
		obj := xobjectDict[name]
		// At page level, only check XObjects actually invoked by Do operators.
		if depth == 0 && referencedNames != nil && !referencedNames[string(name)] {
			continue
		}
		if ctx.Err() != nil {
			return
		}

		// Cycle detection via indirect ref.
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			key := fmt.Sprintf("xo:%d", ref.ObjectNum)
			if visited[key] {
				continue
			}
			visited[key] = true
		}

		// XObjects are streams. Resolve as stream to access the dict.
		stream, err := document.ResolveStream(ctx, doc, obj)
		if err != nil {
			// Fallback: try as plain dict (some producers use dict-only XObjects).
			xobjDict, err := document.ResolveDict(ctx, doc, obj)
			if err != nil || xobjDict == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, xobjDict["Subtype"])
			*results = append(*results, checkXObject(ctx, doc, ast.PdfStream{}, xobjDict, subtype, name, pageNum, part, profile)...)
			continue
		}

		xobjDict := stream.Dict
		subtype, _ := document.ResolveName(ctx, doc, xobjDict["Subtype"])

		*results = append(*results, checkXObject(ctx, doc, stream, xobjDict, subtype, name, pageNum, part, profile)...)

		// An image's soft mask and stencil mask are image dictionaries in their
		// own right, and the image rules bind to them too: veraPDF builds a
		// PDSMaskImage for /SMask and reports its /Interpolate. Fixtures
		// confirm it states the rule for /Mask as well. /Mask may instead be a
		// colour-key ARRAY (ISO 32000-1, 8.9.6.4), which is no image and
		// resolves to no stream.
		if subtype == "Image" {
			for _, maskKey := range []ast.PdfName{"SMask", "Mask"} {
				maskObj, hasMask := xobjDict[maskKey]
				if !hasMask {
					continue
				}
				if ref, ok := maskObj.(ast.PdfIndirectRef); ok {
					key := fmt.Sprintf("xo:%d", ref.ObjectNum)
					if visited[key] {
						continue
					}
					visited[key] = true
				}
				maskStream, err := document.ResolveStream(ctx, doc, maskObj)
				if err != nil {
					continue
				}
				maskSubtype, _ := document.ResolveName(ctx, doc, maskStream.Dict["Subtype"])
				*results = append(*results, checkXObject(ctx, doc, maskStream, maskStream.Dict, maskSubtype,
					name+"/"+maskKey, pageNum, part, profile)...)
			}
		}

		// Recurse into Form XObject sub-resources.
		if subtype == "Form" {
			subRes, _ := document.ResolveDict(ctx, doc, xobjDict["Resources"])
			if subRes != nil {
				scanXObjectResources(ctx, doc, subRes, pageNum, part, profile, results, visited, depth+1, nil)
			}
		}
	}
}

// checkXObject validates a single XObject against the constraints for the given
// PDF/A part.
func checkXObject(
	ctx context.Context,
	doc *document.Document,
	stream ast.PdfStream,
	xobjDict ast.PdfDict,
	subtype ast.PdfName,
	name ast.PdfName,
	pageNum int,
	part int,
	profile string,
) []Violation {
	var results []Violation

	// §6.2.9: Reference XObjects forbidden (all parts).
	// A /Ref key in the XObject dict indicates a reference XObject. The
	// XObject presence tests here (/Ref, /OPI, /PS, /Alternates) read the key
	// as veraPDF's knownKey does ([knownKey]): a direct null is absent (ISO
	// 32000-1, 7.3.7), a reference counts. Measured on each.
	if knownKey(xobjDict, "Ref") {
		results = append(results, refXObjectRule(part, pageNum, name)...)
	}

	// PostScript XObjects forbidden (all parts).
	if subtype == "PS" {
		results = append(results, psXObjectRule(part, pageNum, name)...)
	}

	// Parts 2+/4 have additional form and image XObject checks.
	if part >= 2 {
		switch subtype {
		case "Form":
			results = append(results, checkFormXObject(ctx, doc, xobjDict, name, pageNum, part, profile)...)
		case "Image":
			results = append(results, checkImageXObject(ctx, doc, xobjDict, name, pageNum, part)...)
			// §6.2.8.3 / §6.2.7.3: JPEG2000 codestream constraints.
			if hasJPXFilter(ctx, doc, xobjDict) {
				results = append(results, checkJPXImage(ctx, doc, stream, name, pageNum, part)...)
			}
		}
	} else {
		// PDF/A-1 also checks form XObject forbidden keys (§6.2.5).
		if subtype == "Form" {
			results = append(results, checkFormXObjectPart1(ctx, doc, xobjDict, name, pageNum)...)
		}
		// PDF/A-1 also checks image constraints (§6.2.4).
		if subtype == "Image" {
			results = append(results, checkImageXObjectPart1(ctx, doc, xobjDict, name, pageNum)...)
		}
	}

	return results
}

// checkFormXObjectPart1 checks form XObject constraints for PDF/A-1 (§6.2.5).
func checkFormXObjectPart1(
	ctx context.Context,
	doc *document.Document,
	formDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
) []Violation {
	var results []Violation

	// /OPI key forbidden (ISO 19005-1 §6.2.4 t2 covers Image and Form
	// XObjects; the image path is checked in checkImageXObjectPart1).
	if knownKey(formDict, "OPI") {
		results = append(results, failf(Rule_19005_1_6_2_4_t02, "page %d, XObject %s", pageNum, name)...)
	}

	// /Subtype2 with value /PS is forbidden.
	if sub2Obj, hasSub2 := formDict["Subtype2"]; hasSub2 {
		sub2Name, _ := document.ResolveName(ctx, doc, sub2Obj)
		if sub2Name == "PS" {
			results = append(results, failf(Rule_19005_1_6_2_5_t01, "page %d, XObject %s", pageNum, name)...)
		}
	}

	// /PS key is forbidden in form XObject.
	if knownKey(formDict, "PS") {
		results = append(results, failf(Rule_19005_1_6_2_5_t01, "page %d, XObject %s", pageNum, name)...)
	}

	return results
}

// checkImageXObjectPart1 checks image XObject constraints for PDF/A-1 (§6.2.4).
func checkImageXObjectPart1(
	ctx context.Context,
	doc *document.Document,
	imgDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
) []Violation {
	var results []Violation

	// /Alternates forbidden.
	if knownKey(imgDict, "Alternates") {
		results = append(results, failf(Rule_19005_1_6_2_4_t01, "page %d, XObject %s", pageNum, name)...)
	}

	// /OPI forbidden.
	if knownKey(imgDict, "OPI") {
		results = append(results, failf(Rule_19005_1_6_2_4_t02, "page %d, XObject %s", pageNum, name)...)
	}

	// /Interpolate must be false or absent.
	if interpObj, hasInterp := imgDict["Interpolate"]; hasInterp {
		interpVal, err := document.ResolveBool(ctx, doc, interpObj)
		if err == nil && interpVal {
			results = append(results, failf(Rule_19005_1_6_2_4_t03, "page %d, XObject %s", pageNum, name)...)
		}
	}

	// /Intent must be a valid rendering intent (§6.2.9), the same constraint
	// parts 2-4 enforce in checkImageXObject.
	if intentObj, hasIntent := imgDict["Intent"]; hasIntent {
		intent, _ := document.ResolveName(ctx, doc, intentObj)
		if !validRenderingIntents[string(intent)] {
			results = append(results, failf(renderingIntentRule(1),
				"page %d, XObject %s: invalid rendering intent %q", pageNum, name, intent)...)
		}
	}

	results = append(results, checkImageBitsPerComponent(ctx, doc, imgDict, name, pageNum, 1)...)

	return results
}

// checkFormXObject checks form XObject constraints for PDF/A parts 2+ and 4.
func checkFormXObject(
	ctx context.Context,
	doc *document.Document,
	formDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
	part int,
	_ string,
) []Violation {
	var results []Violation

	// Select the appropriate rule based on part.
	var formRule Rule
	switch part {
	case 2:
		formRule = Rule_19005_2_6_2_9_t01
	case 3:
		formRule = Rule_19005_3_6_2_9_t01
	case 4:
		formRule = Rule_19005_4_6_2_8_1_t01
	}

	// /OPI key forbidden in form XObject.
	if knownKey(formDict, "OPI") {
		results = append(results, failf(formRule, "page %d, XObject %s: OPI key present", pageNum, name)...)
	}

	// ISO 19005-4, 6.2.8.1 forbids the OPI key and nothing else: ISO 32000-2
	// removed PostScript XObjects from the format, so part 4 has no PostScript
	// prohibition left to make. Only ISO 19005-2 and -3, 6.2.9 fold the three
	// prohibitions into one rule.
	if part == 4 {
		return results
	}

	// Parts 2/3: /Subtype2 with value /PS forbidden, /PS key forbidden.
	if sub2Obj, hasSub2 := formDict["Subtype2"]; hasSub2 {
		sub2Name, _ := document.ResolveName(ctx, doc, sub2Obj)
		if sub2Name == "PS" {
			results = append(results, failf(formRule, "page %d, XObject %s: Subtype2 is PS", pageNum, name)...)
		}
	}

	if knownKey(formDict, "PS") {
		results = append(results, failf(formRule, "page %d, XObject %s: PS key present", pageNum, name)...)
	}

	return results
}

// checkImageXObject checks image XObject constraints for PDF/A parts 2+ and 4.
func checkImageXObject(
	ctx context.Context,
	doc *document.Document,
	imgDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
	part int,
) []Violation {
	var results []Violation

	// Select the appropriate rules based on part.
	var altRule, opiRule, interpRule Rule
	switch part {
	case 2:
		altRule = Rule_19005_2_6_2_8_t01
		opiRule = Rule_19005_2_6_2_8_t02
		interpRule = Rule_19005_2_6_2_8_t03
	case 3:
		altRule = Rule_19005_3_6_2_8_t01
		opiRule = Rule_19005_3_6_2_8_t02
		interpRule = Rule_19005_3_6_2_8_t03
	case 4:
		altRule = Rule_19005_4_6_2_7_1_t01
		opiRule = Rule_19005_4_6_2_7_1_t02
		interpRule = Rule_19005_4_6_2_7_1_t03
	}

	// /Alternates forbidden (§6.2.7/6.2.8).
	if knownKey(imgDict, "Alternates") {
		results = append(results, failf(altRule, "page %d, XObject %s", pageNum, name)...)
	}

	// /OPI forbidden (§6.2.8).
	if knownKey(imgDict, "OPI") {
		results = append(results, failf(opiRule, "page %d, XObject %s", pageNum, name)...)
	}

	// /Interpolate must be false or absent (§6.2.8).
	if interpObj, hasInterp := imgDict["Interpolate"]; hasInterp {
		interpVal, err := document.ResolveBool(ctx, doc, interpObj)
		if err == nil && interpVal {
			results = append(results, failf(interpRule, "page %d, XObject %s", pageNum, name)...)
		}
	}

	// /Intent must be a valid rendering intent (§6.2.6).
	if intentObj, hasIntent := imgDict["Intent"]; hasIntent {
		intent, _ := document.ResolveName(ctx, doc, intentObj)
		if !validRenderingIntents[string(intent)] {
			riRule := renderingIntentRule(part)
			results = append(results, failf(riRule,
				"page %d, XObject %s: invalid rendering intent %q", pageNum, name, intent)...)
		}
	}

	results = append(results, checkImageBitsPerComponent(ctx, doc, imgDict, name, pageNum, part)...)

	return results
}

// refXObjectRule returns the appropriate reference XObject rule violation for
// the given PDF/A part.
func refXObjectRule(part int, pageNum int, name ast.PdfName) []Violation {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_2_6_t01, "page %d, XObject %s", pageNum, name)
	case 2:
		return failf(Rule_19005_2_6_2_9_t02, "page %d, XObject %s", pageNum, name)
	case 3:
		return failf(Rule_19005_3_6_2_9_t02, "page %d, XObject %s", pageNum, name)
	case 4:
		return failf(Rule_19005_4_6_2_8_2_t01, "page %d, XObject %s", pageNum, name)
	}
	return nil
}

// psXObjectRule returns the appropriate PostScript XObject rule violation for
// the given PDF/A part.
func psXObjectRule(part int, pageNum int, name ast.PdfName) []Violation {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_2_7_t01, "page %d, XObject %s", pageNum, name)
	case 2:
		return failf(Rule_19005_2_6_2_9_t03, "page %d, XObject %s", pageNum, name)
	case 3:
		return failf(Rule_19005_3_6_2_9_t03, "page %d, XObject %s", pageNum, name)
		// PDF/A-4 does not have a separate PS XObject rule; it's covered by form checks.
	}
	return nil
}

// bpcRule selects the image /BitsPerComponent value rule for the given part.
func bpcRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_4_t04, Rule_19005_2_6_2_8_t04, Rule_19005_3_6_2_8_t04, Rule_19005_4_6_2_7_1_t04)
}

// bpcMaskRule selects the image-mask /BitsPerComponent rule for the given part.
func bpcMaskRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_4_t05, Rule_19005_2_6_2_8_t05, Rule_19005_3_6_2_8_t05, Rule_19005_4_6_2_7_1_t05)
}

// checkImageBitsPerComponent validates an image XObject's /BitsPerComponent.
// A regular image allows 1, 2, 4, or 8, and additionally 16 for PDF/A-2 and
// later (part 1: §6.2.4 t04, parts 2-3: §6.2.8 t04, part 4: §6.2.7.1 t04); an
// image mask must use 1 (the corresponding t05). An absent key is permitted.
// JPXDecode images are skipped: their bit depth is defined by the JPEG2000
// codestream (validated separately) and /BitsPerComponent should be absent, so
// the value rule does not apply. This is a detection-only check: correcting the
// bit depth would require decompressing and resampling the pixel data, which
// risks visible corruption and is out of scope for remediation.
func checkImageBitsPerComponent(ctx context.Context, doc *document.Document, imgDict ast.PdfDict, name ast.PdfName, pageNum, part int) []Violation {
	bpcObj, ok := imgDict["BitsPerComponent"]
	if !ok {
		return nil
	}
	if hasJPXFilter(ctx, doc, imgDict) {
		return nil
	}
	bpc, err := document.ResolveInt(ctx, doc, bpcObj)
	if err != nil {
		return nil
	}

	// An image mask is intrinsically 1 bit per component (ISO 19005 §6.2.4
	// t05 / §6.2.8 t05 / §6.2.7.1 t05). An absent or unresolvable /ImageMask
	// defaults to false, so the regular-image branch applies.
	if mask, _ := document.ResolveBool(ctx, doc, imgDict["ImageMask"]); mask {
		if bpc != 1 {
			return failf(bpcMaskRule(part), "page %d, XObject %s: image mask BitsPerComponent=%d (must be 1)", pageNum, name, bpc)
		}
		return nil
	}

	valid := bpc == 1 || bpc == 2 || bpc == 4 || bpc == 8 || (part >= 2 && bpc == 16)
	if !valid {
		return failf(bpcRule(part), "page %d, XObject %s: BitsPerComponent=%d", pageNum, name, bpc)
	}
	return nil
}
