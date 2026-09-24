package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func deviceRGBRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_3_t01, Rule_19005_2_6_2_4_3_t02, Rule_19005_3_6_2_4_3_t02, Rule_19005_4_6_2_4_3_t02)
}

func deviceCMYKRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_3_t02, Rule_19005_2_6_2_4_3_t03, Rule_19005_3_6_2_4_3_t03, Rule_19005_4_6_2_4_3_t03)
}

func deviceGrayRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_3_t03, Rule_19005_2_6_2_4_3_t04, Rule_19005_3_6_2_4_3_t04, Rule_19005_4_6_2_4_3_t04)
}

// defaultCS tracks which Default{RGB,CMYK,Gray} colour spaces are set.
type defaultCS struct {
	hasRGB  bool
	hasCMYK bool
	hasGray bool
}

// deviceCSUsage tracks which device-dependent colour spaces are used.
type deviceCSUsage struct {
	rgb  bool
	cmyk bool
	gray bool
	// subScope* track device CS usage from sub-scopes (Form XObjects,
	// tiling patterns) that is NOT covered by the sub-scope's own DefaultCS
	// or Group CS. This usage cannot be covered by a parent scope's DefaultCS
	// per ISO 32000-2 §8.6.5.7. Only OutputIntent or blending CS apply.
	subScopeRGB  bool
	subScopeCMYK bool
	subScopeGray bool
	// initialGray tracks painting with no colour space selected, here or in
	// any scope that invoked this one, which uses the initial DeviceGray of
	// ISO 32000-1, 8.6.8. That selection belongs to the root content stream
	// (the page, or an annotation appearance), so the root's DefaultGray
	// covers it and a form's or a tiling pattern's own never does: veraPDF
	// was measured on forms, nested forms, image masks and patterns.
	initialGray bool
}

// maxFormXObjectDepth is the maximum recursion depth for scanning Form XObject
// content streams and their nested resources. Anti-DoS guard, not spec-mandated:
// ISO 32000-1 sets no limit on how deeply one form may invoke another, so a
// crafted file could nest them until the goroutine stack is exhausted.
//
// It bounds CHAIN LENGTH only. Cycles are broken by the visited sets the callers
// own, not by this cap: [checkFormXObjectFonts] and [checkType3CharProcFonts]
// key on object number, [scanFormXObjectCS] on the resolved dict's pointer. A
// depth bound survives a cycle without detecting it, so both are needed.
//
// Ten is safe for real documents: a form invoking a form is already unusual, and
// the depth is the invocation chain rather than the resource tree, so legitimate
// nesting stops far short. This is the package-wide bound, read by thirteen
// recursion sites across nine files (fonts, transparency, colour, file limits,
// accessibility); the same name in typography/extract.go is a separate constant
// for the text-extraction walk.
const maxFormXObjectDepth = 10

