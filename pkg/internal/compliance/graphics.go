package compliance

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkGraphicsGroup validates ExtGState, halftones, rendering intent, and
// overprint restrictions required by PDF/A parts 1-4.
func checkGraphicsGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, profile := vctx.doc, vctx.profile
	part := vctx.part

	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.graphics: starting checks", "profile", profile, "part", part)

	// One ExtGState dictionary is judged once for the whole document; see
	// [objectJudgedOnce].
	judgedGS := make(map[int]bool)

	var results []Violation
	for i := range vctx.pages {
		p := &vctx.pages[i]
		page := p.dict
		if ctx.Err() != nil {
			return results
		}

		res := p.resources
		hasContent := len(p.content) > 0

		// §6.2.2 t2/t3: resource completeness against the page's OWN
		// Resources, with names resolvable only through Pages-tree
		// inheritance reported separately. A name resolved solely via the
		// inherited Pages-tree /Resources is an inherited reference (t2); a
		// name resolved nowhere is undefined (t3). Conflating own-or-fallback
		// page resources hides this distinction.
		if hasContent {
			ops := p.parsedOps(ctx)
			if ops != nil {
				ownRes := resolveResources(ctx, doc, page)
				var inheritedRes ast.PdfDict
				if pagesDict, err := document.ResolveDict(ctx, doc, vctx.catalog["Pages"]); err == nil && pagesDict != nil {
					inheritedRes = resolveResources(ctx, doc, pagesDict)
				}
				results = append(results, checkContentStreamResourcesOps(ctx, doc, ops, p.overflow, ownRes, inheritedRes, part, i+1, false)...)
				// Check inline image /Intent values from parsed BI operations.
				results = append(results, checkInlineImageIntent(ops, part, i+1)...)
				// Inline image filters are stated by checkInlineImageFilters,
				// which scans the same page's raw bytes.
			}
		}

		if res == nil {
			continue
		}

		results = append(results, checkExtGStateGraphics(ctx, doc, res, profile, part, i+1, judgedGS)...)

		if hasContent {
			if part >= 2 {
				results = append(results, checkContentStreamStateOps(ctx, doc, p.parsedOps(ctx), res, part, i+1)...)
			}
		}

		// Also check Form XObject content streams for resource completeness.
		// Only scan Forms referenced by Do operators in the page content.
		var referencedXObjects map[string]bool
		var used *usedResources
		if hasContent {
			_, referencedXObjects, _, used, _ = scanContentStreamDeviceCS(ctx, vctx.pages[i].parsedOps(ctx), collectPatternCSNames(ctx, doc, res))
		}
		results = append(results, checkFormXObjectResources(ctx, doc, res, part, i+1, referencedXObjects)...)

		// Check the content streams of the tiling patterns the page paints.
		results = append(results, checkPatternResources(ctx, doc, res, part, i+1, used)...)

		// Scan Form XObject ExtGState for halftone/rendering intent violations.
		results = append(results, checkFormXObjectExtGState(ctx, doc, res, profile, part, i+1, judgedGS)...)

		// Check Type3 font CharProc resource completeness.
		results = append(results, checkType3CharProcResources(ctx, doc, res, part, i+1)...)

		// Scan annotation appearance ExtGState for halftone/rendering intent.
		results = append(results, checkAnnotationAppearanceExtGState(ctx, doc, page, profile, part, i+1, judgedGS)...)

		// Scan annotation appearance streams for undefined operators.
		results = append(results, checkAnnotationAppearanceOps(ctx, doc, page, part, i+1)...)
	}

	doc.Logger.Debug("compliance.graphics: checks complete", "violations", len(results))
	return results
}

