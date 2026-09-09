package compliance

import (
	"context"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
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

		res := resolveInheritedResources(ctx, doc, page)
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

// sortedByteKeys returns the keys of a byte-keyed set in ascending order, so a
// check that reports the first matching character code is deterministic.
func sortedByteKeys(m map[byte]bool) []byte {
	keys := make([]byte, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
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
	hasOutputIntent, _ := documentOutputIntentInfo(ctx, doc, catalog)

	var results []Violation
	for i, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		res := resolveInheritedResources(ctx, doc, page)
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

		// Check XObjects for transparency, recursing into Form XObjects whose
		// own resources may nest soft-mask images, blend modes, or further
		// forms. veraPDF walks the full resource tree, so a page that carries
		// its transparency only inside a Form XObject still requires a blending
		// colour space (ISO 19005-2 §6.2.10 t2).
		if !pageUsesTransparency {
			pageUsesTransparency = resourcesUseTransparency(ctx, doc, res, 0, map[string]bool{},
				pageDrawnResources(ctx, doc, page))
		}

		// Check tiling patterns for transparency triggers.
		if !pageUsesTransparency {
			pageUsesTransparency = checkPatternTransparency(ctx, doc, res)
		}

		// Check Type 3 fonts for transparency triggers.
		// Type 3 font CharProcs may use ExtGState with transparency or
		// reference Form XObjects with transparency groups.
		if !pageUsesTransparency {
			pageUsesTransparency = checkType3FontTransparency(ctx, doc, page, res)
		}

		// Also check annotations for transparency triggers.
		if !pageUsesTransparency {
			pageUsesTransparency = checkAnnotationTransparency(ctx, doc, page)
		}

		// PDF/A-4: page-level OutputIntents supplement catalog-level.
		pageHasOI := false
		if part == 4 {
			pageHasOI, _ = checkPageOutputIntent(ctx, doc, page)
		}
		effectiveHasOI := hasOutputIntent || pageHasOI

		// A page that uses transparency must have a blending colour space: an
		// explicit /Group /CS, or the document/page OutputIntent ICC profile as a
		// default. veraPDF scopes §6.2.9 t2 / §6.2.10 t2 to that existence check
		// (gOutputCS != null || containsGroupCS == true || containsTransparency ==
		// false). It does NOT require a device /Group /CS to match the OutputIntent's
		// colour space: a DeviceRGB blending CS under a CMYK OutputIntent is reported
		// by §6.2.4.3, not here.
		if pageUsesTransparency && !effectiveHasOI && !pageHasBlendingCS(ctx, doc, page) {
			results = append(results, failf(blendingCSRule(part), "page %d", i+1)...)
		}
	}
	return results
}

// drawnResources records the resource names a content stream invokes: the
// XObjects it paints with Do and the graphics states it sets with gs.
//
// A resource dictionary lists what a page MAY reach, not what it draws.
// ISO 19005-2, 6.2.10 and ISO 19005-4, 6.2.9 ask whether the page "contains
// transparency", which veraPDF answers from the content stream: its
// PDPage.containsTransparency is raised while walking operators, so a soft
// masked image nothing paints leaves it false. Two fixtures identical but for a
// single Do settle it, and pdfjs issue12963.pdf shows the shape in the wild,
// declaring eleven soft masked images while drawing only FXX15, which has no
// /SMask; issue14847.pdf declares two and draws nothing at all. veraPDF reports
// neither, at 2b or at 4.
type drawnResources struct {
	xobjects   map[string]bool
	extGStates map[string]bool
}

// collectDrawnResources reads the XObject and ExtGState names ops invokes.
func collectDrawnResources(ops []content.Operation) *drawnResources {
	drawn := &drawnResources{
		xobjects:   make(map[string]bool),
		extGStates: make(map[string]bool),
	}
	for _, op := range ops {
		var target map[string]bool
		switch op.Operator {
		case "Do":
			target = drawn.xobjects
		case "gs":
			target = drawn.extGStates
		default:
			continue
		}
		if len(op.Operands) == 0 {
			continue
		}
		if name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName); ok {
			target[string(name)] = true
		}
	}
	return drawn
}

