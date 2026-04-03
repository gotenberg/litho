package compliance

import (
	"context"
	"log/slog"

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

	slog.Debug("compliance.graphics: starting checks", "profile", profile, "part", part)

	var results []Violation
	for i := range vctx.pages {
		p := &vctx.pages[i]
		page := p.dict
		if ctx.Err() != nil {
			return results
		}

		res := p.resources
		hasContent := len(p.content) > 0

		// §6.2.2: check content stream resource completeness even when
		// the page has no Resources (the absence itself may be a violation).
		if hasContent {
			ops := p.parsedOps(ctx)
			_, hasOwnRes := page["Resources"]
			if !hasOwnRes && ops != nil && opsReferenceResources(ops) {
				results = append(results, failf(missingResourceRule(part),
					"page %d: content stream references resources but page has no explicitly associated Resources dictionary", i+1)...)
			}
			if res != nil && ops != nil {
				// Content stream resource completeness.
				results = append(results, checkContentStreamResourcesOps(ctx, doc, ops, res, part, i+1)...)
				// Check inline image /Intent values from parsed BI operations.
				results = append(results, checkInlineImageIntent(ops, part, i+1)...)
			}
		}

		if res == nil {
			continue
		}

		results = append(results, checkExtGStateGraphics(ctx, doc, res, profile, part, i+1)...)

		if hasContent {
			if part >= 2 {
				results = append(results, checkContentStreamStateOps(ctx, doc, p.parsedOps(ctx), res, part, i+1)...)
			}
		}

		// Also check Form XObject content streams for resource completeness.
		// Only scan Forms referenced by Do operators in the page content.
		var referencedXObjects map[string]bool
		if hasContent {
			_, referencedXObjects = scanContentStreamDeviceCS(ctx, doc, page, i)
		}
		results = append(results, checkFormXObjectResources(ctx, doc, res, part, i+1, referencedXObjects)...)

		// Scan Form XObject ExtGState for halftone/rendering intent violations.
		results = append(results, checkFormXObjectExtGState(ctx, doc, res, profile, part, i+1)...)

		// Check Type3 font CharProc resource completeness.
		results = append(results, checkType3CharProcResources(ctx, doc, res, part, i+1)...)

		// Scan annotation appearance ExtGState for halftone/rendering intent.
		results = append(results, checkAnnotationAppearanceExtGState(ctx, doc, page, profile, part, i+1)...)
	}

	slog.Debug("compliance.graphics: checks complete", "violations", len(results))
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
	for xoName, xoObj := range xoDict {
		// Only check Form XObjects that are actually referenced by Do operators.
		if referencedNames != nil && !referencedNames[string(xoName)] {
			continue
		}
		if ctx.Err() != nil {
			return results
		}
		stream, err := document.ResolveStream(ctx, doc, xoObj)
		if err != nil {
			slog.Debug("compliance.graphics: cannot resolve XObject stream", "error", err)
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
		results = append(results, checkContentStreamResources(ctx, doc, formData, formRes, part, pageNum)...)
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
	for fontName, fontRef := range fontDict {
		fd, err := document.ResolveDict(ctx, doc, fontRef)
		if err != nil || fd == nil {
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"])
		if subtype != "Type3" {
			continue
		}
		// CharProc content streams may reference named resources.
		_, hasOwnRes := fd["Resources"]
		charProcs, err := document.ResolveDict(ctx, doc, fd["CharProcs"])
		if err != nil || charProcs == nil {
			continue
		}
		for _, cpObj := range charProcs {
			stream, err := document.ResolveStream(ctx, doc, cpObj)
			if err != nil {
				slog.Debug("compliance.graphics: cannot resolve CharProc stream", "error", err)
				continue
			}
			data, err := doc.DecodeStream(ctx, stream)
			if err != nil || len(data) == 0 {
				continue
			}
			if !hasOwnRes && contentReferencesResources(ctx, data) {
				results = append(results, failf(missingResourceRule(part),
					"page %d: Type3 font %s CharProc references resources without explicit Resources dictionary",
					pageNum, fontName)...)
				return results
			}
			// Also check that referenced resources exist in the Type3 Resources dict.
			if hasOwnRes {
				type3Res, _ := document.ResolveDict(ctx, doc, fd["Resources"])
				if type3Res != nil {
					results = append(results, checkContentStreamResources(ctx, doc, data, type3Res, part, pageNum)...)
				}
			}
		}
	}
	return results
}

// checkAnnotationAppearanceExtGState scans annotation appearance stream
// resources for ExtGState entries (rendering intent, halftone, etc.).
func checkAnnotationAppearanceExtGState(ctx context.Context, doc *document.Document, page ast.PdfDict, profile string, part, pageNum int) []Violation {
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
				slog.Debug("compliance.graphics: cannot resolve appearance stream", "error", err)
				continue
			}
			apRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
			if apRes != nil {
				results = append(results, checkExtGStateGraphics(ctx, doc, apRes, profile, part, pageNum)...)
			}
		}
	}
	return results
}