// checkDeviceColorSpaces validates that device-dependent colour spaces
// (DeviceRGB, DeviceCMYK, DeviceGray) are only used when allowed.
// checkDeviceColorSpaces takes the already-resolved pages so it can reuse their
// parsed operations; the page dictionaries are the same ones [collectPages]
// yields, in the same order.
//
// # Corpus evidence
//
//   - pdfjs bug1019475_1.pdf: gives its page a DeviceRGB transparency group
//     beside a DefaultRGB, and veraPDF reports no DeviceRGB.
func checkDeviceColorSpaces(ctx context.Context, doc *document.Document, pages []pageCtx, part int, hasICC bool, iccColorSpace string, results *[]Violation) {
	for i := range pages {
		page := pages[i].dict
		if ctx.Err() != nil {
			return
		}

		// /Resources is an inheritable page attribute (ISO 32000-1 §7.7.3.4): a
		// page with none of its own uses the nearest ancestor's. veraPDF checks
		// device colour usage against the effective resources, so litho must
		// resolve inheritance here or it skips colour checks on pages whose
		// resources live on a Pages node (qpdf shared-images, form-xobjects-*).
		// A page with no /Resources anywhere in its ancestry still paints: the
		// initial graphics-state colour space is DeviceGray (ISO 32000-1
		// §8.6.8), so a bare "0 0 m 10 10 l S" is a DeviceGray use that
		// ISO 19005-1 §6.2.3.3 t3 requires an OutputIntent to cover. Keep
		// scanning with a nil resource dict instead of skipping the page: every
		// lookup below tolerates a nil dict, and a page with no resources can
		// declare no DefaultCS to suppress the finding.
		resources := resolveInheritedResources(ctx, doc, page)

		defaults := collectDefaultColorSpaces(ctx, doc, resources)

		// Scan colour spaces used in the page resources.
		csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve ColorSpace dict", "page", i, "error", err)
		}

		// Scan content stream operators for device CS usage and collect
		// XObject names referenced by Do operators.
		contentCSUsed, referencedXObjects, contentCSNames, usedResourceKeys, inheritedCSAtDo := scanContentStreamDeviceCS(ctx, pages[i].parsedOps(ctx), collectPatternCSNames(ctx, doc, resources))

		deviceCSUsed := scanForDeviceColorSpaces(ctx, doc, csDict, resources, referencedXObjects, usedResourceKeys, inheritedCSAtDo, part, contentCSNames)
		deviceCSUsed.rgb = deviceCSUsed.rgb || contentCSUsed.rgb
		deviceCSUsed.cmyk = deviceCSUsed.cmyk || contentCSUsed.cmyk
		deviceCSUsed.gray = deviceCSUsed.gray || contentCSUsed.gray
		// The initial DeviceGray painted with no colour space selected, on the
		// page or in a form or tiling pattern it draws, is the page's own
		// selection, which the page's DefaultGray covers (ISO 32000-1, 8.6.5.6).
		deviceCSUsed.gray = deviceCSUsed.gray || contentCSUsed.initialGray || deviceCSUsed.initialGray

		// Also scan annotation appearance streams for device CS usage.
		annotUsage := scanAnnotationAppearanceCS(ctx, doc, page, part)
		deviceCSUsed.rgb = deviceCSUsed.rgb || annotUsage.rgb
		deviceCSUsed.cmyk = deviceCSUsed.cmyk || annotUsage.cmyk
		deviceCSUsed.gray = deviceCSUsed.gray || annotUsage.gray
		deviceCSUsed.subScopeRGB = deviceCSUsed.subScopeRGB || annotUsage.subScopeRGB
		deviceCSUsed.subScopeCMYK = deviceCSUsed.subScopeCMYK || annotUsage.subScopeCMYK
		deviceCSUsed.subScopeGray = deviceCSUsed.subScopeGray || annotUsage.subScopeGray

		// Check page transparency group colour space as additional coverage.
		// Per the spec, the "current transparency blending space" also provides
		// device CS coverage.
		blendingCS := pageBlendingColorSpace(ctx, doc, page)

		// PDF/A-4 (PDF 2.0): page-level OutputIntents provide coverage for
		// the page, overriding or supplementing catalog-level OutputIntents.
		pageHasICC, pageICCCS := false, ""
		if part == 4 {
			pageHasICC, pageICCCS = checkPageOutputIntent(ctx, doc, page)
		}
		effectiveHasICC := hasICC || pageHasICC
		effectiveICCCS := iccColorSpace
		if pageHasICC {
			effectiveICCCS = pageICCCS
		}

		// Universal coverage: the OutputIntent applies to all scopes. The
		// transparency-group blending CS counts only under PDF/A-4. ISO
		// 19005-2/3 §6.2.4.3 conditions DeviceRGB/CMYK coverage on the
		// OutputIntent ICC profile colour space alone, so a page-level RGB
		// group blending CS does not cover page-content rg under those parts.
		// ISO 19005-4 (PDF 2.0) admits the current blending colour space as
		// coverage, following the PDF 2.0 transparency model.
		rgbUniversalCover := (effectiveHasICC && effectiveICCCS == "RGB ") || (part == 4 && blendingCS == "RGB ")
		cmykUniversalCover := (effectiveHasICC && effectiveICCCS == "CMYK") || (part == 4 && blendingCS == "CMYK")
		grayUniversalCover := effectiveHasICC

		// DeviceRGB: needs DefaultRGB, OutputIntent with RGB, or RGB blending CS.
		if deviceCSUsed.rgb && !defaults.hasRGB && !rgbUniversalCover {
			*results = append(*results, failf(deviceRGBRule(part), "page %d: DeviceRGB used without DefaultRGB or RGB OutputIntent", i+1)...)
		}
		// Sub-scope DeviceRGB: page DefaultRGB doesn't apply; only universal
		// coverage. This adds something only when a page DefaultRGB suppressed
		// the check above: without one, that check already reports the same
		// usage, and firing both wrote the clause twice for a single use.
		if deviceCSUsed.subScopeRGB && defaults.hasRGB && !rgbUniversalCover {
			*results = append(*results, failf(deviceRGBRule(part), "page %d: DeviceRGB used in pattern/form without local DefaultRGB or RGB OutputIntent", i+1)...)
		}

		// DeviceCMYK: needs DefaultCMYK, OutputIntent with CMYK, or CMYK blending CS.
		if deviceCSUsed.cmyk && !defaults.hasCMYK && !cmykUniversalCover {
			*results = append(*results, failf(deviceCMYKRule(part), "page %d: DeviceCMYK used without DefaultCMYK or CMYK OutputIntent", i+1)...)
		}
		// Sub-scope DeviceCMYK: page DefaultCMYK doesn't apply. Reported only
		// when a page DefaultCMYK suppressed the check above (see DeviceRGB).
		if deviceCSUsed.subScopeCMYK && defaults.hasCMYK && !cmykUniversalCover {
			*results = append(*results, failf(deviceCMYKRule(part), "page %d: DeviceCMYK used in pattern/form without local DefaultCMYK or CMYK OutputIntent", i+1)...)
		}

		// DeviceGray: needs DefaultGray or any OutputIntent.
		if deviceCSUsed.gray && !defaults.hasGray && !grayUniversalCover {
			*results = append(*results, failf(deviceGrayRule(part), "page %d: DeviceGray used without DefaultGray or OutputIntent", i+1)...)
		}
		// Sub-scope DeviceGray: page DefaultGray doesn't apply. Reported only
		// when a page DefaultGray suppressed the check above (see DeviceRGB).
		if deviceCSUsed.subScopeGray && defaults.hasGray && !grayUniversalCover {
			*results = append(*results, failf(deviceGrayRule(part), "page %d: DeviceGray used in pattern/form without local DefaultGray or OutputIntent", i+1)...)
		}

		// A transparency group whose blending colour space is itself a device
		// colour space is a device-colour usage (ISO 19005-2/3 §6.2.4.3; veraPDF
		// object PDDeviceRGB at context /Group/colorSpace). The page's effective
		// resources resolve it like any other device colour space, so a matching
		// Default colour space there covers it (ISO 32000-1, 8.6.5.6), inherited
		// from a Pages node or not, and veraPDF then reports no device colour.
		// Measured for RGB, CMYK and Gray groups. The "transparency group"
		// context routes the repair to fixGroupCS, which drops the /CS so the
		// blending space falls back to the intent profile, rather than to the
		// OutputIntent fixer.
		switch pageTransparencyGroupCSName(ctx, doc, page) {
		case "DeviceRGB":
			if !rgbUniversalCover && !defaults.hasRGB {
				*results = append(*results, failf(deviceRGBRule(part), "page %d: transparency group blending colour space is DeviceRGB without RGB OutputIntent", i+1)...)
			}
		case "DeviceCMYK":
			if !cmykUniversalCover && !defaults.hasCMYK {
				*results = append(*results, failf(deviceCMYKRule(part), "page %d: transparency group blending colour space is DeviceCMYK without CMYK OutputIntent", i+1)...)
			}
		case "DeviceGray":
			if !grayUniversalCover && !defaults.hasGray {
				*results = append(*results, failf(deviceGrayRule(part), "page %d: transparency group blending colour space is DeviceGray without OutputIntent", i+1)...)
			}
		}
	}
}