// drawsXObject reports whether the content stream painted the named XObject
// with Do. A nil receiver admits every name, which is what callers holding no
// parsed content stream need.
func (d *drawnResources) drawsXObject(name string) bool {
	if d == nil {
		return true
	}
	return d.xobjects[name]
}

// setsExtGState reports whether the content stream selected the named graphics
// state with gs. A nil receiver admits every name.
func (d *drawnResources) setsExtGState(name string) bool {
	if d == nil {
		return true
	}
	return d.extGStates[name]
}

// pageDrawnResources reads the resource names a page's own content stream
// invokes. Returns nil when the content cannot be read, which leaves the
// transparency survey unscoped rather than silently emptying it: a page whose
// content litho cannot parse must not be reported as transparency-free.
func pageDrawnResources(ctx context.Context, doc *document.Document, page ast.PdfDict) *drawnResources {
	data, err := resolvePageContentBytes(ctx, doc, page)
	if err != nil {
		return nil
	}
	// No content, or empty content, paints nothing: the drawn set is empty
	// rather than unknown, so a resource the page never reaches stays out of
	// scope. Only a stream litho cannot read falls back to surveying them all.
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return nil
	}
	return collectDrawnResources(ops)
}

// formDrawnResources parses a Form XObject's own content stream so recursion
// into it judges what the form paints rather than what it declares. Returns nil
// when the stream cannot be read, which keeps the survey of that form's
// resources unscoped rather than silently emptying it.
func formDrawnResources(ctx context.Context, doc *document.Document, stream ast.PdfStream) *drawnResources {
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return nil
	}
	return collectDrawnResources(ops)
}