// opsReferenceResources checks if parsed operations contain any operators
// that reference named resources (Tf, cs/CS, Do, gs, sh, BDC/DP).
func opsReferenceResources(ops []content.Operation) bool {
	for _, op := range ops {
		if _, ok := operatorResourceCategory[op.Operator]; ok && len(op.Operands) >= 1 {
			if _, isName := op.Operands[0].(ast.PdfName); isName {
				return true
			}
		}
	}
	return false
}

// contentReferencesResources parses a raw content stream and checks if it has
// any operators that reference named resources.
func contentReferencesResources(ctx context.Context, streamData []byte) bool {
	ops, err := content.ParseStreamData(ctx, streamData)
	if err != nil {
		return false
	}
	return opsReferenceResources(ops)
}

// checkFormXObjectExtGState scans Form XObject resources for ExtGState
// entries (halftone, rendering intent, etc.) that the page-level scan misses.
func checkFormXObjectExtGState(ctx context.Context, doc *document.Document, pageRes ast.PdfDict, profile string, part, pageNum int) []Violation {
	if pageRes == nil {
		return nil
	}
	xoDict, err := document.ResolveDict(ctx, doc, pageRes["XObject"])
	if err != nil || xoDict == nil {
		return nil
	}
	var results []Violation
	for _, xoObj := range xoDict {
		stream, err := document.ResolveStream(ctx, doc, xoObj)
		if err != nil {
			slog.Debug("compliance.graphics: cannot resolve XObject stream", "error", err)
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
		results = append(results, checkExtGStateGraphics(ctx, doc, formRes, profile, part, pageNum)...)
	}
	return results
}

// checkExtGStateGraphics validates all ExtGState entries in a page's resources.
func checkExtGStateGraphics(ctx context.Context, doc *document.Document, resources ast.PdfDict, profile string, part, pageNum int) []Violation {
	gsDict, err := document.ResolveDict(ctx, doc, resources["ExtGState"])
	if err != nil || gsDict == nil {
		return nil
	}

	var results []Violation
	for gsName, gsObj := range gsDict {
		if ctx.Err() != nil {
			return results
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

// --- Transfer function checks (§6.2.5) ---

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
	// PDF/A-2+/4: /TR key is also forbidden.
	if _, hasTR := gs["TR"]; hasTR {
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

// --- HTP key check (§6.2.5) ---

// htpForbiddenRule returns the rule for HTP key being forbidden.
func htpForbiddenRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_5_t03, Rule_19005_3_6_2_5_t03, Rule_19005_4_6_2_5_t03)
}

func checkHTPKey(_ context.Context, _ *document.Document, gs ast.PdfDict, _ string, part, pageNum int, gsName string) []Violation {
	// HTP check applies to parts 2+ and 4 only.
	if part < 2 {
		return nil
	}

	if _, hasHTP := gs["HTP"]; hasHTP {
		return failf(htpForbiddenRule(part), "page %d, ExtGState %s", pageNum, gsName)
	}
	// HTO key is also forbidden (PDF/A-4 §6.2.5 t3).
	if _, hasHTO := gs["HTO"]; hasHTO {
		return failf(htpForbiddenRule(part), "page %d, ExtGState %s: HTO key forbidden", pageNum, gsName)
	}
	return nil
}

// --- Halftone checks (§6.2.5) ---

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

	// HalftoneName forbidden.
	if _, hasHTName := ht["HalftoneName"]; hasHTName {
		results = append(results, failf(halftoneNameRule(part), "page %d, ExtGState %s", pageNum, gsName)...)
	}

	// TransferFunction must be /Default or absent.
	if tfObj, hasTF := ht["TransferFunction"]; hasTF {
		tfName, _ := document.ResolveName(ctx, doc, tfObj)
		if tfName != "Default" {
			results = append(results, failf(halftoneTransferRule(part), "page %d, ExtGState %s", pageNum, gsName)...)
		}
	}

	// For Type 5 composite halftones (parts 2+): validate TransferFunction
	// on sub-halftone dicts per the VeraPDF rule:
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
		for key, subObj := range ht {
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

// --- Rendering intent check (§6.2.6) ---

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

// --- Overprint mode check ---

func checkOverprint(ctx context.Context, doc *document.Document, gs ast.PdfDict, _ string, part, pageNum int, gsName string) []Violation {
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
		slog.Debug("compliance.graphics: cannot resolve OPM value", "error", err)
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

	// There is no dedicated VeraPDF rule variable for OPM alone in parts 2-4.
	// This is typically caught as part of color-space-specific overprint
	// validation. Log the condition for debugging but do not emit a violation
	// without a matching rule, to avoid false positives.

	return nil
}
