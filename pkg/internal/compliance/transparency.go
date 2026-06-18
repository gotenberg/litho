package compliance

import (
	"context"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkTransparencyGroup validates transparency restrictions required by
// PDF/A parts 1-4. PDF/UA profiles have no transparency checks.
func checkTransparencyGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part

	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.transparency: starting checks", "profile", profile, "part", part)

	// Extract raw page dicts for sub-functions that expect []ast.PdfDict.
	rawPages := make([]ast.PdfDict, len(vctx.pages))
	for i := range vctx.pages {
		rawPages[i] = vctx.pages[i].dict
	}

	var results []Violation
	if part == 1 {
		results = checkTransparencyPart1(ctx, doc, rawPages)
	} else {
		results = checkTransparencyParts2Plus(ctx, doc, catalog, rawPages, profile, part)
	}

	doc.Logger.Debug("compliance.transparency: checks complete", "violations", len(results))
	return results
}

func checkTransparencyPart1(ctx context.Context, doc *document.Document, pages []ast.PdfDict) []Violation {
	var results []Violation
	for i, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		// Check page-level transparency group.
		results = append(results, checkPageTransparencyGroup1(ctx, doc, page, i+1)...)

		visited := make(map[int]bool)

		res := resolveResources(ctx, doc, page)
		if res != nil {
			results = append(results, checkResourceTransparency1(ctx, doc, res, i+1, visited, 0)...)
		}

		// Annotation appearance streams carry their own /Resources, so an
		// ExtGState reached only inside an appearance form is still part of
		// the rendered content and falls under the PDF/A-1 §6.4 transparency
		// restrictions.
		results = append(results, checkAnnotAPTransparency1(ctx, doc, page, i+1, visited)...)
	}
	return results
}

// checkResourceTransparency1 applies the PDF/A-1 §6.4 ExtGState and XObject
// transparency checks to one resource dict and recurses into the resources of
// nested Form XObjects and tiling patterns. The §6.4 restrictions cover every
// graphics state used to render the page, so the walk follows resources through
// nested XObjects and patterns rather than stopping at the page dict. visited
// breaks resource cycles; depth is capped by [maxFormXObjectDepth] (anti-DoS
// guard shared with the device-colour scan).
func checkResourceTransparency1(ctx context.Context, doc *document.Document, res ast.PdfDict, pageNum int, visited map[int]bool, depth int) []Violation {
	if depth > maxFormXObjectDepth || ctx.Err() != nil {
		return nil
	}

	results := checkExtGStateTransparency1(ctx, doc, res, pageNum)
	results = append(results, checkXObjectTransparency1(ctx, doc, res, pageNum)...)

	results = append(results, walkNestedResources1(ctx, doc, res["XObject"], pageNum, visited, depth)...)
	results = append(results, walkNestedResources1(ctx, doc, res["Pattern"], pageNum, visited, depth)...)

	// Type 3 font resources: a CharProc is a content stream, and its
	// operators draw through the font's own /Resources. A transparency Group
	// XObject reached only via a glyph procedure is therefore still rendered
	// content and is forbidden under PDF/A-1 §6.4, so the walk descends into
	// each Type 3 font's resources.
	if fontDict, err := document.ResolveDict(ctx, doc, res["Font"]); err == nil && fontDict != nil {
		names := make([]ast.PdfName, 0, len(fontDict))
		for name := range fontDict {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			entry := fontDict[name]
			if ref, ok := entry.(ast.PdfIndirectRef); ok {
				if visited[ref.ObjectNum] {
					continue
				}
				visited[ref.ObjectNum] = true
			}
			fd, err := document.ResolveDict(ctx, doc, entry)
			if err != nil || fd == nil {
				continue
			}
			if st, _ := document.ResolveName(ctx, doc, fd["Subtype"]); st != "Type3" {
				continue
			}
			fontRes, err := document.ResolveDict(ctx, doc, fd["Resources"])
			if err != nil || fontRes == nil {
				continue
			}
			results = append(results, checkResourceTransparency1(ctx, doc, fontRes, pageNum, visited, depth+1)...)
		}
	}
	return results
}