// checkFormXObjectResources checks content stream resource completeness for
// Form XObjects referenced by Do operators on the page. Each Form XObject
// has its own Resources dict; operators in its content stream must reference
// resources defined there.
func checkFormXObjectResources(ctx context.Context, doc *document.Document, pageRes ast.PdfDict, part int, pageNum int, referencedNames map[string]bool) []Violation {
	if pageRes == nil {
		return nil
	}
	xoDict, err := document.ResolveDict(ctx, doc, pageRes["XObject"])
	if err != nil || xoDict == nil {
		return nil
	}

	var results []Violation
	for _, xoName := range sortedNames(xoDict) {
		xoObj := xoDict[xoName]
		// Only check Form XObjects that are actually referenced by Do operators.
		if referencedNames != nil && !referencedNames[string(xoName)] {
			continue
		}
		if ctx.Err() != nil {
			return results
		}
		stream, err := document.ResolveStream(ctx, doc, xoObj)
		if err != nil {
			doc.Logger.Debug("compliance.graphics: cannot resolve XObject stream", "error", err)
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		if subtype != "Form" {
			continue
		}
		formData, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(formData) == 0 {
			continue
		}
		formRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if formRes == nil {
			formRes = make(ast.PdfDict)
		}
		// Per ISO 19005-2 §6.2.2, a Form XObject content stream resolves
		// resource names against its own /Resources first. A name found only
		// in the PAGE's resources is an inherited reference (t2), so the page
		// resources serve as the fallback scope when scanning the form stream.
		results = append(results, checkContentStreamResources(ctx, doc, formData, formRes, pageRes, part, pageNum, true)...)
	}
	return results
}

// checkPatternResources checks the content stream of every tiling pattern the
// page paints. A tiling pattern draws its cell with a content stream of its own
// (ISO 32000-1, 8.7.3.1), so the operand limits and resource rules apply there
// as they do on the page. ISO 19005-1, 6.1.12 t2 states the real-magnitude
// limit over CosReal, which is every real the file writes, pattern cells
// included.
//
// Only a pattern a scn or SCN selects is judged, the same scope
// [scanPatternImages] uses. veraPDF passes a pattern the page declares and
// never paints, measured on a fixture whose unselected pattern carries the same
// out-of-range real.
//
// # Corpus evidence
//
//   - pdfjs 22060_A1_01_Plans.pdf: keeps its 136 KB page stream inside the
//     limit and holds -39455.5 in patterns /R21 and /R24, and veraPDF reports
//     it there six times.
func checkPatternResources(ctx context.Context, doc *document.Document, pageRes ast.PdfDict, part, pageNum int, used *usedResources) []Violation {
	if pageRes == nil || used == nil {
		return nil
	}
	patterns, err := document.ResolveDict(ctx, doc, pageRes["Pattern"])
	if err != nil || patterns == nil {
		return nil
	}

	var results []Violation
	for _, name := range sortedNames(patterns) {
		if ctx.Err() != nil {
			return results
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
		if err != nil || len(data) == 0 {
			continue
		}
		patternRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if patternRes == nil {
			patternRes = make(ast.PdfDict)
		}
		// A pattern content stream resolves resource names against its own
		// /Resources first, with the page's serving as the fallback scope, as a
		// Form XObject's does.
		results = append(results, checkContentStreamResources(ctx, doc, data, patternRes, pageRes, part, pageNum, true)...)
	}
	return results
}

// checkType3CharProcResources checks Type3 font CharProc content streams for
// resource references. Type3 CharProcs are content streams that may reference
// colour spaces, fonts, etc. If they do, the Type3 font dict must have an
// explicitly associated /Resources entry.
func checkType3CharProcResources(ctx context.Context, doc *document.Document, pageRes ast.PdfDict, part, pageNum int) []Violation {
	if pageRes == nil {
		return nil
	}
	fontDict, err := document.ResolveDict(ctx, doc, pageRes["Font"])
	if err != nil || fontDict == nil {
		return nil
	}
	var results []Violation
	for _, fontName := range sortedNames(fontDict) {
		fontRef := fontDict[fontName]
		fd, err := document.ResolveDict(ctx, doc, fontRef)
		if err != nil || fd == nil {
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"])
		if subtype != "Type3" {
			continue
		}
		// §6.2.2 scopes resource completeness to the CharProc stream: a name
		// resolved only through the page's resources is inherited (t2), one
		// resolved nowhere is undefined (t3). A Type3 font without its own
		// /Resources therefore fires t2 per inherited name, never a
		// blanket missing-dictionary violation.
		type3Res, err := document.ResolveDict(ctx, doc, fd["Resources"])
		if err != nil || type3Res == nil {
			type3Res = make(ast.PdfDict)
		}
		charProcs, err := document.ResolveDict(ctx, doc, fd["CharProcs"])
		if err != nil || charProcs == nil {
			continue
		}
		for _, cpName := range sortedNames(charProcs) {
			cpObj := charProcs[cpName]
			stream, err := document.ResolveStream(ctx, doc, cpObj)
			if err != nil {
				doc.Logger.Debug("compliance.graphics: cannot resolve CharProc stream", "error", err)
				continue
			}
			data, err := doc.DecodeStream(ctx, stream)
			if err != nil || len(data) == 0 {
				continue
			}
			results = append(results, checkContentStreamResources(ctx, doc, data, type3Res, pageRes, part, pageNum, true)...)
		}
	}
	return results
}

// checkAnnotationAppearanceExtGState scans annotation appearance stream
// resources for ExtGState entries (rendering intent, halftone, etc.).
func checkAnnotationAppearanceExtGState(ctx context.Context, doc *document.Document, page ast.PdfDict, profile string, part, pageNum int, judged map[int]bool) []Violation {
	annotsArr, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil || len(annotsArr) == 0 {
		return nil
	}
	var results []Violation
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
				doc.Logger.Debug("compliance.graphics: cannot resolve appearance stream", "error", err)
				continue
			}
			apRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
			if apRes != nil {
				results = append(results, checkExtGStateGraphics(ctx, doc, apRes, profile, part, pageNum, judged)...)
			}
		}
	}
	return results
}

