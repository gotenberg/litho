package compliance

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// --- Rule selectors ---

func deviceRGBRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_3_t01, Rule_19005_2_6_2_4_3_t02, Rule_19005_3_6_2_4_3_t02, Rule_19005_4_6_2_4_3_t02)
}

func deviceCMYKRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_3_t02, Rule_19005_2_6_2_4_3_t03, Rule_19005_3_6_2_4_3_t03, Rule_19005_4_6_2_4_3_t03)
}

func deviceGrayRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_3_t03, Rule_19005_2_6_2_4_3_t04, Rule_19005_3_6_2_4_3_t04, Rule_19005_4_6_2_4_3_t04)
}

// --- Types ---

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
	// per ISO 32000-2 §8.6.5.7 — only OutputIntent or blending CS apply.
	subScopeRGB  bool
	subScopeCMYK bool
	subScopeGray bool
}

// maxFormXObjectDepth is the maximum recursion depth for scanning Form XObject
// content streams and their nested resources.
const maxFormXObjectDepth = 10

// --- Check implementation ---

// checkDeviceColorSpaces validates that device-dependent colour spaces
// (DeviceRGB, DeviceCMYK, DeviceGray) are only used when allowed.
func checkDeviceColorSpaces(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, hasICC bool, iccColorSpace string, results *[]Violation) {
	pages := collectPages(ctx, doc, catalog)
	for i, page := range pages {
		if ctx.Err() != nil {
			return
		}

		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}

		defaults := collectDefaultColorSpaces(ctx, doc, resources)

		// Scan colour spaces used in the page resources.
		csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
		if err != nil {
			slog.Debug("compliance.color: cannot resolve ColorSpace dict", "page", i, "error", err)
		}

		// Scan content stream operators for device CS usage and collect
		// XObject names referenced by Do operators.
		contentCSUsed, referencedXObjects := scanContentStreamDeviceCS(ctx, doc, page, i)

		deviceCSUsed := scanForDeviceColorSpaces(ctx, doc, csDict, resources, referencedXObjects)
		deviceCSUsed.rgb = deviceCSUsed.rgb || contentCSUsed.rgb
		deviceCSUsed.cmyk = deviceCSUsed.cmyk || contentCSUsed.cmyk
		deviceCSUsed.gray = deviceCSUsed.gray || contentCSUsed.gray

		// Also scan annotation appearance streams for device CS usage.
		annotUsage := scanAnnotationAppearanceCS(ctx, doc, page)
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

		// Universal coverage: OutputIntent and blending CS apply to ALL scopes.
		rgbUniversalCover := (effectiveHasICC && effectiveICCCS == "RGB ") || blendingCS == "RGB "
		cmykUniversalCover := (effectiveHasICC && effectiveICCCS == "CMYK") || blendingCS == "CMYK"
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
		slog.Debug("compliance.color: cannot resolve ColorSpace dict for defaults", "error", err)
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
		// Only accept PDF/A OutputIntents, not PDF/X or others.
		s, _ := document.ResolveName(ctx, doc, intent["S"])
		if s != "GTS_PDFA" && s != "GTS_PDFA1" {
			continue
		}
		destObj, ok := intent["DestOutputProfile"]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, destObj)
		if err != nil {
			slog.Debug("compliance.color: cannot resolve output profile stream", "error", err)
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

// scanForDeviceColorSpaces looks through page resources for uses of DeviceRGB,
// DeviceCMYK, and DeviceGray colour spaces.
func scanForDeviceColorSpaces(ctx context.Context, doc *document.Document, csDict ast.PdfDict, resources ast.PdfDict, referencedXObjects map[string]bool) deviceCSUsage {
	var usage deviceCSUsage

	// Check named colour spaces in the ColorSpace dictionary.
	for _, csObj := range csDict {
		if ctx.Err() != nil {
			return usage
		}
		csName := resolveCSName(ctx, doc, csObj)
		markDeviceCS(csName, &usage)

		// Separation and DeviceN arrays use an alternate space (arr[2]) that
		// may itself be a device colour space. If so, the device CS is in use.
		if csName == "Separation" || csName == "DeviceN" {
			arr, err := document.ResolveArray(ctx, doc, csObj)
			if err == nil && len(arr) >= 3 {
				altName := resolveCSName(ctx, doc, arr[2])
				markDeviceCS(altName, &usage)
			}
		}

		// Indexed CS uses a base colour space (arr[1]). If the base is a
		// device CS, then device CS is in use.
		if csName == "Indexed" {
			arr, err := document.ResolveArray(ctx, doc, csObj)
			if err == nil && len(arr) >= 2 {
				baseName := resolveCSName(ctx, doc, arr[1])
				markDeviceCS(baseName, &usage)
			}
		}
	}

	// Check inline colour spaces in XObject, Font, Shading, and Pattern resources.
	// Only scan XObjects that are actually referenced by Do operators in the
	// content stream; unreferenced XObjects are not "used" per the spec.
	visited := make(map[string]bool)
	scanXObjectColorSpaces(ctx, doc, resources, &usage, 0, visited, referencedXObjects)
	scanFontColorSpaces(ctx, doc, resources, &usage)
	scanShadingColorSpaces(ctx, doc, resources, &usage)
	scanPatternColorSpaces(ctx, doc, resources, &usage, 0, visited)

	return usage
}

// scanXObjectColorSpaces checks XObject resources for device colour space usage.
// For Form XObjects, it recurses into their content streams and nested resources.
func scanXObjectColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage, depth int, visited map[string]bool, referencedNames map[string]bool) {
	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return
	}

	for name, xobjObj := range xobjDict {
		if ctx.Err() != nil {
			return
		}

		// Only scan XObjects that are actually invoked (by Do operators).
		// Unreferenced XObjects in /Resources are not "used" per the spec.
		if referencedNames != nil && !referencedNames[string(name)] {
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, xobjObj)
		if err != nil {
			slog.Debug("compliance.color: cannot resolve XObject stream", "name", name, "error", err)
			continue
		}

		// Check the XObject's own /ColorSpace entry.
		csObj := stream.Dict["ColorSpace"]
		csResolved := resolveCSName(ctx, doc, csObj)
		if csResolved != "" {
			markDeviceCS(csResolved, usage)
			// Check base/alternate spaces for Indexed, Separation, DeviceN.
			arr, err := document.ResolveArray(ctx, doc, csObj)
			if err == nil && len(arr) >= 2 {
				switch csResolved {
				case "Indexed":
					baseName := resolveCSName(ctx, doc, arr[1])
					markDeviceCS(baseName, usage)
				case "Separation", "DeviceN":
					if len(arr) >= 3 {
						altName := resolveCSName(ctx, doc, arr[2])
						markDeviceCS(altName, usage)
					}
				}
			}
		}

		// For Form XObjects, recurse into content stream and nested resources.
		subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		if subtype == "Form" {
			formUsage := scanFormXObjectCS(ctx, doc, stream, depth, visited)
			usage.rgb = usage.rgb || formUsage.rgb
			usage.cmyk = usage.cmyk || formUsage.cmyk
			usage.gray = usage.gray || formUsage.gray
			usage.subScopeRGB = usage.subScopeRGB || formUsage.subScopeRGB
			usage.subScopeCMYK = usage.subScopeCMYK || formUsage.subScopeCMYK
			usage.subScopeGray = usage.subScopeGray || formUsage.subScopeGray
		}
	}
}