// walkNestedResources1 recurses into the /Resources of each stream in an
// XObject or Pattern resource sub-dictionary. Names are sorted so the
// violation order is deterministic.
func walkNestedResources1(ctx context.Context, doc *document.Document, dictObj ast.PdfObject, pageNum int, visited map[int]bool, depth int) []Violation {
	dict, err := document.ResolveDict(ctx, doc, dictObj)
	if err != nil || dict == nil {
		return nil
	}

	names := make([]ast.PdfName, 0, len(dict))
	for name := range dict {
		names = append(names, name)
	}
	slices.Sort(names)

	var results []Violation
	for _, name := range names {
		entry := dict[name]
		if ref, ok := entry.(ast.PdfIndirectRef); ok {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}
		stream, err := document.ResolveStream(ctx, doc, entry)
		if err != nil {
			continue
		}
		nested, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if err != nil || nested == nil {
			continue
		}
		results = append(results, checkResourceTransparency1(ctx, doc, nested, pageNum, visited, depth+1)...)
	}
	return results
}

// checkAnnotAPTransparency1 applies the PDF/A-1 §6.4 transparency checks to
// each annotation's appearance streams: the appearance form XObject's own
// /Group and /SMask, plus the ExtGStates and nested XObjects in its
// /Resources. An appearance form is a form XObject that paints the
// annotation, so a transparency Group or /SMask on it is rendered content and
// falls under §6.4. visited dedups appearance forms shared across annotations
// so a forbidden Group fires once per form, not once per referencing
// annotation.
func checkAnnotAPTransparency1(ctx context.Context, doc *document.Document, page ast.PdfDict, pageNum int, visited map[int]bool) []Violation {
	annots, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil || len(annots) == 0 {
		return nil
	}

	var results []Violation
	for _, annotObj := range annots {
		if ctx.Err() != nil {
			return results
		}
		annotDict, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annotDict == nil {
			continue
		}
		apDict, err := document.ResolveDict(ctx, doc, annotDict["AP"])
		if err != nil || apDict == nil {
			continue
		}
		for _, apName := range sortedNames(apDict) {
			results = append(results, appearanceTransparency1(ctx, doc, apDict[apName], pageNum, visited)...)
		}
	}
	return results
}

// appearanceTransparency1 checks one /AP entry, either an appearance form
// XObject stream or a sub-dictionary of appearance states (e.g. a button's
// /On and /Off), for forbidden PDF/A-1 transparency.
func appearanceTransparency1(ctx context.Context, doc *document.Document, entry ast.PdfObject, pageNum int, visited map[int]bool) []Violation {
	if stream, err := document.ResolveStream(ctx, doc, entry); err == nil {
		return appearanceStreamTransparency1(ctx, doc, entry, stream, pageNum, visited)
	}
	subDict, err := document.ResolveDict(ctx, doc, entry)
	if err != nil || subDict == nil {
		return nil
	}
	var results []Violation
	for _, name := range sortedNames(subDict) {
		sub := subDict[name]
		if stream, err := document.ResolveStream(ctx, doc, sub); err == nil {
			results = append(results, appearanceStreamTransparency1(ctx, doc, sub, stream, pageNum, visited)...)
		}
	}
	return results
}

// appearanceStreamTransparency1 checks an appearance form XObject's own
// transparency keys and recurses into its /Resources, deduping shared
// appearance forms through visited.
func appearanceStreamTransparency1(ctx context.Context, doc *document.Document, entry ast.PdfObject, stream ast.PdfStream, pageNum int, visited map[int]bool) []Violation {
	if ref, ok := entry.(ast.PdfIndirectRef); ok {
		if visited[ref.ObjectNum] {
			return nil
		}
		visited[ref.ObjectNum] = true
	}
	results := formXObjectTransparency1(ctx, doc, stream.Dict, pageNum, "annotation appearance")
	if nested, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && nested != nil {
		results = append(results, checkResourceTransparency1(ctx, doc, nested, pageNum, visited, 0)...)
	}
	return results
}