// pageBlendingColorSpace returns the ICC colour space signature of the page's
// transparency group colour space (/Group /CS), or "" if absent. The returned
// value is "RGB ", "CMYK", or "GRAY" (ICC 4-byte signature).
func pageBlendingColorSpace(ctx context.Context, doc *document.Document, page ast.PdfDict) string {
	groupObj, ok := page["Group"]
	if !ok {
		return ""
	}
	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return ""
	}
	// Must be a transparency group.
	s, _ := document.ResolveName(ctx, doc, group["S"])
	if s != "Transparency" {
		return ""
	}
	csObj, ok := group["CS"]
	if !ok {
		return ""
	}
	// Resolve the colour space. Device-independent blending colour spaces
	// provide device CS coverage. This includes ICCBased, CalRGB, CalGray,
	// and Lab. Device{RGB,CMYK,Gray} blending spaces do NOT count.
	csName := resolveCSName(ctx, doc, csObj)
	switch csName {
	case "CalRGB":
		return "RGB "
	case "CalGray":
		return "GRAY"
	case "Lab":
		return "GRAY" // Lab is device-independent; treat as covering Gray.
	case "ICCBased":
		// Read the ICC profile to determine the actual colour space.
		arr, err := document.ResolveArray(ctx, doc, csObj)
		if err != nil || len(arr) < 2 {
			return ""
		}
		stream, err := document.ResolveStream(ctx, doc, arr[1])
		if err != nil {
			return ""
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			return ""
		}
		hdr, ok := parseICCHeader(data)
		if !ok {
			return ""
		}
		return hdr.colorSpace
	default:
		return ""
	}
}