// checkAnnotationAppearanceOps reports content-stream operators that ISO
// 32000-1 does not define when they appear inside an annotation appearance
// stream.
//
// An appearance stream is a content stream (ISO 32000-1, 12.5.5), so the
// prohibition in ISO 19005-2, 6.2.2 reaches it. The page-level scan only sees
// a page's own /Contents and the Form XObjects its resources name, so an
// operator inside /Annots[]/AP went unreported while veraPDF flagged it.
//
// The named-resource test runs here too, for the one part that states it.
// An earlier note left it out as unmeasured against the reference validator;
// it has since been measured. veraPDF 1.30.2 fails ISO 19005-4, 6.2.2 t3 on
// appearance streams.
//
// # Corpus evidence
//
//   - pdfjs issue14438.pdf: runs /OC /MC0 BDC in 2 of its 19 appearance
//     streams, whose own /Resources carry no /Properties, and veraPDF reports
//     exactly those 2.
func checkAnnotationAppearanceOps(ctx context.Context, doc *document.Document, page ast.PdfDict, part, pageNum int) []Violation {
	rule := undefinedOperatorRule(part)
	if rule.Specification == "" && missingResourceRule(part).Specification == "" {
		return nil
	}
	pageRes := resolveResources(ctx, doc, page)
	annotsArr, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil || len(annotsArr) == 0 {
		return nil
	}

	var results []Violation
	// An appearance stream shared by several annotations is one stream, so it
	// answers once per page rather than once per reference.
	seen := make(map[int]bool)
	for _, annotObj := range annotsArr {
		if err := ctx.Err(); err != nil {
			return results
		}
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
			for _, streamObj := range appearanceStreamObjects(ctx, doc, apObj) {
				if ref, isRef := streamObj.(ast.PdfIndirectRef); isRef {
					if seen[ref.ObjectNum] {
						continue
					}
					seen[ref.ObjectNum] = true
				}
				results = append(results, checkAppearanceStreamOps(ctx, doc, streamObj, rule, pageRes, part, pageNum)...)
			}
		}
	}
	return results
}