// sortedNames returns the keys of a dict in deterministic order.
func sortedNames(d ast.PdfDict) []ast.PdfName {
	names := make([]ast.PdfName, 0, len(d))
	for name := range d {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// checkPageTransparencyGroup1 checks for forbidden transparency Group on a page (PDF/A-1).
func checkPageTransparencyGroup1(ctx context.Context, doc *document.Document, page ast.PdfDict, pageNum int) []Violation {
	groupObj, hasGroup := page["Group"]
	if !hasGroup {
		return nil
	}

	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return nil
	}

	sName, _ := document.ResolveName(ctx, doc, group["S"])
	if sName == "Transparency" {
		return failf(Rule_19005_1_6_4_t03, "page %d", pageNum)
	}
	return nil
}

// checkExtGStateTransparency1 checks all ExtGState entries for transparency
// (SMask, BM, CA, ca) which are forbidden in PDF/A-1.
func checkExtGStateTransparency1(ctx context.Context, doc *document.Document, resources ast.PdfDict, pageNum int) []Violation {
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

		gs, err := document.ResolveDict(ctx, doc, gsObj)
		if err != nil || gs == nil {
			continue
		}

		// SMask must be None or absent.
		if smaskObj, hasSMask := gs["SMask"]; hasSMask {
			smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
			if smaskName != "None" {
				results = append(results, failf(Rule_19005_1_6_4_t01, "page %d, ExtGState %s", pageNum, string(gsName))...)
			}
		}

		// BM must be Normal or Compatible.
		if bmObj, hasBM := gs["BM"]; hasBM {
			bm := resolveBlendMode(ctx, doc, bmObj)
			if bm != "" && bm != "Normal" && bm != "Compatible" {
				results = append(results, failf(Rule_19005_1_6_4_t04, "page %d, ExtGState %s, BM=%s", pageNum, string(gsName), bm)...)
			}
		}

		// CA (stroke alpha) must be 1.0.
		if caObj, hasCA := gs["CA"]; hasCA {
			ca := document.Float(caObj)
			if !floatEquals1(ca) {
				results = append(results, failf(Rule_19005_1_6_4_t05, "page %d, ExtGState %s, CA=%g", pageNum, string(gsName), ca)...)
			}
		}

		// ca (fill alpha) must be 1.0.
		if caObj, hasCa := gs["ca"]; hasCa {
			ca := document.Float(caObj)
			if !floatEquals1(ca) {
				results = append(results, failf(Rule_19005_1_6_4_t06, "page %d, ExtGState %s, ca=%g", pageNum, string(gsName), ca)...)
			}
		}
	}
	return results
}

// checkXObjectTransparency1 checks XObject entries for forbidden SMask (PDF/A-1).
func checkXObjectTransparency1(ctx context.Context, doc *document.Document, resources ast.PdfDict, pageNum int) []Violation {
	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return nil
	}

	var results []Violation
	for _, xoName := range sortedNames(xobjDict) {
		xoObj := xobjDict[xoName]
		if ctx.Err() != nil {
			return results
		}

		// XObjects are streams: ResolveDict rejects ast.PdfStream, which
		// left this check dead for every real Form/Image XObject. Resolve
		// the stream and use its dict; keep the dict fallback for
		// malformed producers.
		var xo ast.PdfDict
		if stream, err := document.ResolveStream(ctx, doc, xoObj); err == nil {
			xo = stream.Dict
		} else if d, derr := document.ResolveDict(ctx, doc, xoObj); derr == nil {
			xo = d
		}
		if xo == nil {
			continue
		}
		results = append(results, formXObjectTransparency1(ctx, doc, xo, pageNum, "XObject "+string(xoName))...)
	}
	return results
}

// formXObjectTransparency1 fires the PDF/A-1 §6.4 SMask (t2) and
// transparency-Group (t3) checks for a single form XObject dictionary.
// label identifies the XObject in the violation context.
func formXObjectTransparency1(ctx context.Context, doc *document.Document, xo ast.PdfDict, pageNum int, label string) []Violation {
	var results []Violation

	// SMask key must not be present on XObjects.
	if _, hasSMask := xo["SMask"]; hasSMask {
		results = append(results, failf(Rule_19005_1_6_4_t02, "page %d, %s", pageNum, label)...)
	}

	// Transparency Group is forbidden on form XObjects.
	if groupObj, hasGroup := xo["Group"]; hasGroup {
		group, err := document.ResolveDict(ctx, doc, groupObj)
		if err == nil && group != nil {
			if sName, _ := document.ResolveName(ctx, doc, group["S"]); sName == "Transparency" {
				results = append(results, failf(Rule_19005_1_6_4_t03, "page %d, %s", pageNum, label)...)
			}
		}
	}
	return results
}