// pageTransparencyGroupCSName returns the raw colour space name of the page's
// transparency group blending space (/Group /CS), e.g. "DeviceRGB", or "" when
// absent. Unlike [pageBlendingColorSpace] it does not resolve ICC signatures:
// it exists to detect a device blending space, which veraPDF treats as a
// device-colour usage coverable only by an OutputIntent.
func pageTransparencyGroupCSName(ctx context.Context, doc *document.Document, page ast.PdfDict) string {
	group, err := document.ResolveDict(ctx, doc, page["Group"])
	if err != nil || group == nil {
		return ""
	}
	if s, _ := document.ResolveName(ctx, doc, group["S"]); s != "Transparency" {
		return ""
	}
	csObj, ok := group["CS"]
	if !ok {
		return ""
	}
	return resolveCSName(ctx, doc, csObj)
}

// collectDefaultColorSpaces extracts Default{RGB,CMYK,Gray} entries from a
// resources dictionary's /ColorSpace sub-dictionary.
func collectDefaultColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict) defaultCS {
	csObj, ok := resources["ColorSpace"]
	if !ok {
		return defaultCS{}
	}

	csDict, err := document.ResolveDict(ctx, doc, csObj)
	if err != nil || csDict == nil {
		doc.Logger.Debug("compliance.color: cannot resolve ColorSpace dict for defaults", "error", err)
		return defaultCS{}
	}

	var ds defaultCS
	if _, ok := csDict["DefaultRGB"]; ok {
		ds.hasRGB = true
	}
	if _, ok := csDict["DefaultCMYK"]; ok {
		ds.hasCMYK = true
	}
	if _, ok := csDict["DefaultGray"]; ok {
		ds.hasGray = true
	}
	return ds
}

// collectPatternCSNames returns the /Resources/ColorSpace keys that name a
// Pattern colour space, so the content scan can tell that "/cs1 cs" selects
// one.
//
// A pattern space is written [/Pattern] or [/Pattern base] (ISO 32000-1
// §8.7.3.3) and is normally reached through a resource name rather than the
// literal /Pattern. Recognising only the literal spelling left every such
// pattern looking unpainted.
//
// # Corpus evidence
//
//   - pdfjs tiling-pattern-large-steps.pdf: declares /cs1 [/Pattern
//     /DeviceRGB] and selects it with "/cs1 cs".
//   - pdfjs issue13372.pdf: declares /R8 [/Pattern].
func collectPatternCSNames(ctx context.Context, doc *document.Document, resources ast.PdfDict) map[string]bool {
	csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err != nil || csDict == nil {
		return nil
	}

	var names map[string]bool
	for name, csObj := range csDict {
		if resolveCSName(ctx, doc, csObj) != "Pattern" {
			continue
		}
		if names == nil {
			names = make(map[string]bool)
		}
		names[string(name)] = true
	}
	return names
}

// collectUsedCSNames returns the set of colour space names referenced by
// cs/CS operators in the pre-parsed operations. Returns nil if ops is empty.
func collectUsedCSNames(ops []content.Operation) map[string]bool {
	if len(ops) == 0 {
		return nil
	}
	used := make(map[string]bool)
	for _, op := range ops {
		if op.Operator == "cs" || op.Operator == "CS" {
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					used[string(name)] = true
				}
			}
		}
	}
	return used
}

// checkPageOutputIntent checks for a page-level /OutputIntents array (PDF 2.0).
// Returns whether an OutputIntent with a valid ICC profile exists and its colour space.
func checkPageOutputIntent(ctx context.Context, doc *document.Document, page ast.PdfDict) (bool, string) {
	oiObj, ok := page["OutputIntents"]
	if !ok {
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
		// Only accept PDF/A OutputIntents, not PDF/X or others. The
		// subtype is GTS_PDFA1 for every PDF/A part (ISO 32000-2 Table
		// 401). "GTS_PDFA" is not a defined subtype, so an intent carrying
		// it is not a PDF/A OutputIntent and cannot supply device CS
		// coverage.
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
			doc.Logger.Debug("compliance.color: cannot resolve output profile stream", "error", err)
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) < 20 {
			return true, ""
		}
		return true, string(data[16:20])
	}
	return false, ""
}