// appearanceStreamObjects returns the appearance streams an /AP entry holds.
// The entry is a stream, or a subdictionary keyed by appearance state when the
// annotation has several (ISO 32000-1, 12.5.5), as a checkbox does with /Off
// and /Yes.
func appearanceStreamObjects(ctx context.Context, doc *document.Document, apObj ast.PdfObject) []ast.PdfObject {
	if _, err := document.ResolveStream(ctx, doc, apObj); err == nil {
		return []ast.PdfObject{apObj}
	}
	stateDict, err := document.ResolveDict(ctx, doc, apObj)
	if err != nil || stateDict == nil {
		return nil
	}
	out := make([]ast.PdfObject, 0, len(stateDict))
	for _, name := range sortedNames(stateDict) {
		out = append(out, stateDict[name])
	}
	return out
}

// checkAppearanceStreamOps parses one appearance stream, reports every operator
// outside [validPDFOperators], and reports the names it references that no
// resource dictionary defines.
func checkAppearanceStreamOps(ctx context.Context, doc *document.Document, streamObj ast.PdfObject, rule Rule, pageRes ast.PdfDict, part, pageNum int) []Violation {
	stream, err := document.ResolveStream(ctx, doc, streamObj)
	if err != nil {
		doc.Logger.Debug("compliance.graphics: cannot resolve appearance stream", "error", err)
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return nil
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance.graphics: cannot parse appearance stream", "error", err)
		return nil
	}

	var results []Violation
	for idx, op := range ops {
		if idx%100 == 0 && ctx.Err() != nil {
			return results
		}
		if op.Operator == "" || validPDFOperators[op.Operator] {
			continue
		}
		results = append(results, failf(rule,
			"page %d: undefined operator %q in annotation appearance", pageNum, op.Operator)...)
	}

	// Only ISO 19005-4 states the undefined-name test (§6.2.2 t3), so the scan
	// is confined to the part that can report it. The inherited-resource
	// dictionary is nil: a name the page defines is not an inherited reference
	// for an appearance stream, and veraPDF reports nothing at all for it.
	if missingResourceRule(part).Specification == "" {
		return results
	}
	streamRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil {
		doc.Logger.Debug("compliance.graphics: cannot resolve appearance resources", "error", err)
		return results
	}
	for _, v := range checkContentStreamResourcesOps(ctx, doc, ops, nil,
		appearanceResources(ctx, doc, streamRes, pageRes), nil, part, pageNum, false) {
		v.Context += ", annotation appearance"
		results = append(results, v)
	}
	return results
}

// appearanceResources returns the resource dictionary an appearance stream's
// names resolve against: its own /Resources, with the page's standing in for
// every name the stream does not define itself.
//
// The fallback is per name rather than per category, measured against veraPDF
// 1.30.2. Defining /MC0 on the page alone silences §6.2.2 t3, and so does
// giving the appearance stream a /Properties holding a different name while the
// page defines /MC0. ISO 32000-1, 12.5.5 leaves an appearance stream's
// resources to the stream, so the page fills the gaps rather than replacing
// them.
//
// # Corpus evidence
//
//   - pdfjs issue14438.pdf: the file both /MC0 measurements were made on.
func appearanceResources(ctx context.Context, doc *document.Document, streamRes, pageRes ast.PdfDict) ast.PdfDict {
	if streamRes == nil {
		return pageRes
	}
	if pageRes == nil {
		return streamRes
	}

	merged := make(ast.PdfDict, len(streamRes)+len(pageRes))
	maps.Copy(merged, streamRes)
	for category, pageEntry := range pageRes {
		streamEntry, ok := streamRes[category]
		if !ok {
			merged[category] = pageEntry
			continue
		}
		streamSub, err := document.ResolveDict(ctx, doc, streamEntry)
		if err != nil || streamSub == nil {
			continue
		}
		pageSub, err := document.ResolveDict(ctx, doc, pageEntry)
		if err != nil || pageSub == nil {
			continue
		}
		sub := make(ast.PdfDict, len(pageSub)+len(streamSub))
		maps.Copy(sub, pageSub)
		maps.Copy(sub, streamSub)
		merged[category] = sub
	}
	return merged
}