// standardBlendModes lists all blend modes defined in ISO 32000-1:2008.
var standardBlendModes = map[string]bool{ //nolint:gochecknoglobals // constant lookup table.
	"Normal":     true,
	"Compatible": true,
	"Multiply":   true,
	"Screen":     true,
	"Overlay":    true,
	"Darken":     true,
	"Lighten":    true,
	"ColorDodge": true,
	"ColorBurn":  true,
	"HardLight":  true,
	"SoftLight":  true,
	"Difference": true,
	"Exclusion":  true,
	"Hue":        true,
	"Saturation": true,
	"Color":      true,
	"Luminosity": true,
}

// blendModeRule returns the rule for invalid blend mode, varying by part.
func blendModeRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_10_t01, Rule_19005_3_6_2_10_t01, Rule_19005_4_6_2_9_t01)
}

// blendingCSRule returns the rule for missing blending color space on
// transparent pages.
func blendingCSRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_10_t02, Rule_19005_3_6_2_10_t02, Rule_19005_4_6_2_9_t02)
}

func checkTransparencyParts2Plus(ctx context.Context, doc *document.Document, catalog ast.PdfDict, pages []ast.PdfDict, _ string, part int) []Violation {
	hasOutputIntent, oiColorSpace := documentOutputIntentInfo(ctx, doc, catalog)

	var results []Violation
	for i, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		res := resolveResources(ctx, doc, page)
		if res == nil {
			continue
		}

		pageUsesTransparency := false

		// A page with its own transparency Group is itself using transparency.
		if groupObj, hasGroup := page["Group"]; hasGroup {
			group, gErr := document.ResolveDict(ctx, doc, groupObj)
			if gErr == nil && group != nil {
				s, _ := document.ResolveName(ctx, doc, group["S"])
				if s == "Transparency" {
					pageUsesTransparency = true
				}
			}
		}

		// Check blend modes in ExtGState.
		gsDict, err := document.ResolveDict(ctx, doc, res["ExtGState"])
		if err == nil && gsDict != nil {
			for _, gsName := range sortedNames(gsDict) {
				gsObj := gsDict[gsName]
				if ctx.Err() != nil {
					return results
				}

				gs, err := document.ResolveDict(ctx, doc, gsObj)
				if err != nil || gs == nil {
					continue
				}

				// Validate blend mode.
				if bmObj, hasBM := gs["BM"]; hasBM {
					bm := resolveBlendMode(ctx, doc, bmObj)
					if bm != "" && !standardBlendModes[bm] {
						results = append(results, failf(blendModeRule(part), "page %d, ExtGState %s, BM=%s", i+1, string(gsName), bm)...)
					}
					if bm != "" && bm != "Normal" && bm != "Compatible" {
						pageUsesTransparency = true
					}
				}

				// SMask (other than None) indicates transparency.
				if smaskObj, hasSMask := gs["SMask"]; hasSMask {
					smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
					if smaskName != "None" {
						pageUsesTransparency = true
					}
				}

				// Non-1.0 alpha values indicate transparency.
				if caObj, hasCA := gs["CA"]; hasCA {
					if !floatEquals1(document.Float(caObj)) {
						pageUsesTransparency = true
					}
				}
				if caObj, hasCa := gs["ca"]; hasCa {
					if !floatEquals1(document.Float(caObj)) {
						pageUsesTransparency = true
					}
				}
			}
		}

		// Check Image XObjects for /SMask (soft mask images).
		if !pageUsesTransparency {
			xobjDict, err := document.ResolveDict(ctx, doc, res["XObject"])
			if err == nil && xobjDict != nil {
				for _, xobjObj := range xobjDict {
					if pageUsesTransparency {
						break
					}
					stream, err := document.ResolveStream(ctx, doc, xobjObj)
					if err != nil {
						doc.Logger.Debug("compliance.transparency: cannot resolve XObject stream", "error", err)
						continue
					}
					subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
					switch string(subtype) {
					case "Image":
						// Image with /SMask triggers transparency.
						if _, hasSMask := stream.Dict["SMask"]; hasSMask {
							pageUsesTransparency = true
						}
					case "Form":
						// Form XObject with transparency group triggers transparency.
						groupObj, hasGroup := stream.Dict["Group"]
						if hasGroup {
							group, err := document.ResolveDict(ctx, doc, groupObj)
							if err == nil && group != nil {
								s, _ := document.ResolveName(ctx, doc, group["S"])
								if s == "Transparency" {
									pageUsesTransparency = true
								}
							}
						}
					}
				}
			}
		}

		// Check tiling patterns for transparency triggers.
		if !pageUsesTransparency {
			pageUsesTransparency = checkPatternTransparency(ctx, doc, res)
		}

		// Check Type 3 fonts for transparency triggers.
		// Type 3 font CharProcs may use ExtGState with transparency or
		// reference Form XObjects with transparency groups.
		if !pageUsesTransparency {
			pageUsesTransparency = checkType3FontTransparency(ctx, doc, res)
		}

		// Also check annotations for transparency triggers.
		if !pageUsesTransparency {
			pageUsesTransparency = checkAnnotationTransparency(ctx, doc, page)
		}

		// PDF/A-4: page-level OutputIntents supplement catalog-level.
		pageHasOI := false
		pageOICS := ""
		if part == 4 {
			pageHasOI, pageOICS = checkPageOutputIntent(ctx, doc, page)
		}
		effectiveHasOI := hasOutputIntent || pageHasOI
		effectiveOICS := oiColorSpace
		if pageHasOI && pageOICS != "" {
			effectiveOICS = pageOICS
		}

		// If the page uses transparency, it must have a blending colour space.
		// The OutputIntent ICC profile provides a default blending CS for the
		// entire document, so pages without an explicit /Group /CS are valid
		// when an OutputIntent exists.
		if pageUsesTransparency && !effectiveHasOI {
			if !pageHasBlendingCS(ctx, doc, page) {
				results = append(results, failf(blendingCSRule(part), "page %d", i+1)...)
			}
		}

		// Even when an OutputIntent exists, device CS used as blending CS
		// must be compatible with the OutputIntent's ICC color space.
		if pageUsesTransparency && effectiveHasOI && effectiveOICS != "" {
			results = append(results, checkBlendingCSCompatibility(ctx, doc, page, res, effectiveOICS, i+1, part)...)
		}
	}
	return results
}