// scanFormXObjectCS decodes a Form XObject's content stream and scans for
// device-dependent colour space operators. It also checks nested resources
// (ColorSpace, XObject, Shading, Pattern) within the Form XObject.
func scanFormXObjectCS(ctx context.Context, doc *document.Document, stream ast.PdfStream, depth int, visited map[string]bool) deviceCSUsage {
	var usage deviceCSUsage

	if depth >= maxFormXObjectDepth {
		return usage
	}

	// Cycle detection: use fmt.Sprintf("%p") on the dict map pointer as key.
	// PdfDict is a map type; each resolved instance has a unique header pointer.
	dictAddr := formatDictAddr(stream.Dict)
	if visited[dictAddr] {
		return usage
	}
	visited[dictAddr] = true

	// Decode and scan the content stream for device CS operators.
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		slog.Debug("compliance.color: cannot decode Form XObject stream", "error", err)
		return usage
	}

	// Track Do operator targets so we only scan referenced nested XObjects.
	nestedXObjects := make(map[string]bool)
	formFillCSSet := false
	formStrokeCSSet := false

	if len(data) > 0 {
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			slog.Debug("compliance.color: cannot parse Form XObject content stream", "error", err)
		} else {
			// Check inline image colour spaces from parsed BI operations.
			scanInlineImageCSOps(ops, &usage)
			for idx, op := range ops {
				if idx%100 == 0 && ctx.Err() != nil {
					return usage
				}

				switch op.Operator {
				case "g":
					usage.gray = true
					formFillCSSet = true
				case "G":
					usage.gray = true
					formStrokeCSSet = true
				case "rg":
					usage.rgb = true
					formFillCSSet = true
				case "RG":
					usage.rgb = true
					formStrokeCSSet = true
				case "k":
					usage.cmyk = true
					formFillCSSet = true
				case "K":
					usage.cmyk = true
					formStrokeCSSet = true
				case "cs":
					formFillCSSet = true
					if len(op.Operands) >= 1 {
						if name, ok := op.Operands[0].(ast.PdfName); ok {
							markDeviceCS(string(name), &usage)
						}
					}
				case "CS":
					formStrokeCSSet = true
					if len(op.Operands) >= 1 {
						if name, ok := op.Operands[0].(ast.PdfName); ok {
							markDeviceCS(string(name), &usage)
						}
					}
				case "sc", "scn":
					formFillCSSet = true
				case "SC", "SCN":
					formStrokeCSSet = true
				case "f", "F", "f*":
					if !formFillCSSet {
						usage.gray = true
					}
				case "S", "s":
					if !formStrokeCSSet {
						usage.gray = true
					}
				case "B", "B*", "b", "b*":
					if !formFillCSSet || !formStrokeCSSet {
						usage.gray = true
					}
				case "Do":
					if len(op.Operands) >= 1 {
						if name, ok := op.Operands[0].(ast.PdfName); ok {
							nestedXObjects[string(name)] = true
						}
					}
				}
			}
		}
	}

	// Check nested resources within the Form XObject.
	formRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil || formRes == nil {
		return usage
	}

	// Check ColorSpace dict in nested resources — including Indexed base
	// spaces. Separation/DeviceN alternate spaces are NOT checked here
	// because the Form XObject may have its own DefaultCS that covers them.
	formCSDict, err := document.ResolveDict(ctx, doc, formRes["ColorSpace"])
	if err == nil && formCSDict != nil {
		formDefaults := collectDefaultColorSpaces(ctx, doc, formRes)
		for _, csObj := range formCSDict {
			if ctx.Err() != nil {
				return usage
			}
			resolved := resolveCSName(ctx, doc, csObj)
			markDeviceCS(resolved, &usage)

			// Check Indexed base space.
			if resolved == "Indexed" {
				arr, err := document.ResolveArray(ctx, doc, csObj)
				if err == nil && len(arr) >= 2 {
					baseName := resolveCSName(ctx, doc, arr[1])
					// Only flag if not covered by the XObject's own DefaultCS.
					switch baseName {
					case "DeviceRGB":
						if !formDefaults.hasRGB {
							usage.rgb = true
						}
					case "DeviceCMYK":
						if !formDefaults.hasCMYK {
							usage.cmyk = true
						}
					case "DeviceGray":
						if !formDefaults.hasGray {
							usage.gray = true
						}
					}
				}
			}
		}
	}

	// Recurse into nested XObjects (only those referenced by Do operators).
	scanXObjectColorSpaces(ctx, doc, formRes, &usage, depth+1, visited, nestedXObjects)

	// Check nested Shading and Pattern resources.
	scanShadingColorSpaces(ctx, doc, formRes, &usage)
	scanPatternColorSpaces(ctx, doc, formRes, &usage, depth+1, visited)

	// Suppress device CS usage that is covered by the Form/Pattern's own
	// DefaultCS entries. Device CS operators in the content stream are
	// covered if the Form's resources define a matching Default colour space.
	formDefaults := collectDefaultColorSpaces(ctx, doc, formRes)
	if formDefaults.hasRGB {
		usage.rgb = false
	}
	if formDefaults.hasCMYK {
		usage.cmyk = false
	}
	if formDefaults.hasGray {
		usage.gray = false
	}

	// Also suppress if the Form XObject's /Group/CS provides a device-
	// independent blending colour space that covers the device CS.
	groupCS := formGroupICCColorSpace(ctx, doc, stream)
	if groupCS == "RGB " {
		usage.rgb = false
	}
	if groupCS == "CMYK" {
		usage.cmyk = false
	}
	if groupCS == "GRAY" {
		usage.gray = false
	}

	// Device CS that remains after sub-scope DefaultCS and Group CS
	// suppression is "uncovered from sub-scope". Parent-scope DefaultCS
	// cannot cover it (ISO 32000-2 §8.6.5.7: DefaultCS applies only
	// to the content stream whose Resources define it).
	usage.subScopeRGB = usage.subScopeRGB || usage.rgb
	usage.subScopeCMYK = usage.subScopeCMYK || usage.cmyk
	usage.subScopeGray = usage.subScopeGray || usage.gray

	return usage
}