// checkFormXObjectExtGState scans Form XObject resources for ExtGState
// entries (halftone, rendering intent, etc.) that the page-level scan misses.
func checkFormXObjectExtGState(ctx context.Context, doc *document.Document, pageRes ast.PdfDict, profile string, part, pageNum int, judged map[int]bool) []Violation {
	if pageRes == nil {
		return nil
	}
	xoDict, err := document.ResolveDict(ctx, doc, pageRes["XObject"])
	if err != nil || xoDict == nil {
		return nil
	}
	var results []Violation
	for _, xoName := range sortedNames(xoDict) {
		xoObj := xoDict[xoName]
		stream, err := document.ResolveStream(ctx, doc, xoObj)
		if err != nil {
			doc.Logger.Debug("compliance.graphics: cannot resolve XObject stream", "error", err)
			continue
		}
		st, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		if st != "Form" {
			continue
		}
		formRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if formRes == nil {
			continue
		}
		results = append(results, checkExtGStateGraphics(ctx, doc, formRes, profile, part, pageNum, judged)...)
	}
	return results
}

// checkExtGStateGraphics validates all ExtGState entries in a page's resources.
//
// # Corpus evidence
//
//   - govdocs thread_100_100666.pdf: references ExtGState object 190 from 30
//     pages, and veraPDF reports its /TR once. It backs judging a shared
//     dictionary once.
func checkExtGStateGraphics(ctx context.Context, doc *document.Document, resources ast.PdfDict, profile string, part, pageNum int, judged map[int]bool) []Violation {
	gsDict, err := document.ResolveDict(ctx, doc, resources["ExtGState"])
	if err != nil || gsDict == nil {
		return nil
	}

	var results []Violation
	for _, gsName := range sortedNames(gsDict) {
		gsObj := gsDict[gsName]
		if ctx.Err() != nil {
			return results
		}

		// One ExtGState dictionary, one finding: these rules forbid keys OF THE
		// DICTIONARY (ISO 19005-2 §6.2.5 among them), so a state 30 pages share
		// is one violation, not 30. veraPDF states each against its single
		// PDExtGState. See [objectJudgedOnce].
		if objectJudgedOnce(gsObj, judged) {
			continue
		}

		gs, err := document.ResolveDict(ctx, doc, gsObj)
		if err != nil || gs == nil {
			continue
		}

		results = append(results, checkTransferFunction(ctx, doc, gs, profile, part, pageNum, string(gsName))...)
		results = append(results, checkHTPKey(ctx, doc, gs, profile, part, pageNum, string(gsName))...)
		results = append(results, checkHalftone(ctx, doc, gs, profile, part, pageNum, string(gsName))...)
		results = append(results, checkRenderingIntent(ctx, doc, gs, profile, part, pageNum, string(gsName))...)
		results = append(results, checkOverprint(ctx, doc, gs, profile, part, pageNum, string(gsName))...)
	}
	return results
}

// trForbiddenRule returns the rule for TR key being forbidden, varying by part.
func trForbiddenRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_8_t01, Rule_19005_2_6_2_5_t01, Rule_19005_3_6_2_5_t01, Rule_19005_4_6_2_5_t01)
}

// tr2NotDefaultRule returns the rule for TR2 not being /Default.
func tr2NotDefaultRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_8_t02, Rule_19005_2_6_2_5_t02, Rule_19005_3_6_2_5_t02, Rule_19005_4_6_2_5_t02)
}

func checkTransferFunction(ctx context.Context, doc *document.Document, gs ast.PdfDict, _ string, part, pageNum int, gsName string) []Violation {
	var results []Violation

	// PDF/A-1: /TR key is completely forbidden.
	// PDF/A-2+/4: /TR key is also forbidden. A /TR written as null is absent
	// (ISO 32000-1, 7.3.7) and one naming any object counts, as veraPDF's
	// knownKey reads it ([knownKey]). Measured.
	if knownKey(gs, "TR") {
		results = append(results, failf(trForbiddenRule(part), "page %d, ExtGState %s", pageNum, gsName)...)
	}

	// PDF/A-1: /TR2 check uses same rule (6.2.8 t02).
	// PDF/A-2+/4: /TR2 must be /Default if present.
	if tr2Obj, hasTR2 := gs["TR2"]; hasTR2 {
		tr2Name, _ := document.ResolveName(ctx, doc, tr2Obj)
		if tr2Name != "Default" {
			results = append(results, failf(tr2NotDefaultRule(part), "page %d, ExtGState %s", pageNum, gsName)...)
		}
	}

	return results
}