// checkAnnotationTransparency checks whether any annotation on the page
// uses transparency (via appearance streams with transparency groups or
// ExtGState with transparency settings).
func checkAnnotationTransparency(ctx context.Context, doc *document.Document, page ast.PdfDict) bool {
	annotsArr, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil || len(annotsArr) == 0 {
		return false
	}
	for _, annotObj := range annotsArr {
		if ctx.Err() != nil {
			return false
		}
		annot, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annot == nil {
			continue
		}
		// Check /BM (blend mode) directly on the annotation dict.
		// PDF annotations can carry their own blend mode (ISO 32000-2 Table 165).
		if bmObj, hasBM := annot["BM"]; hasBM {
			bm := resolveBlendMode(ctx, doc, bmObj)
			if bm != "" && bm != "Normal" && bm != "Compatible" {
				return true
			}
		}
		apDict, err := document.ResolveDict(ctx, doc, annot["AP"])
		if err != nil || apDict == nil {
			continue
		}
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apObj, ok := apDict[key]
			if !ok {
				continue
			}
			stream, err := document.ResolveStream(ctx, doc, apObj)
			if err != nil {
				doc.Logger.Debug("compliance.transparency: cannot resolve appearance stream", "error", err)
				continue
			}
			// Check for transparency group on the appearance Form XObject.
			groupObj, hasGroup := stream.Dict["Group"]
			if hasGroup {
				group, err := document.ResolveDict(ctx, doc, groupObj)
				if err == nil && group != nil {
					s, _ := document.ResolveName(ctx, doc, group["S"])
					if s == "Transparency" {
						return true
					}
				}
			}
			// Check ExtGState in appearance resources.
			apRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
			if err == nil && apRes != nil {
				if resourcesHaveTransparency(ctx, doc, apRes) {
					return true
				}
			}
		}
	}
	return false
}