// formGroupICCColorSpace returns the ICC colour space signature of a Form
// XObject's /Group/CS, or "" if absent or not device-independent.
func formGroupICCColorSpace(ctx context.Context, doc *document.Document, stream ast.PdfStream) string {
	groupObj, ok := stream.Dict["Group"]
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
	// Only isolated transparency groups provide device CS coverage.
	// Non-isolated groups inherit the parent's colour space.
	iVal, _ := document.ResolveBool(ctx, doc, group["I"])
	if !bool(iVal) {
		return ""
	}
	csObj, ok := group["CS"]
	if !ok {
		return ""
	}
	// Check for device-independent CS types.
	csName := resolveCSName(ctx, doc, csObj)
	switch csName {
	case "CalRGB":
		return "RGB "
	case "CalGray":
		return "GRAY"
	case "Lab":
		return "GRAY"
	case "ICCBased":
		arr, err := document.ResolveArray(ctx, doc, csObj)
		if err != nil || len(arr) < 2 {
			return ""
		}
		iccStream, err := document.ResolveStream(ctx, doc, arr[1])
		if err != nil {
			return ""
		}
		data, err := doc.DecodeStream(ctx, iccStream)
		if err != nil || len(data) < 20 {
			return ""
		}
		return string(data[16:20])
	}
	return ""
}