// htpForbiddenRule returns the rule for HTP key being forbidden.
func htpForbiddenRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_5_t03, Rule_19005_3_6_2_5_t03, Rule_19005_4_6_2_5_t03)
}

func checkHTPKey(_ context.Context, _ *document.Document, gs ast.PdfDict, _ string, part, pageNum int, gsName string) []Violation {
	// The forbidden key differs by part: ISO 19005-2 and -3 §6.2.5 t3 read
	// "containsHTP == false", while ISO 19005-4 §6.2.5 t3 reads
	// "containsHTO == false". Checking both keys under every part reported
	// /HTP against part 4 and /HTO against parts 2 and 3, neither of which
	// the governing rule constrains.
	key := ast.PdfName("HTP")
	if part == 4 {
		key = ast.PdfName("HTO")
	}
	if part < 2 {
		return nil
	}
	if _, has := gs[key]; has {
		return failf(htpForbiddenRule(part), "page %d, ExtGState %s: %s key forbidden", pageNum, gsName, key)
	}
	return nil
}

// halftoneTypeRule returns the rule for invalid HalftoneType.
func halftoneTypeRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_5_t04, Rule_19005_3_6_2_5_t04, Rule_19005_4_6_2_5_t04)
}

// halftoneNameRule returns the rule for forbidden HalftoneName.
func halftoneNameRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_5_t05, Rule_19005_3_6_2_5_t05, Rule_19005_4_6_2_5_t05)
}

// halftoneTransferRule returns the rule for non-Default TransferFunction.
func halftoneTransferRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_5_t06, Rule_19005_3_6_2_5_t06, Rule_19005_4_6_2_5_t06)
}

func checkHalftone(ctx context.Context, doc *document.Document, gs ast.PdfDict, _ string, part, pageNum int, gsName string) []Violation {
	// Halftone checks apply to parts 2+ and 4 only.
	if part < 2 {
		return nil
	}

	htObj, hasHT := gs["HT"]
	if !hasHT {
		return nil
	}

	ht, err := document.ResolveDict(ctx, doc, htObj)
	if err != nil || ht == nil {
		return nil
	}

	var results []Violation

	// HalftoneType must be 1 or 5.
	htType, err := document.ResolveInt(ctx, doc, ht["HalftoneType"])
	if err == nil {
		if htType != 1 && htType != 5 {
			results = append(results, failf(halftoneTypeRule(part), "page %d, ExtGState %s, HalftoneType %d", pageNum, gsName, htType)...)
		}
	}

	// HalftoneName forbidden. One that resolves to null names nothing (ISO
	// 32000-1, 7.3.7 and 7.3.10): veraPDF reports neither a null nor a reference
	// to a null object. Measured.
	if nonNullKeyPresent(ctx, doc, ht, "HalftoneName") {
		results = append(results, failf(halftoneNameRule(part), "page %d, ExtGState %s", pageNum, gsName)...)
	}

	// TransferFunction must be /Default or absent. One that resolves to null is
	// absent (ISO 32000-1, 7.3.7 and 7.3.10), measured as for HalftoneName.
	if tfObj, hasTF := ht["TransferFunction"]; hasTF && !resourceEntryNull(ctx, doc, tfObj) {
		tfName, _ := document.ResolveName(ctx, doc, tfObj)
		if tfName != "Default" {
			results = append(results, failf(halftoneTransferRule(part), "page %d, ExtGState %s", pageNum, gsName)...)
		}
	}

	// For Type 5 composite halftones (parts 2+), §6.2.5 t06 governs the
	// TransferFunction on each sub-halftone dict. A device colorant
	// (Cyan/Magenta/Yellow/Black) already has a process transfer function, so
	// a per-component TransferFunction would redefine tone reproduction and is
	// forbidden. A non-process colorant (Red/Green/Blue/spots) needs its own
	// transfer function to be reproducible, so it must be present:
	//   - Default: always exempt
	//   - CMYK primaries (Cyan/Magenta/Yellow/Black): TF must be absent
	//   - Non-primary colorants (Red/Green/Blue/spots): TF must be present
	if htType == 5 && part >= 2 {
		cmykPrimaries := map[ast.PdfName]bool{
			"Cyan": true, "Magenta": true, "Yellow": true, "Black": true,
		}
		htMetaKeys := map[ast.PdfName]bool{
			"HalftoneType": true, "HalftoneName": true, "Type": true, "TransferFunction": true,
		}
		for _, key := range sortedNames(ht) {
			subObj := ht[key]
			if htMetaKeys[key] || key == "Default" {
				continue
			}
			subHT, err := document.ResolveDict(ctx, doc, subObj)
			if err != nil || subHT == nil {
				continue
			}
			_, hasTF := subHT["TransferFunction"]
			if cmykPrimaries[key] && hasTF {
				// CMYK primary: TF must be absent.
				results = append(results, failf(halftoneTransferRule(part),
					"page %d, ExtGState %s, CMYK halftone component %s has TransferFunction", pageNum, gsName, key)...)
			} else if !cmykPrimaries[key] && !hasTF {
				// Non-primary: TF must be present.
				results = append(results, failf(halftoneTransferRule(part),
					"page %d, ExtGState %s, non-primary halftone component %s missing TransferFunction", pageNum, gsName, key)...)
			}
		}
	}

	return results
}