// resourcesUseTransparency reports whether a resource dictionary reaches any
// transparency the content stream actually draws: an ExtGState with a
// non-isolating blend mode, a soft mask, or a non-opaque constant alpha; an
// Image XObject with a /SMask; or a Form XObject with a transparency /Group. It
// recurses into Form XObject resources, bounded by maxFormXObjectDepth and a
// visited set of form-dict identities, so nested transparency is detected while
// cyclic references cannot loop.
//
// drawn narrows the scan to the names the content stream invokes; see
// [drawnResources]. A nil drawn admits every name, for callers that hold no
// content stream to consult.
func resourcesUseTransparency(ctx context.Context, doc *document.Document, res ast.PdfDict, depth int, visited map[string]bool, drawn *drawnResources) bool {
	if res == nil || depth > maxFormXObjectDepth || ctx.Err() != nil {
		return false
	}

	if gsDict, err := document.ResolveDict(ctx, doc, res["ExtGState"]); err == nil && gsDict != nil {
		for _, name := range sortedNames(gsDict) {
			if !drawn.setsExtGState(string(name)) {
				continue
			}
			gs, err := document.ResolveDict(ctx, doc, gsDict[name])
			if err != nil || gs == nil {
				continue
			}
			if bmObj, ok := gs["BM"]; ok {
				bm := resolveBlendMode(ctx, doc, bmObj)
				if bm != "" && bm != "Normal" && bm != "Compatible" {
					return true
				}
			}
			if smObj, ok := gs["SMask"]; ok {
				if nm, _ := document.ResolveName(ctx, doc, smObj); nm != "None" {
					return true
				}
			}
			if v, ok := gs["CA"]; ok && !floatEquals1(document.Float(v)) {
				return true
			}
			if v, ok := gs["ca"]; ok && !floatEquals1(document.Float(v)) {
				return true
			}
		}
	}

	xobjDict, err := document.ResolveDict(ctx, doc, res["XObject"])
	if err != nil || xobjDict == nil {
		return false
	}
	for _, name := range sortedNames(xobjDict) {
		if ctx.Err() != nil {
			return false
		}
		if !drawn.drawsXObject(string(name)) {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, xobjDict[name])
		if err != nil {
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		switch subtype {
		case "Image":
			if _, ok := stream.Dict["SMask"]; ok {
				return true
			}
			// A JPXDecode image can carry its opacity channel inside the JPEG2000
			// codestream instead of an explicit /SMask, signalled by /SMaskInData
			// 1 or 2 (ISO 32000-1 Table 89). That is still transparency, so the
			// page needs a blending colour space (veraPDF §6.2.10 t2).
			if document.Float(stream.Dict["SMaskInData"]) > 0 {
				return true
			}
		case "Form":
			if group, err := document.ResolveDict(ctx, doc, stream.Dict["Group"]); err == nil && group != nil {
				if s, _ := document.ResolveName(ctx, doc, group["S"]); s == "Transparency" {
					return true
				}
			}
			addr := formatDictAddr(stream.Dict)
			if visited[addr] {
				continue
			}
			visited[addr] = true
			formRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
			// A drawn form paints its own content, so recurse under the names
			// that form invokes rather than the page's.
			formDrawn := drawn
			if drawn != nil {
				formDrawn = formDrawnResources(ctx, doc, stream)
			}
			if resourcesUseTransparency(ctx, doc, formRes, depth+1, visited, formDrawn) {
				return true
			}
		}
	}
	return false
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
			for _, stream := range appearanceStreams(ctx, doc, apObj) {
				// Transparency group on the appearance Form XObject.
				if group, err := document.ResolveDict(ctx, doc, stream.Dict["Group"]); err == nil && group != nil {
					if s, _ := document.ResolveName(ctx, doc, group["S"]); s == "Transparency" {
						return true
					}
				}
				// Transparency in the appearance's own resources. This walks the
				// same tree as the page pass: an appearance routinely paints a
				// soft-masked image or a transparency-group form rather than
				// setting an ExtGState, and veraPDF counts those toward the
				// page's containsTransparency (pdfjs annotation-as.pdf carries
				// image /SMask entries, stamps.pdf a /Group /S /Transparency,
				// and both fail ISO 19005-2 §6.2.10 t2 there).
				if apRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && apRes != nil {
					if resourcesUseTransparency(ctx, doc, apRes, 0, map[string]bool{},
						formDrawnResources(ctx, doc, stream)) {
						return true
					}
				}
			}
		}
	}
	return false
}