// scanFontColorSpaces checks Font resources for device colour space references
// (e.g., Type 3 fonts with their own Resources).
func scanFontColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage) {
	fontDict, err := document.ResolveDict(ctx, doc, resources["Font"])
	if err != nil || fontDict == nil {
		return
	}

	for _, fontObj := range fontDict {
		if ctx.Err() != nil {
			return
		}

		fd, err := document.ResolveDict(ctx, doc, fontObj)
		if err != nil || fd == nil {
			continue
		}

		subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"])
		if subtype != "Type3" {
			continue
		}

		// Scan CharProcs content streams for device CS operators.
		// This must run even when the font has no /Resources dict, since
		// Type3 fonts may legally inherit resources from the page.
		charProcs, err := document.ResolveDict(ctx, doc, fd["CharProcs"])
		if err == nil && charProcs != nil {
			for _, cpObj := range charProcs {
				stream, err := document.ResolveStream(ctx, doc, cpObj)
				if err != nil {
					slog.Debug("compliance.color: cannot resolve CharProc stream", "error", err)
					continue
				}
				streamData, err := doc.DecodeStream(ctx, stream)
				if err != nil {
					slog.Debug("compliance.color: cannot decode CharProc stream", "error", err)
					continue
				}
				ops, err := content.ParseStreamData(ctx, streamData)
				if err != nil {
					slog.Debug("compliance.color: cannot parse CharProc stream", "error", err)
					continue
				}
				for _, op := range ops {
					switch op.Operator {
					case "g", "G":
						usage.gray = true
					case "rg", "RG":
						usage.rgb = true
					case "k", "K":
						usage.cmyk = true
					case "cs", "CS":
						if len(op.Operands) >= 1 {
							if csN, ok := op.Operands[0].(ast.PdfName); ok {
								markDeviceCS(string(csN), usage)
							}
						}
					}
				}
			}
		}

		// Scan font's own ColorSpace resource dict (if present).
		fontRes, err := document.ResolveDict(ctx, doc, fd["Resources"])
		if err != nil || fontRes == nil {
			continue
		}
		fontCSDict, err := document.ResolveDict(ctx, doc, fontRes["ColorSpace"])
		if err != nil {
			slog.Debug("compliance.color: cannot resolve font ColorSpace dict", "error", err)
			continue
		}
		for _, csObj := range fontCSDict {
			resolved := resolveCSName(ctx, doc, csObj)
			markDeviceCS(resolved, usage)
		}
	}
}