// validRenderingIntents lists the four standard PDF rendering intents.
var validRenderingIntents = map[string]bool{ //nolint:gochecknoglobals // constant lookup table.
	"RelativeColorimetric": true,
	"AbsoluteColorimetric": true,
	"Perceptual":           true,
	"Saturation":           true,
}

// renderingIntentRule returns the rule for invalid rendering intent.
func renderingIntentRule(part int) Rule {
	// PDF/A-4 has no separate rendering intent rule.
	return ruleByPart(part, Rule_19005_1_6_2_9_t01, Rule_19005_2_6_2_6_t01, Rule_19005_3_6_2_6_t01, Rule{})
}

func checkRenderingIntent(ctx context.Context, doc *document.Document, gs ast.PdfDict, _ string, part, pageNum int, gsName string) []Violation {
	// PDF/A-4 does not define a rendering intent rule.
	if part == 4 {
		return nil
	}

	riObj, hasRI := gs["RI"]
	if !hasRI {
		return nil
	}

	ri, _ := document.ResolveName(ctx, doc, riObj)
	if ri == "" {
		return nil
	}

	if !validRenderingIntents[string(ri)] {
		return failf(renderingIntentRule(part), "page %d, ExtGState %s, intent %s", pageNum, gsName, ri)
	}
	return nil
}

func checkOverprint(ctx context.Context, doc *document.Document, gs ast.PdfDict, _ string, part, _ int, _ string) []Violation {
	// Overprint checks apply to parts 2+ and 4 only.
	if part < 2 {
		return nil
	}

	opmObj, hasOPM := gs["OPM"]
	if !hasOPM {
		return nil
	}

	opm, err := document.ResolveInt(ctx, doc, opmObj)
	if err != nil {
		doc.Logger.Debug("compliance.graphics: cannot resolve OPM value", "error", err)
		return nil
	}

	// OPM shall not be 1 when overprint is enabled in certain conditions.
	// Only flag when overprint stroke (OP) or overprint fill (op) is true.
	if opm != 1 {
		return nil
	}

	// When OPM is 1, check whether overprint is active. If OP or op keys
	// are not present, overprint defaults to false and OPM=1 is harmless.
	_, hasOP := gs["OP"]
	_, hasop := gs["op"]
	if !hasOP && !hasop {
		return nil
	}

	// PDF/A parts 2-4 define no standalone rule for OPM=1; the restriction is
	// expressed through colour-space-specific overprint validation, not a bare
	// OPM check. Emitting a violation here without a matching rule would be a
	// false positive, so OPM=1 with active overprint is left to those checks.

	return nil
}