// resourcesHaveTransparency checks if a resources dict contains ExtGState
// entries with transparency settings (non-Normal BM, SMask, CA/ca < 1.0).
func resourcesHaveTransparency(ctx context.Context, doc *document.Document, resources ast.PdfDict) bool {
	gsDict, err := document.ResolveDict(ctx, doc, resources["ExtGState"])
	if err != nil || gsDict == nil {
		return false
	}
	for _, gsObj := range gsDict {
		gs, err := document.ResolveDict(ctx, doc, gsObj)
		if err != nil || gs == nil {
			continue
		}
		if bmObj, hasBM := gs["BM"]; hasBM {
			bm := resolveBlendMode(ctx, doc, bmObj)
			if bm != "" && bm != "Normal" && bm != "Compatible" {
				return true
			}
		}
		if smaskObj, hasSMask := gs["SMask"]; hasSMask {
			smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
			if smaskName != "None" {
				return true
			}
		}
		if caObj, hasCA := gs["CA"]; hasCA {
			if !floatEquals1(document.Float(caObj)) {
				return true
			}
		}
		if caObj, hasCa := gs["ca"]; hasCa {
			if !floatEquals1(document.Float(caObj)) {
				return true
			}
		}
	}
	return false
}

// checkBlendingCSCompatibility validates that any device CS used as a blending
// colour space on the page (or in XObjects, annotations, Type 3 fonts) is
// compatible with the OutputIntent's ICC colour space.
func checkBlendingCSCompatibility(ctx context.Context, doc *document.Document, page ast.PdfDict, resources ast.PdfDict, oiColorSpace string, pageNum int, part int) []Violation {
	var results []Violation

	// Check page-level /Group/CS.
	pageBlendCS := resolveBlendingDeviceCS(ctx, doc, page)
	if pageBlendCS != "" && !blendingCSMatchesOutputIntent(pageBlendCS, oiColorSpace) {
		results = append(results, failf(blendingCSRule(part),
			"page %d: blending CS %s incompatible with OutputIntent %s", pageNum, pageBlendCS, oiColorSpace)...)
	}

	// Check Form XObject /Group/CS entries.
	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err == nil && xobjDict != nil {
		for _, xobjObj := range xobjDict {
			stream, err := document.ResolveStream(ctx, doc, xobjObj)
			if err != nil {
				doc.Logger.Debug("compliance.transparency: cannot resolve XObject stream", "error", err)
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
			if subtype != "Form" {
				continue
			}
			xoCS := resolveBlendingDeviceCS(ctx, doc, stream.Dict)
			if xoCS != "" && !blendingCSMatchesOutputIntent(xoCS, oiColorSpace) {
				results = append(results, failf(blendingCSRule(part),
					"page %d: Form XObject blending CS %s incompatible with OutputIntent %s", pageNum, xoCS, oiColorSpace)...)
			}
		}
	}

	// Check annotation appearance /Group/CS entries.
	annotsArr, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err == nil {
		for _, annotObj := range annotsArr {
			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			apDict, err := document.ResolveDict(ctx, doc, annot["AP"])
			if err != nil || apDict == nil {
				continue
			}
			for _, key := range []ast.PdfName{"N", "R", "D"} {
				apObj, ok := apDict[key]
				if !ok {
					continue
				}
				stream, err := document.ResolveStream(ctx, doc, apObj)
				if err != nil {
					doc.Logger.Debug("compliance.transparency: cannot resolve appearance stream", "error", err)
					continue
				}
				apCS := resolveBlendingDeviceCS(ctx, doc, stream.Dict)
				if apCS != "" && !blendingCSMatchesOutputIntent(apCS, oiColorSpace) {
					results = append(results, failf(blendingCSRule(part),
						"page %d: annotation blending CS %s incompatible with OutputIntent %s", pageNum, apCS, oiColorSpace)...)
				}
			}
		}
	}

	return results
}

// resolveBlendingDeviceCS extracts the device CS name from a dict's
// /Group << /S /Transparency /CS <cs> >>. Returns "DeviceRGB", "DeviceCMYK",
// "DeviceGray", or "" if not a device CS or not present.
func resolveBlendingDeviceCS(ctx context.Context, doc *document.Document, dict ast.PdfDict) string {
	groupObj, ok := dict["Group"]
	if !ok {
		return ""
	}
	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return ""
	}
	s, _ := document.ResolveName(ctx, doc, group["S"])
	if s != "Transparency" {
		return ""
	}
	csObj, ok := group["CS"]
	if !ok {
		return ""
	}
	csName, _ := document.ResolveName(ctx, doc, csObj)
	cs := string(csName)
	if cs == "DeviceRGB" || cs == "DeviceCMYK" || cs == "DeviceGray" {
		return cs
	}
	return ""
}