// scanContentStreamDeviceCS parses the content stream of a page and detects
// device-dependent colour space usage from operators:
//   - g/G → DeviceGray
//   - rg/RG → DeviceRGB
//   - k/K → DeviceCMYK
//   - cs/CS with operand "DeviceRGB", "DeviceCMYK", or "DeviceGray"
func scanContentStreamDeviceCS(ctx context.Context, doc *document.Document, page ast.PdfDict, pageIdx int) (deviceCSUsage, map[string]bool) {
	var usage deviceCSUsage
	referencedXObjects := make(map[string]bool)

	streamData, err := getPageContentBytes(ctx, doc, page)
	if err != nil {
		slog.Debug("compliance.color: failed to get content stream bytes for CS scan",
			"page", pageIdx, "error", err)
		return usage, referencedXObjects
	}

	if len(streamData) == 0 {
		return usage, referencedXObjects
	}

	ops, err := content.ParseStreamData(ctx, streamData)
	if err != nil {
		slog.Debug("compliance.color: failed to parse content stream for CS scan",
			"page", pageIdx, "error", err)
		return usage, referencedXObjects
	}

	// Check inline image colour spaces from parsed BI operations.
	scanInlineImageCSOps(ops, &usage)

	// Track whether fill/stroke CS has been explicitly set. If a painting
	// operator runs without any prior CS setting, DeviceGray is implicitly used.
	fillCSSet := false
	strokeCSSet := false
	type csState struct{ fill, stroke bool }
	var csStack []csState

	for idx, op := range ops {
		// Check context every ~100 operators.
		if idx%100 == 0 && ctx.Err() != nil {
			return usage, referencedXObjects
		}

		switch op.Operator {
		case "q":
			csStack = append(csStack, csState{fillCSSet, strokeCSSet})
		case "Q":
			if len(csStack) > 0 {
				prev := csStack[len(csStack)-1]
				csStack = csStack[:len(csStack)-1]
				fillCSSet = prev.fill
				strokeCSSet = prev.stroke
			}
		case "g", "G":
			usage.gray = true
			if op.Operator == "g" {
				fillCSSet = true
			} else {
				strokeCSSet = true
			}
		case "rg", "RG":
			usage.rgb = true
			if op.Operator == "rg" {
				fillCSSet = true
			} else {
				strokeCSSet = true
			}
		case "k", "K":
			usage.cmyk = true
			if op.Operator == "k" {
				fillCSSet = true
			} else {
				strokeCSSet = true
			}
		case "cs":
			fillCSSet = true
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					markDeviceCS(string(name), &usage)
				}
			}
		case "CS":
			strokeCSSet = true
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					markDeviceCS(string(name), &usage)
				}
			}
		case "sc", "scn":
			fillCSSet = true
		case "SC", "SCN":
			strokeCSSet = true
		// Painting operators: if no CS has been set, implicit DeviceGray.
		case "f", "F", "f*":
			if !fillCSSet {
				usage.gray = true
			}
		case "S", "s":
			if !strokeCSSet {
				usage.gray = true
			}
		case "B", "B*", "b", "b*":
			if !fillCSSet || !strokeCSSet {
				usage.gray = true
			}
		case "Tj", "TJ", "'", "\"":
			// Text uses fill CS by default (Tr 0).
			if !fillCSSet {
				usage.gray = true
			}
		case "Do":
			// Track XObject names referenced by Do operators.
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					referencedXObjects[string(name)] = true
				}
			}
		}
	}

	return usage, referencedXObjects
}

