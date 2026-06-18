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
}

// maxFormXObjectDepth is the maximum recursion depth for scanning Form XObject
// content streams and their nested resources.
const maxFormXObjectDepth = 10

// checkDeviceColorSpaces validates that device-dependent colour spaces
// (DeviceRGB, DeviceCMYK, DeviceGray) are only used when allowed.
func checkDeviceColorSpaces(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, hasICC bool, iccColorSpace string, results *[]Violation) {
	pages := collectPages(ctx, doc, catalog)
	for i, page := range pages {
		if ctx.Err() != nil {
			return
		}

		resources := resolveResources(ctx, doc, page)
		if resources == nil {
			continue
		}

		defaults := collectDefaultColorSpaces(ctx, doc, resources)

		// Scan colour spaces used in the page resources.
		csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve ColorSpace dict", "page", i, "error", err)
		}

		// Scan content stream operators for device CS usage and collect
		// XObject names referenced by Do operators.
		contentCSUsed, referencedXObjects := scanContentStreamDeviceCS(ctx, doc, page, i)

		deviceCSUsed := scanForDeviceColorSpaces(ctx, doc, csDict, resources, referencedXObjects, part)
		deviceCSUsed.rgb = deviceCSUsed.rgb || contentCSUsed.rgb
		deviceCSUsed.cmyk = deviceCSUsed.cmyk || contentCSUsed.cmyk
		deviceCSUsed.gray = deviceCSUsed.gray || contentCSUsed.gray

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
		// Sub-scope DeviceRGB: page DefaultRGB doesn't apply; only universal coverage.
		if deviceCSUsed.subScopeRGB && !rgbUniversalCover {
			*results = append(*results, failf(deviceRGBRule(part), "page %d: DeviceRGB used in pattern/form without local DefaultRGB or RGB OutputIntent", i+1)...)
		}

		// DeviceCMYK: needs DefaultCMYK, OutputIntent with CMYK, or CMYK blending CS.
		if deviceCSUsed.cmyk && !defaults.hasCMYK && !cmykUniversalCover {
			*results = append(*results, failf(deviceCMYKRule(part), "page %d: DeviceCMYK used without DefaultCMYK or CMYK OutputIntent", i+1)...)
		}
		// Sub-scope DeviceCMYK: page DefaultCMYK doesn't apply.
		if deviceCSUsed.subScopeCMYK && !cmykUniversalCover {
			*results = append(*results, failf(deviceCMYKRule(part), "page %d: DeviceCMYK used in pattern/form without local DefaultCMYK or CMYK OutputIntent", i+1)...)
		}

		// DeviceGray: needs DefaultGray or any OutputIntent.
		if deviceCSUsed.gray && !defaults.hasGray && !grayUniversalCover {
			*results = append(*results, failf(deviceGrayRule(part), "page %d: DeviceGray used without DefaultGray or OutputIntent", i+1)...)
		}
		// Sub-scope DeviceGray: page DefaultGray doesn't apply.
		if deviceCSUsed.subScopeGray && !grayUniversalCover {
			*results = append(*results, failf(deviceGrayRule(part), "page %d: DeviceGray used in pattern/form without local DefaultGray or OutputIntent", i+1)...)
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