// appearanceStreams resolves an /AP entry to its appearance stream(s). The value
// is either a single Form XObject stream, or a sub-dictionary of appearance
// states (e.g. /On and /Off for a checkbox or radio-button widget) whose values
// are each an appearance stream.
func appearanceStreams(ctx context.Context, doc *document.Document, apObj ast.PdfObject) []ast.PdfStream {
	if stream, err := document.ResolveStream(ctx, doc, apObj); err == nil {
		return []ast.PdfStream{stream}
	}
	sub, err := document.ResolveDict(ctx, doc, apObj)
	if err != nil || sub == nil {
		return nil
	}
	var streams []ast.PdfStream
	for _, name := range sortedNames(sub) {
		if stream, err := document.ResolveStream(ctx, doc, sub[name]); err == nil {
			streams = append(streams, stream)
		}
	}
	return streams
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

func checkType3FontTransparency(ctx context.Context, doc *document.Document, page, pageRes ast.PdfDict) bool {
	fontDict, err := document.ResolveDict(ctx, doc, pageRes["Font"])
	if err != nil || fontDict == nil {
		return false
	}
	rendered := pageType3RenderedCodes(ctx, doc, page)
	for fontName, fontObj := range fontDict {
		fd, err := document.ResolveDict(ctx, doc, fontObj)
		if err != nil || fd == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"]); subtype != "Type3" {
			continue
		}
		fontRes, err := document.ResolveDict(ctx, doc, fd["Resources"])
		if err != nil || fontRes == nil {
			continue
		}
		charProcs, err := document.ResolveDict(ctx, doc, fd["CharProcs"])
		if err != nil || charProcs == nil {
			continue
		}

		// veraPDF counts a Type 3 font's transparency only for glyphs the content
		// actually paints: a transparent glyph that is never shown does not make
		// the page transparent (ISO 19005-2 §6.2.10 / 19005-4 §6.2.9, PDPage
		// containsTransparency). Scope the scan to the rendered glyphs' CharProcs.
		// If the rendered set cannot be determined (unparsable content), fall back
		// to every CharProc so real transparency is never missed.
		var glyphs []ast.PdfName
		if codes := rendered[string(fontName)]; len(codes) > 0 {
			codeToName := type3EncodingCodeToName(ctx, doc, fd)
			for code := range codes {
				if name, ok := codeToName[code]; ok {
					glyphs = append(glyphs, ast.PdfName(name))
				}
			}
		} else {
			for name := range charProcs {
				glyphs = append(glyphs, name)
			}
		}

		for _, gname := range glyphs {
			stream, err := document.ResolveStream(ctx, doc, charProcs[gname])
			if err != nil {
				continue
			}
			if type3CharProcTransparent(ctx, doc, stream, fontRes) {
				return true
			}
		}
	}
	return false
}

// pageType3RenderedCodes returns, per font resource name, the set of single-byte
// character codes shown by the page's own content stream. It scans only the page
// content (matching the resource scope of [checkType3FontTransparency]).
func pageType3RenderedCodes(ctx context.Context, doc *document.Document, page ast.PdfDict) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	data, err := resolvePageContentBytes(ctx, doc, page)
	if err != nil || len(data) == 0 {
		return out
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return out
	}
	current := ""
	// The text font belongs to the graphics state, so Q restores it. See ISO
	// 32000-1, 8.4.2 and Table 104. Without this the codes shown after a
	// restore are attributed to whichever font Tf named last.
	var fontStack []string
	record := func(s ast.PdfString) {
		if current == "" {
			return
		}
		codes := out[current]
		if codes == nil {
			codes = map[int]bool{}
			out[current] = codes
		}
		for _, b := range []byte(s) {
			codes[int(b)] = true
		}
	}
	for _, op := range ops {
		switch op.Operator {
		case "q":
			fontStack = append(fontStack, current)
		case "Q":
			if n := len(fontStack); n > 0 {
				current = fontStack[n-1]
				fontStack = fontStack[:n-1]
			}
		case "Tf":
			if len(op.Operands) >= 1 {
				if n, ok := op.Operands[0].(ast.PdfName); ok {
					current = string(n)
				}
			}
		case "Tj", "'", "\"":
			for _, operand := range op.Operands {
				if s, ok := operand.(ast.PdfString); ok {
					record(s)
				}
			}
		case "TJ":
			if len(op.Operands) >= 1 {
				if arr, ok := op.Operands[0].(ast.PdfArray); ok {
					for _, el := range arr {
						if s, ok := el.(ast.PdfString); ok {
							record(s)
						}
					}
				}
			}
		}
	}
	return out
}