// scanInlineImageCSOps checks parsed BI operations for inline image colour
// space usage. Inline images use abbreviated key names: /CS for ColorSpace.
// Abbreviations: /CS /G = DeviceGray, /CS /RGB = DeviceRGB, /CS /CMYK = DeviceCMYK.
// Also handles /CS /DeviceRGB etc. and /ColorSpace variants.
func scanInlineImageCSOps(ops []content.Operation, usage *deviceCSUsage) {
	for _, op := range ops {
		if op.Operator != "BI" || len(op.Operands) == 0 {
			continue
		}
		dict, ok := op.Operands[0].(ast.PdfDict)
		if !ok {
			continue
		}
		// Check /CS (abbreviated) and /ColorSpace (full name).
		for _, key := range []ast.PdfName{"CS", "ColorSpace"} {
			val, exists := dict[key]
			if !exists {
				continue
			}
			csName, ok := val.(ast.PdfName)
			if !ok {
				continue
			}
			switch string(csName) {
			case "G", "DeviceGray":
				usage.gray = true
			case "RGB", "DeviceRGB":
				usage.rgb = true
			case "CMYK", "DeviceCMYK":
				usage.cmyk = true
			}
		}
	}
}

// scanAnnotationAppearanceCS scans annotation appearance streams on a page
// for device colour space usage. Annotations have /AP << /N <stream/dict> >> etc.
func scanAnnotationAppearanceCS(ctx context.Context, doc *document.Document, page ast.PdfDict) deviceCSUsage {
	var usage deviceCSUsage
	annotsArr, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil || len(annotsArr) == 0 {
		return usage
	}

	visited := make(map[string]bool)
	for _, annotObj := range annotsArr {
		if ctx.Err() != nil {
			return usage
		}
		annot, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annot == nil {
			continue
		}
		apDict, err := document.ResolveDict(ctx, doc, annot["AP"])
		if err != nil || apDict == nil {
			continue
		}
		// Check /N (normal), /R (rollover), /D (down) appearances.
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apObj, ok := apDict[key]
			if !ok {
				continue
			}
			// Appearance can be a stream (single) or dict of streams.
			stream, err := document.ResolveStream(ctx, doc, apObj)
			if err == nil {
				formUsage := scanFormXObjectCS(ctx, doc, stream, 0, visited)
				usage.rgb = usage.rgb || formUsage.rgb
				usage.cmyk = usage.cmyk || formUsage.cmyk
				usage.gray = usage.gray || formUsage.gray
				usage.subScopeRGB = usage.subScopeRGB || formUsage.subScopeRGB
				usage.subScopeCMYK = usage.subScopeCMYK || formUsage.subScopeCMYK
				usage.subScopeGray = usage.subScopeGray || formUsage.subScopeGray
				continue
			}
			// Dict of sub-appearances (keyed by state name).
			subDict, err := document.ResolveDict(ctx, doc, apObj)
			if err != nil || subDict == nil {
				continue
			}
			for _, subObj := range subDict {
				subStream, err := document.ResolveStream(ctx, doc, subObj)
				if err != nil {
					slog.Debug("compliance.color: cannot resolve sub-XObject stream", "error", err)
					continue
				}
				formUsage := scanFormXObjectCS(ctx, doc, subStream, 0, visited)
				usage.rgb = usage.rgb || formUsage.rgb
				usage.cmyk = usage.cmyk || formUsage.cmyk
				usage.gray = usage.gray || formUsage.gray
				usage.subScopeRGB = usage.subScopeRGB || formUsage.subScopeRGB
				usage.subScopeCMYK = usage.subScopeCMYK || formUsage.subScopeCMYK
				usage.subScopeGray = usage.subScopeGray || formUsage.subScopeGray
			}
		}
	}
	return usage
}