// blendingCSMatchesOutputIntent checks if a device blending CS is compatible
// with the OutputIntent's ICC colour space.
func blendingCSMatchesOutputIntent(deviceCS string, oiColorSpace string) bool {
	switch deviceCS {
	case "DeviceRGB":
		return oiColorSpace == "RGB "
	case "DeviceCMYK":
		return oiColorSpace == "CMYK"
	case "DeviceGray":
		return true // DeviceGray is compatible with any OutputIntent.
	}
	return true
}

// documentOutputIntentInfo returns whether the document has an OutputIntent
// with a DestOutputProfile stream, and the ICC colour space of that profile.
func documentOutputIntentInfo(ctx context.Context, doc *document.Document, catalog ast.PdfDict) (bool, string) {
	oiObj, hasOI := catalog["OutputIntents"]
	if !hasOI {
		return false, ""
	}
	arr, err := document.ResolveArray(ctx, doc, oiObj)
	if err != nil || len(arr) == 0 {
		return false, ""
	}
	for _, intentObj := range arr {
		intent, err := document.ResolveDict(ctx, doc, intentObj)
		if err != nil || intent == nil {
			continue
		}
		// Only PDF/A OutputIntents provide coverage. The subtype is
		// GTS_PDFA1 for every PDF/A part (ISO 32000-2 Table 401);
		// "GTS_PDFA" does not exist.
		s, _ := document.ResolveName(ctx, doc, intent["S"])
		if s != "GTS_PDFA1" {
			continue
		}
		destObj, ok := intent["DestOutputProfile"]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, destObj)
		if err != nil {
			doc.Logger.Debug("compliance.transparency: cannot resolve output profile stream", "error", err)
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) < 20 {
			return true, "" // Has OutputIntent but can't read color space
		}
		cs := string(data[16:20])
		return true, cs
	}
	return false, ""
}

// checkType3FontTransparency checks whether any Type 3 font in the page
// resources uses transparency (via ExtGState or transparency group XObjects
// in its own resources).
// checkPatternTransparency checks whether any tiling pattern in the page
// resources uses transparency (via ExtGState with alpha or blend mode).
func checkPatternTransparency(ctx context.Context, doc *document.Document, pageRes ast.PdfDict) bool {
	patDict, err := document.ResolveDict(ctx, doc, pageRes["Pattern"])
	if err != nil || patDict == nil {
		return false
	}
	for _, patObj := range patDict {
		stream, err := document.ResolveStream(ctx, doc, patObj)
		if err != nil {
			doc.Logger.Debug("compliance.transparency: cannot resolve pattern stream", "error", err)
			continue
		}
		patRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if err != nil || patRes == nil {
			continue
		}
		gsDict, err := document.ResolveDict(ctx, doc, patRes["ExtGState"])
		if err != nil || gsDict == nil {
			continue
		}
		for _, gsObj := range gsDict {
			gs, err := document.ResolveDict(ctx, doc, gsObj)
			if err != nil || gs == nil {
				continue
			}
			if bmObj, hasBM := gs["BM"]; hasBM {
				bm := resolveBlendMode(ctx, doc, bmObj)
				if bm != "" && bm != "Normal" && bm != "Compatible" {
					return true
				}
			}
			if smaskObj, hasSMask := gs["SMask"]; hasSMask {
				smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
				if smaskName != "None" {
					return true
				}
			}
			if caObj, hasCA := gs["CA"]; hasCA {
				if !floatEquals1(document.Float(caObj)) {
					return true
				}
			}
			if caObj, hasCa := gs["ca"]; hasCa {
				if !floatEquals1(document.Float(caObj)) {
					return true
				}
			}
		}
	}
	return false
}