// type3EncodingCodeToName maps character codes to glyph names from a Type 3
// font's /Encoding /Differences array. Codes absent from the array are omitted.
func type3EncodingCodeToName(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[int]string {
	out := map[int]string{}
	encDict, err := document.ResolveDict(ctx, doc, fontDict["Encoding"])
	if err != nil || encDict == nil {
		return out
	}
	diffArr, err := document.ResolveArray(ctx, doc, encDict["Differences"])
	if err != nil {
		return out
	}
	code := 0
	for _, item := range diffArr {
		switch v := item.(type) {
		case ast.PdfInt:
			code = int(v)
		case ast.PdfName:
			out[code] = string(v)
			code++
		}
	}
	return out
}

// type3CharProcTransparent reports whether a Type 3 glyph procedure paints with
// transparency: a gs operator selecting an ExtGState with a non-opaque alpha,
// soft mask, or non-standard blend mode, or a Do operator painting a Form
// XObject that is a transparency group. The referenced resources resolve through
// the font's own /Resources.
func type3CharProcTransparent(ctx context.Context, doc *document.Document, charProc ast.PdfStream, fontRes ast.PdfDict) bool {
	data, err := doc.DecodeStream(ctx, charProc)
	if err != nil {
		return false
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return false
	}
	gsDict, _ := document.ResolveDict(ctx, doc, fontRes["ExtGState"])
	xobjDict, _ := document.ResolveDict(ctx, doc, fontRes["XObject"])
	for _, op := range ops {
		switch op.Operator {
		case "gs":
			if gsDict == nil || len(op.Operands) < 1 {
				continue
			}
			name, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				continue
			}
			if gs, err := document.ResolveDict(ctx, doc, gsDict[name]); err == nil && gs != nil && extGStateTransparent(ctx, doc, gs) {
				return true
			}
		case "Do":
			if xobjDict == nil || len(op.Operands) < 1 {
				continue
			}
			name, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				continue
			}
			stream, err := document.ResolveStream(ctx, doc, xobjDict[name])
			if err != nil {
				continue
			}
			switch st, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); st {
			case "Image":
				// A glyph procedure that paints a soft-masked image makes the
				// page transparent exactly as the page content would (ISO
				// 32000-1 §11.6.5): pdfjs issue10717.pdf keeps its /SMask
				// images in the Type3 font's own /Resources, where only this
				// walk reaches them.
				if _, ok := stream.Dict["SMask"]; ok {
					return true
				}
				if document.Float(stream.Dict["SMaskInData"]) > 0 {
					return true
				}
			case "Form":
				if group, err := document.ResolveDict(ctx, doc, stream.Dict["Group"]); err == nil && group != nil {
					if s, _ := document.ResolveName(ctx, doc, group["S"]); s == "Transparency" {
						return true
					}
				}
				formRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
				if resourcesUseTransparency(ctx, doc, formRes, 0, map[string]bool{},
					formDrawnResources(ctx, doc, stream)) {
					return true
				}
			}
		}
	}
	return false
}

// extGStateTransparent reports whether an ExtGState dictionary establishes
// transparency: a non-standard blend mode, a soft mask, or a non-opaque
// stroking or non-stroking constant alpha.
func extGStateTransparent(ctx context.Context, doc *document.Document, gs ast.PdfDict) bool {
	if bmObj, ok := gs["BM"]; ok {
		if bm := resolveBlendMode(ctx, doc, bmObj); bm != "" && bm != "Normal" && bm != "Compatible" {
			return true
		}
	}
	if smaskObj, ok := gs["SMask"]; ok {
		if smaskName, _ := document.ResolveName(ctx, doc, smaskObj); smaskName != "None" {
			return true
		}
	}
	if caObj, ok := gs["CA"]; ok && !floatEquals1(document.Float(caObj)) {
		return true
	}
	if caObj, ok := gs["ca"]; ok && !floatEquals1(document.Float(caObj)) {
		return true
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

	csObj, hasCS := group["CS"]
	if !hasCS {
		return false
	}
	// Per ISO 32000-1 §7.3.9 an indirect reference to a non-existent object is
	// the null object, and per §7.3.7 a dictionary entry whose value is null is
	// equivalent to the entry being absent. pdfjs issue11045.pdf points its
	// group /CS at a free object, so the page has no blending colour space and
	// still owes ISO 19005-2 §6.2.10 t2 an answer.
	cs, err := document.ResolveObject(ctx, doc, csObj)
	if err != nil {
		return false
	}
	return !isNullObject(cs)
}