// scanShadingColorSpaces checks Shading resources for device colour space usage.
// Shading dictionaries contain a /ColorSpace entry that may reference a device CS.
func scanShadingColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage) {
	shadingDict, err := document.ResolveDict(ctx, doc, resources["Shading"])
	if err != nil || shadingDict == nil {
		return
	}

	for name, shadingObj := range shadingDict {
		if ctx.Err() != nil {
			return
		}

		sd, err := document.ResolveDict(ctx, doc, shadingObj)
		if err != nil || sd == nil {
			slog.Debug("compliance.color: cannot resolve Shading dict", "name", name, "error", err)
			continue
		}

		csObj, ok := sd["ColorSpace"]
		if !ok {
			continue
		}

		csName := resolveCSName(ctx, doc, csObj)
		if csName != "" {
			markDeviceCS(csName, usage)
		}
	}
}

// scanPatternColorSpaces checks Pattern resources for device colour space usage.
// Tiling patterns (Type 1) have their own content streams and resources that may
// use device-dependent colour spaces.
func scanPatternColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage, depth int, visited map[string]bool) {
	patternDict, err := document.ResolveDict(ctx, doc, resources["Pattern"])
	if err != nil || patternDict == nil {
		return
	}

	for _, patternObj := range patternDict {
		if ctx.Err() != nil {
			return
		}

		// Patterns can be streams (tiling) or dicts (shading patterns).
		stream, err := document.ResolveStream(ctx, doc, patternObj)
		if err != nil {
			// Shading pattern (Type 2) — check its /Shading dict for device CS.
			patDict, err := document.ResolveDict(ctx, doc, patternObj)
			if err == nil && patDict != nil {
				shadingObj, ok := patDict["Shading"]
				if ok {
					sd, err := document.ResolveDict(ctx, doc, shadingObj)
					if err == nil && sd != nil {
						if csObj, ok := sd["ColorSpace"]; ok {
							csN := resolveCSName(ctx, doc, csObj)
							markDeviceCS(csN, usage)
						}
					}
				}
			}
			continue
		}

		patternType, _ := document.ResolveInt(ctx, doc, stream.Dict["PatternType"])
		if patternType != 1 {
			continue
		}

		// Scan the tiling pattern's content stream as a Form XObject.
		formUsage := scanFormXObjectCS(ctx, doc, stream, depth, visited)
		usage.rgb = usage.rgb || formUsage.rgb
		usage.cmyk = usage.cmyk || formUsage.cmyk
		usage.gray = usage.gray || formUsage.gray
		usage.subScopeRGB = usage.subScopeRGB || formUsage.subScopeRGB
		usage.subScopeCMYK = usage.subScopeCMYK || formUsage.subScopeCMYK
		usage.subScopeGray = usage.subScopeGray || formUsage.subScopeGray
	}
}