func checkType3FontTransparency(ctx context.Context, doc *document.Document, pageRes ast.PdfDict) bool {
	fontDict, err := document.ResolveDict(ctx, doc, pageRes["Font"])
	if err != nil || fontDict == nil {
		return false
	}
	for _, fontObj := range fontDict {
		fd, err := document.ResolveDict(ctx, doc, fontObj)
		if err != nil || fd == nil {
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"])
		if subtype != "Type3" {
			continue
		}
		fontRes, err := document.ResolveDict(ctx, doc, fd["Resources"])
		if err != nil || fontRes == nil {
			continue
		}
		// Check Type 3 font's ExtGState for transparency.
		gsDict, err := document.ResolveDict(ctx, doc, fontRes["ExtGState"])
		if err == nil && gsDict != nil {
			for _, gsObj := range gsDict {
				gs, err := document.ResolveDict(ctx, doc, gsObj)
				if err != nil || gs == nil {
					continue
				}
				if bmObj, hasBM := gs["BM"]; hasBM {
					bm := resolveBlendMode(ctx, doc, bmObj)
					if bm != "" && bm != "Normal" && bm != "Compatible" {
						return true
					}
				}
				if smaskObj, hasSMask := gs["SMask"]; hasSMask {
					smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
					if smaskName != "None" {
						return true
					}
				}
				if caObj, hasCA := gs["CA"]; hasCA {
					if !floatEquals1(document.Float(caObj)) {
						return true
					}
				}
				if caObj, hasCa := gs["ca"]; hasCa {
					if !floatEquals1(document.Float(caObj)) {
						return true
					}
				}
			}
		}
		// Check Type 3 font's XObjects for transparency groups.
		xobjDict, err := document.ResolveDict(ctx, doc, fontRes["XObject"])
		if err == nil && xobjDict != nil {
			for _, xobjObj := range xobjDict {
				stream, err := document.ResolveStream(ctx, doc, xobjObj)
				if err != nil {
					doc.Logger.Debug("compliance.transparency: cannot resolve XObject stream", "error", err)
					continue
				}
				st, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
				if st == "Form" {
					groupObj, hasGroup := stream.Dict["Group"]
					if hasGroup {
						group, err := document.ResolveDict(ctx, doc, groupObj)
						if err == nil && group != nil {
							s, _ := document.ResolveName(ctx, doc, group["S"])
							if s == "Transparency" {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

// resolveBlendMode resolves a blend mode value which may be a name or the
// first element of an array of names.
func resolveBlendMode(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	// BM can be a single name.
	if name, _ := document.ResolveName(ctx, doc, obj); name != "" {
		return string(name)
	}
	// Or an array of names. Use the first entry.
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) == 0 {
		return ""
	}
	name, _ := document.ResolveName(ctx, doc, arr[0])
	return string(name)
}

// floatEquals1 tests whether a float64 is approximately 1.0.
const alphaEpsilon = 1e-6

func floatEquals1(v float64) bool {
	return math.Abs(v-1.0) < alphaEpsilon
}

// pageHasBlendingCS checks whether a page dictionary has a Group dict with
// /S = Transparency and a /CS entry.
func pageHasBlendingCS(ctx context.Context, doc *document.Document, page ast.PdfDict) bool {
	groupObj, hasGroup := page["Group"]
	if !hasGroup {
		return false
	}

	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return false
	}

	sName, _ := document.ResolveName(ctx, doc, group["S"])
	if sName != "Transparency" {
		return false
	}

	_, hasCS := group["CS"]
	return hasCS
}
