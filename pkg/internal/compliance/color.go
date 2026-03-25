package compliance

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkColorGroup validates colour-related PDF/A requirements:
//   - §6.2.2/6.2.3: OutputIntent ICC profiles
//   - §6.2.3.3 / §6.2.4.3: device-dependent colour space restrictions
//   - §6.2.3.2 / §6.2.4.2: ICCBased profile restrictions
func checkColorGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	part := pdfaPart(profile)
	if part == 0 {
		slog.Debug("compliance_2/color: skipping color checks for non-PDF/A profile", "profile", profile)
		return nil
	}

	slog.Debug("compliance_2/color: starting color checks", "profile", profile, "part", part)

	var results []Result

	// --- §6.2.2 / §6.2.3: OutputIntent ICC profile validation ---
	iccColorSpace, hasICC := checkOutputIntentICC(ctx, doc, catalog, part, &results)

	// --- §6.2.3.3 / §6.2.4.3: device-dependent colour space restrictions ---
	checkDeviceColorSpaces(ctx, doc, catalog, part, hasICC, iccColorSpace, &results)

	// --- §6.2.3.2 / §6.2.4.2: ICCBased profile restrictions ---
	checkICCBasedProfiles(ctx, doc, catalog, part, &results)

	// --- §6.2.4.4: Separation and DeviceN colour space consistency ---
	if part >= 2 {
		checkSeparationDeviceN(ctx, doc, catalog, part, &results)
	}

	slog.Debug("compliance_2/color: color checks complete", "violations", len(results))
	return results
}

// iccProfileHeader holds parsed fields from the 128-byte ICC profile header.
type iccProfileHeader struct {
	deviceClass string // bytes 12-16
	colorSpace  string // bytes 16-20
}

// iccHeaderSize is the minimum number of bytes needed to parse the ICC header.
const iccHeaderSize = 20

// parseICCHeader extracts the device class and colour space from raw ICC
// profile data.
func parseICCHeader(data []byte) (iccProfileHeader, bool) {
	if len(data) < iccHeaderSize {
		slog.Debug("compliance_2/color: ICC profile too short", "len", len(data))
		return iccProfileHeader{}, false
	}
	hdr := iccProfileHeader{
		deviceClass: string(data[12:16]),
		colorSpace:  string(data[16:20]),
	}
	slog.Debug("compliance_2/color: parsed ICC header",
		"deviceClass", hdr.deviceClass,
		"colorSpace", hdr.colorSpace,
	)
	return hdr, true
}

// validICCDeviceClasses are the allowed device classes for OutputIntent profiles.
var validICCDeviceClasses = map[string]bool{ //nolint:gochecknoglobals // constant set.
	"prtr": true,
	"mntr": true,
}

// validICCColorSpaces are the allowed colour spaces for OutputIntent profiles.
// Note: ICC colour space signatures are exactly 4 bytes; "RGB " has a trailing space.
var validICCColorSpaces = map[string]bool{ //nolint:gochecknoglobals // constant set.
	"GRAY": true,
	"RGB ": true,
	"CMYK": true,
}

// checkOutputIntentICC validates OutputIntent ICC profiles per §6.2.2 (part 1)
// or §6.2.3 (parts 2-4). It returns the ICC colour space string and whether a
// valid ICC profile was found.
func checkOutputIntentICC(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, results *[]Result) (string, bool) {
	slog.Debug("compliance_2/color: checking OutputIntent ICC profiles")

	outputIntents, err := document.ResolveArray(ctx, doc, catalog["OutputIntents"])
	if err != nil || len(outputIntents) == 0 {
		slog.Debug("compliance_2/color: no OutputIntents found", "error", err)
		return "", false
	}

	slog.Debug("compliance_2/color: found OutputIntents", "count", len(outputIntents))

	// §6.2.2 t2: if multiple OutputIntents exist with subtype GTS_PDFA1,
	// they must all reference the same DestOutputProfile. Track all
	// GTS_PDFA1 subtypes separately for consistency validation.
	var pdfaIntentCount int

	var (
		firstProfileRef  ast.PdfObject
		firstProfileData []byte
		firstColorSpace  string
		iccColorSpace    string
		hasValidICC      bool
	)

	for i, intentObj := range outputIntents {
		if ctx.Err() != nil {
			return iccColorSpace, hasValidICC
		}

		intent, err := document.ResolveDict(ctx, doc, intentObj)
		if err != nil || intent == nil {
			slog.Debug("compliance_2/color: cannot resolve OutputIntent dict", "index", i, "error", err)
			continue
		}

		// Track OutputIntent subtype (/S key).
		subtypeName, _ := document.ResolveName(ctx, doc, intent["S"])
		if subtypeName == "GTS_PDFA1" {
			pdfaIntentCount++
		}
		slog.Debug("compliance_2/color: OutputIntent subtype", "index", i, "S", subtypeName)

		destProfileObj, ok := intent["DestOutputProfile"]
		if !ok {
			// §6.2.2: OutputIntent without DestOutputProfile must have
			// OutputConditionIdentifier referencing a well-known registry.
			if _, hasOCI := intent["OutputConditionIdentifier"]; !hasOCI {
				slog.Debug("compliance_2/color: OutputIntent missing both DestOutputProfile and OutputConditionIdentifier", "index", i)
				*results = append(*results, failf(outputIntentICCRule(part),
					"OutputIntent[%d]: missing DestOutputProfile and OutputConditionIdentifier", i)...)
			}
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, destProfileObj)
		if err != nil {
			slog.Debug("compliance_2/color: cannot resolve DestOutputProfile stream", "index", i, "error", err)
			continue
		}

		data, err := doc.DecodeStream(stream)
		if err != nil {
			slog.Debug("compliance_2/color: cannot decode ICC profile stream", "index", i, "error", err)
			*results = append(*results, failf(outputIntentICCRule(part), "OutputIntent[%d]: failed to decode ICC profile", i)...)
			continue
		}

		hdr, ok := parseICCHeader(data)
		if !ok {
			*results = append(*results, failf(outputIntentICCRule(part), "OutputIntent[%d]: ICC profile header too short", i)...)
			continue
		}

		// Validate device class.
		if !validICCDeviceClasses[hdr.deviceClass] {
			slog.Debug("compliance_2/color: invalid ICC device class", "index", i, "deviceClass", hdr.deviceClass)
			*results = append(*results, failf(outputIntentICCRule(part),
				"OutputIntent[%d]: invalid device class %q (expected prtr or mntr)", i, hdr.deviceClass)...)
		}

		// Validate colour space.
		if !validICCColorSpaces[hdr.colorSpace] {
			slog.Debug("compliance_2/color: invalid ICC colour space", "index", i, "colorSpace", hdr.colorSpace)
			*results = append(*results, failf(outputIntentICCRule(part),
				"OutputIntent[%d]: invalid colour space %q (expected GRAY, RGB  or CMYK)", i, hdr.colorSpace)...)
		} else {
			iccColorSpace = hdr.colorSpace
			hasValidICC = true
		}

		// §6.2.2 t2 / §6.2.3 t2: multiple OutputIntents must be consistent.
		// Check that all DestOutputProfile entries reference the same indirect
		// object or have identical data, AND that all ICC colour spaces match.
		if firstProfileRef == nil {
			firstProfileRef = destProfileObj
			firstProfileData = data
			firstColorSpace = hdr.colorSpace
		} else {
			// Compare indirect references if available; otherwise compare raw data.
			if !sameIndirectRef(firstProfileRef, destProfileObj) && !bytesEqual(firstProfileData, data) {
				slog.Debug("compliance_2/color: non-matching DestOutputProfile", "index", i)
				*results = append(*results, failf(outputIntentConsistencyRule(part),
					"OutputIntent[%d]: DestOutputProfile does not match first entry", i)...)
			}

			// Flag inconsistent colour spaces across OutputIntents.
			if hdr.colorSpace != firstColorSpace {
				slog.Debug("compliance_2/color: inconsistent OutputIntent colour spaces",
					"index", i, "expected", firstColorSpace, "got", hdr.colorSpace)
				*results = append(*results, failf(outputIntentConsistencyRule(part),
					"OutputIntent[%d]: colour space %q does not match first entry %q", i, hdr.colorSpace, firstColorSpace)...)
			}
		}
	}

	// §6.2.2 t2: if multiple GTS_PDFA1 OutputIntents exist but none has a
	// valid ICC profile, flag the inconsistency.
	if pdfaIntentCount > 1 && !hasValidICC {
		slog.Debug("compliance_2/color: multiple GTS_PDFA1 OutputIntents but no valid ICC profile",
			"count", pdfaIntentCount)
		*results = append(*results, failf(outputIntentConsistencyRule(part),
			"%d GTS_PDFA1 OutputIntents but no valid DestOutputProfile found", pdfaIntentCount)...)
	}

	slog.Debug("compliance_2/color: OutputIntent ICC check done",
		"hasValidICC", hasValidICC,
		"iccColorSpace", iccColorSpace,
	)
	return iccColorSpace, hasValidICC
}

// sameIndirectRef returns true if both objects are indirect references to the
// same object number.
func sameIndirectRef(a, b ast.PdfObject) bool {
	refA, okA := a.(ast.PdfIndirectRef)
	refB, okB := b.(ast.PdfIndirectRef)
	if okA && okB {
		return refA.ObjectNum == refB.ObjectNum && refA.GenNum == refB.GenNum
	}
	return false
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// checkDeviceColorSpaces validates that device-dependent colour spaces
// (DeviceRGB, DeviceCMYK, DeviceGray) are only used when allowed.
func checkDeviceColorSpaces(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, hasICC bool, iccColorSpace string, results *[]Result) {
	slog.Debug("compliance_2/color: checking device-dependent colour spaces")

	pages := collectPages(ctx, doc, catalog)
	for i, page := range pages {
		if ctx.Err() != nil {
			return
		}

		resources := getResources(ctx, doc, page)
		if resources == nil {
			slog.Debug("compliance_2/color: page has no resources", "page", i)
			continue
		}

		defaults := collectDefaultColorSpaces(ctx, doc, resources)
		slog.Debug("compliance_2/color: page default colour spaces",
			"page", i,
			"hasDefaultRGB", defaults.hasRGB,
			"hasDefaultCMYK", defaults.hasCMYK,
			"hasDefaultGray", defaults.hasGray,
		)

		// Scan colour spaces used in the page resources.
		csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
		if err != nil {
			slog.Debug("compliance_2/color: cannot resolve ColorSpace dict", "page", i, "error", err)
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

		slog.Debug("compliance_2/color: device colour spaces found on page",
			"page", i,
			"usesRGB", deviceCSUsed.rgb,
			"usesCMYK", deviceCSUsed.cmyk,
			"usesGray", deviceCSUsed.gray,
		)

		// Check page transparency group colour space as additional coverage.
		// Per the spec, the "current transparency blending space" also provides
		// device CS coverage.
		blendingCS := pageBlendingColorSpace(ctx, doc, page)

		// DeviceRGB: needs DefaultRGB, OutputIntent with RGB, or RGB blending CS.
		if deviceCSUsed.rgb && !defaults.hasRGB {
			rgbCovered := (hasICC && iccColorSpace == "RGB ") || blendingCS == "RGB "
			if !rgbCovered {
				slog.Debug("compliance_2/color: DeviceRGB used without coverage", "page", i)
				*results = append(*results, failf(deviceRGBRule(part), "page %d: DeviceRGB used without DefaultRGB or RGB OutputIntent", i+1)...)
			}
		}

		// DeviceCMYK: needs DefaultCMYK, OutputIntent with CMYK, or CMYK blending CS.
		if deviceCSUsed.cmyk && !defaults.hasCMYK {
			cmykCovered := (hasICC && iccColorSpace == "CMYK") || blendingCS == "CMYK"
			if !cmykCovered {
				slog.Debug("compliance_2/color: DeviceCMYK used without coverage", "page", i)
				*results = append(*results, failf(deviceCMYKRule(part), "page %d: DeviceCMYK used without DefaultCMYK or CMYK OutputIntent", i+1)...)
			}
		}

		// DeviceGray: needs DefaultGray or any OutputIntent.
		// Unlike DeviceRGB/DeviceCMYK, the spec does NOT allow the
		// transparency blending CS to cover DeviceGray.
		if deviceCSUsed.gray && !defaults.hasGray {
			if !hasICC {
				slog.Debug("compliance_2/color: DeviceGray used without coverage", "page", i)
				*results = append(*results, failf(deviceGrayRule(part), "page %d: DeviceGray used without DefaultGray or OutputIntent", i+1)...)
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
		data, err := doc.DecodeStream(stream)
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

// defaultCS tracks which Default{RGB,CMYK,Gray} colour spaces are set.
type defaultCS struct {
	hasRGB  bool
	hasCMYK bool
	hasGray bool
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
		slog.Debug("compliance_2/color: cannot resolve ColorSpace dict for defaults", "error", err)
		return defaultCS{}
	}

	var ds defaultCS
	if _, ok := csDict["DefaultRGB"]; ok {
		slog.Debug("compliance_2/color: found DefaultRGB")
		ds.hasRGB = true
	}
	if _, ok := csDict["DefaultCMYK"]; ok {
		slog.Debug("compliance_2/color: found DefaultCMYK")
		ds.hasCMYK = true
	}
	if _, ok := csDict["DefaultGray"]; ok {
		slog.Debug("compliance_2/color: found DefaultGray")
		ds.hasGray = true
	}
	return ds
}

// deviceCSUsage tracks which device-dependent colour spaces are used.
type deviceCSUsage struct {
	rgb  bool
	cmyk bool
	gray bool
}

// scanForDeviceColorSpaces looks through page resources for uses of DeviceRGB,
// DeviceCMYK, and DeviceGray colour spaces.
func scanForDeviceColorSpaces(ctx context.Context, doc *document.Document, csDict ast.PdfDict, resources ast.PdfDict, referencedXObjects map[string]bool) deviceCSUsage {
	var usage deviceCSUsage

	// Check named colour spaces in the ColorSpace dictionary.
	for name, csObj := range csDict {
		if ctx.Err() != nil {
			return usage
		}
		csName := resolveCSName(ctx, doc, csObj)
		slog.Debug("compliance_2/color: checking named colour space", "name", name, "resolved", csName)
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
	visited := make(map[string]bool) //nolint:mnd // cycle detection for Form XObject recursion.
	scanXObjectColorSpaces(ctx, doc, resources, &usage, 0, visited, referencedXObjects)
	scanFontColorSpaces(ctx, doc, resources, &usage)
	scanShadingColorSpaces(ctx, doc, resources, &usage)
	scanPatternColorSpaces(ctx, doc, resources, &usage, 0, visited)

	return usage
}

// resolveCSName resolves a colour space object to its base name string.
func resolveCSName(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	// Direct name.
	if name, err := document.ResolveName(ctx, doc, obj); err == nil && name != "" {
		return string(name)
	}

	// Array form: [/name ...].
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err == nil && len(arr) > 0 {
		if name, err := document.ResolveName(ctx, doc, arr[0]); err == nil {
			return string(name)
		}
	}

	return ""
}

// markDeviceCS sets the appropriate flag in usage if csName is a device colour space.
func markDeviceCS(csName string, usage *deviceCSUsage) {
	switch csName {
	case "DeviceRGB":
		usage.rgb = true
	case "DeviceCMYK":
		usage.cmyk = true
	case "DeviceGray":
		usage.gray = true
	}
}

// maxFormXObjectDepth is the maximum recursion depth for scanning Form XObject
// content streams and their nested resources.
const maxFormXObjectDepth = 10

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
			slog.Debug("compliance_2/color: cannot resolve XObject stream", "name", name, "error", err)
			continue
		}

		// Check the XObject's own /ColorSpace entry.
		csObj := stream.Dict["ColorSpace"]
		csResolved := resolveCSName(ctx, doc, csObj)
		if csResolved != "" {
			slog.Debug("compliance_2/color: XObject uses colour space", "name", name, "cs", csResolved)
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
		}
	}
}

// scanFormXObjectCS decodes a Form XObject's content stream and scans for
// device-dependent colour space operators. It also checks nested resources
// (ColorSpace, XObject, Shading, Pattern) within the Form XObject.
func scanFormXObjectCS(ctx context.Context, doc *document.Document, stream ast.PdfStream, depth int, visited map[string]bool) deviceCSUsage {
	var usage deviceCSUsage

	if depth >= maxFormXObjectDepth {
		slog.Debug("compliance_2/color: Form XObject recursion depth limit reached", "depth", depth)
		return usage
	}

	// Cycle detection: use fmt.Sprintf("%p") on the dict map pointer as key.
	// PdfDict is a map type; each resolved instance has a unique header pointer.
	dictAddr := fmt.Sprintf("%p", stream.Dict)
	if visited[dictAddr] {
		slog.Debug("compliance_2/color: Form XObject already visited, skipping", "addr", dictAddr)
		return usage
	}
	visited[dictAddr] = true

	// Decode and scan the content stream for device CS operators.
	data, err := doc.DecodeStream(stream)
	if err != nil {
		slog.Debug("compliance_2/color: cannot decode Form XObject stream", "error", err)
		return usage
	}

	// Pre-scan raw bytes for inline image colour spaces.
	scanInlineImageCS(data, &usage)

	// Track Do operator targets so we only scan referenced nested XObjects.
	nestedXObjects := make(map[string]bool)

	if len(data) > 0 {
		ops, err := content.ParseStreamData(data)
		if err != nil {
			slog.Debug("compliance_2/color: cannot parse Form XObject content stream", "error", err)
		} else {
			for idx, op := range ops {
				if idx%100 == 0 && ctx.Err() != nil { //nolint:mnd // per-spec checkpoint interval
					return usage
				}

				switch op.Operator {
				case "g", "G":
					usage.gray = true
				case "rg", "RG":
					usage.rgb = true
				case "k", "K":
					usage.cmyk = true
				case "cs", "CS":
					if len(op.Operands) >= 1 {
						if name, ok := op.Operands[0].(ast.PdfName); ok {
							markDeviceCS(string(name), &usage)
						}
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
		for csName, csObj := range formCSDict {
			if ctx.Err() != nil {
				return usage
			}
			resolved := resolveCSName(ctx, doc, csObj)
			slog.Debug("compliance_2/color: Form XObject nested colour space", "csName", csName, "resolved", resolved)
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

	return usage
}

// scanFontColorSpaces checks Font resources for device colour space references
// (e.g., Type 3 fonts with their own Resources).
func scanFontColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage) {
	fontDict, err := document.ResolveDict(ctx, doc, resources["Font"])
	if err != nil || fontDict == nil {
		return
	}

	for name, fontObj := range fontDict {
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

		slog.Debug("compliance_2/color: checking Type3 font resources", "font", name)

		fontRes, err := document.ResolveDict(ctx, doc, fd["Resources"])
		if err != nil || fontRes == nil {
			continue
		}

		fontCSDict, err := document.ResolveDict(ctx, doc, fontRes["ColorSpace"])
		if err != nil {
			continue
		}

		for csName, csObj := range fontCSDict {
			resolved := resolveCSName(ctx, doc, csObj)
			slog.Debug("compliance_2/color: Type3 font colour space", "font", name, "csName", csName, "resolved", resolved)
			markDeviceCS(resolved, usage)
		}

		// Also scan CharProcs content streams for device CS operators.
		charProcs, err := document.ResolveDict(ctx, doc, fd["CharProcs"])
		if err != nil || charProcs == nil {
			continue
		}
		for cpName, cpObj := range charProcs {
			stream, err := document.ResolveStream(ctx, doc, cpObj)
			if err != nil {
				continue
			}
			streamData, err := doc.DecodeStream(stream)
			if err != nil {
				continue
			}
			ops, err := content.ParseStreamData(streamData)
			if err != nil {
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
			slog.Debug("compliance_2/color: scanned Type3 CharProc", "font", name, "charProc", cpName)
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
		slog.Debug("compliance_2/color: failed to get content stream bytes for CS scan",
			"page", pageIdx, "error", err)
		return usage, referencedXObjects
	}

	if len(streamData) == 0 {
		return usage, referencedXObjects
	}

	// Pre-scan raw bytes for inline image colour spaces. The content stream
	// parser skips BI...ID...EI blocks, so we detect inline image device CS
	// usage via byte pattern matching before parsing.
	scanInlineImageCS(streamData, &usage)

	ops, err := content.ParseStreamData(streamData)
	if err != nil {
		slog.Debug("compliance_2/color: failed to parse content stream for CS scan",
			"page", pageIdx, "error", err)
		return usage, referencedXObjects
	}

	slog.Debug("compliance_2/color: scanning content stream operators for device CS",
		"page", pageIdx, "opCount", len(ops))

	for idx, op := range ops {
		// Check context every ~100 operators.
		if idx%100 == 0 && ctx.Err() != nil { //nolint:mnd // per-spec checkpoint interval
			slog.Debug("compliance_2/color: context cancelled during content CS scan",
				"page", pageIdx, "operator_index", idx)
			return usage, referencedXObjects
		}

		switch op.Operator {
		case "g", "G":
			slog.Debug("compliance_2/color: DeviceGray operator found",
				"page", pageIdx, "operator", op.Operator)
			usage.gray = true
		case "rg", "RG":
			slog.Debug("compliance_2/color: DeviceRGB operator found",
				"page", pageIdx, "operator", op.Operator)
			usage.rgb = true
		case "k", "K":
			slog.Debug("compliance_2/color: DeviceCMYK operator found",
				"page", pageIdx, "operator", op.Operator)
			usage.cmyk = true
		case "cs", "CS":
			// cs/CS take a colour space name operand.
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					slog.Debug("compliance_2/color: cs/CS operator with operand",
						"page", pageIdx, "operator", op.Operator, "csName", string(name))
					markDeviceCS(string(name), &usage)
				}
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

// scanInlineImageCS scans raw content stream bytes for inline image colour
// spaces. Inline images (BI...ID...EI) are skipped by the content stream
// parser, so we detect device CS usage via byte pattern matching.
// Abbreviations: /CS /G = DeviceGray, /CS /RGB = DeviceRGB, /CS /CMYK = DeviceCMYK.
// Also handles /CS /DeviceRGB etc. and /ColorSpace variants.
func scanInlineImageCS(data []byte, usage *deviceCSUsage) {
	// Find all BI...ID blocks and check /CS parameter.
	for i := 0; i < len(data)-2; i++ {
		// Match "BI" preceded by whitespace (or start of data) followed by whitespace.
		if data[i] != 'B' || data[i+1] != 'I' {
			continue
		}
		if i > 0 && !isWhitespace(data[i-1]) {
			continue
		}
		if i+2 >= len(data) || !isWhitespace(data[i+2]) {
			continue
		}
		// Found BI — scan ahead for /CS or /ColorSpace before ID.
		end := bytes.Index(data[i:], []byte("\nID"))
		if end < 0 {
			end = bytes.Index(data[i:], []byte(" ID"))
		}
		if end < 0 {
			continue
		}
		block := data[i : i+end]
		// Look for /CS or /ColorSpace followed by a name.
		for _, key := range [][]byte{[]byte("/CS"), []byte("/ColorSpace")} {
			_, after, ok := bytes.Cut(block, key)
			if !ok {
				continue
			}
			// Skip whitespace after key, then read the name.
			rest := after
			for len(rest) > 0 && isWhitespace(rest[0]) {
				rest = rest[1:]
			}
			if len(rest) == 0 || rest[0] != '/' {
				continue
			}
			// Extract name token (up to whitespace or /).
			nameEnd := 1
			for nameEnd < len(rest) && !isWhitespace(rest[nameEnd]) && rest[nameEnd] != '/' {
				nameEnd++
			}
			csName := string(rest[1:nameEnd])
			switch csName {
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

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t' || b == '\x00' || b == '\x0c'
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
					continue
				}
				formUsage := scanFormXObjectCS(ctx, doc, subStream, 0, visited)
				usage.rgb = usage.rgb || formUsage.rgb
				usage.cmyk = usage.cmyk || formUsage.cmyk
				usage.gray = usage.gray || formUsage.gray
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
			slog.Debug("compliance_2/color: cannot resolve Shading dict", "name", name, "error", err)
			continue
		}

		csObj, ok := sd["ColorSpace"]
		if !ok {
			continue
		}

		csName := resolveCSName(ctx, doc, csObj)
		if csName != "" {
			slog.Debug("compliance_2/color: Shading uses colour space", "name", name, "cs", csName)
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

	for name, patternObj := range patternDict {
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
			slog.Debug("compliance_2/color: skipping non-tiling pattern", "name", name, "type", patternType)
			continue
		}

		slog.Debug("compliance_2/color: scanning tiling pattern", "name", name)

		// Scan the tiling pattern's content stream as a Form XObject.
		formUsage := scanFormXObjectCS(ctx, doc, stream, depth, visited)
		usage.rgb = usage.rgb || formUsage.rgb
		usage.cmyk = usage.cmyk || formUsage.cmyk
		usage.gray = usage.gray || formUsage.gray
	}
}

// checkSeparationDeviceN validates Separation and DeviceN colour space
// consistency per §6.2.4.4. It ensures that the same colorant name always maps
// to the same alternate colour space across the entire document.
func checkSeparationDeviceN(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, results *[]Result) {
	slog.Debug("compliance_2/color: checking Separation/DeviceN colour space consistency")

	// colorantAlternates tracks colorant name -> first alternate space seen.
	colorantAlternates := make(map[string]string)
	// colorantTintRefs tracks colorant name -> first tint transform indirect ref object number.
	// Used for tint transform consistency (same name must reference same object).
	colorantTintRefs := make(map[string]int)

	pages := collectPages(ctx, doc, catalog)
	for i, page := range pages {
		if ctx.Err() != nil {
			return
		}

		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}

		scanSeparationDeviceNInResources(ctx, doc, resources, part, i, colorantAlternates, colorantTintRefs, results)
	}

	slog.Debug("compliance_2/color: Separation/DeviceN check complete",
		"uniqueColorants", len(colorantAlternates))
}

// scanSeparationDeviceNInResources scans a resources dictionary for Separation
// and DeviceN colour space arrays and validates colorant-to-alternate consistency.
func scanSeparationDeviceNInResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, colorantAlternates map[string]string, colorantTintRefs map[string]int, results *[]Result) {
	visited := make(map[string]bool)
	scanSeparationDeviceNRecursive(ctx, doc, resources, part, pageIdx, colorantAlternates, colorantTintRefs, results, 0, visited)
}

// scanSeparationDeviceNRecursive scans resources (and Form XObject sub-resources)
// for Separation and DeviceN colour space arrays.
func scanSeparationDeviceNRecursive(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, colorantAlternates map[string]string, colorantTintRefs map[string]int, results *[]Result, depth int, visited map[string]bool) {
	if depth > maxFormXObjectDepth || ctx.Err() != nil {
		return
	}

	csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err == nil && csDict != nil {
		for name, csObj := range csDict {
			if ctx.Err() != nil {
				return
			}

			arr, err := document.ResolveArray(ctx, doc, csObj)
			if err != nil || len(arr) < 2 {
				continue
			}

			csType, err := document.ResolveName(ctx, doc, arr[0])
			if err != nil {
				continue
			}

			switch string(csType) {
			case "Separation":
				checkSeparationCS(ctx, doc, arr, part, pageIdx, string(name), colorantAlternates, colorantTintRefs, results)
			case "DeviceN":
				checkDeviceNCS(ctx, doc, arr, part, pageIdx, string(name), colorantAlternates, results)
			}
		}
	}

	// Recurse into Form XObject resources.
	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return
	}
	for _, xobjObj := range xobjDict {
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
			continue
		}
		subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		if subtype != "Form" {
			continue
		}
		subRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if subRes != nil {
			scanSeparationDeviceNRecursive(ctx, doc, subRes, part, pageIdx, colorantAlternates, colorantTintRefs, results, depth+1, visited)
		}
	}
}

// checkSeparationCS validates a Separation colour space array:
// [/Separation /ColorantName /AlternateSpace /TintTransform]
func checkSeparationCS(ctx context.Context, doc *document.Document, arr ast.PdfArray, part int, pageIdx int, csName string, colorantAlternates map[string]string, _ map[string]int, results *[]Result) {
	if len(arr) < 4 {
		slog.Debug("compliance_2/color: Separation array too short", "page", pageIdx, "cs", csName, "len", len(arr))
		return
	}

	colorantName, err := document.ResolveName(ctx, doc, arr[1])
	if err != nil {
		slog.Debug("compliance_2/color: cannot resolve Separation colorant name", "page", pageIdx, "cs", csName, "error", err)
		return
	}

	alternateName := resolveCSName(ctx, doc, arr[2])
	if alternateName == "" {
		slog.Debug("compliance_2/color: cannot resolve Separation alternate space", "page", pageIdx, "cs", csName)
		return
	}

	slog.Debug("compliance_2/color: found Separation colour space",
		"page", pageIdx, "cs", csName,
		"colorant", string(colorantName),
		"alternate", alternateName,
	)

	cName := string(colorantName)
	if prev, ok := colorantAlternates[cName]; ok {
		if prev != alternateName {
			slog.Debug("compliance_2/color: Separation colorant alternate mismatch",
				"colorant", cName, "prevAlternate", prev, "newAlternate", alternateName)
			*results = append(*results, failf(separationConsistencyRule(part),
				"page %d, ColorSpace %s: Separation colorant %q uses alternate %q but previously used %q",
				pageIdx+1, csName, cName, alternateName, prev)...)
		}
	} else {
		colorantAlternates[cName] = alternateName
	}
}

// checkDeviceNCS validates a DeviceN colour space array:
// [/DeviceN [/names...] /AlternateSpace /TintTransform ...]
func checkDeviceNCS(ctx context.Context, doc *document.Document, arr ast.PdfArray, part int, pageIdx int, csName string, colorantAlternates map[string]string, results *[]Result) {
	if len(arr) < 4 {
		slog.Debug("compliance_2/color: DeviceN array too short", "page", pageIdx, "cs", csName, "len", len(arr))
		return
	}

	names, err := document.ResolveArray(ctx, doc, arr[1])
	if err != nil {
		slog.Debug("compliance_2/color: cannot resolve DeviceN names array", "page", pageIdx, "cs", csName, "error", err)
		return
	}

	alternateName := resolveCSName(ctx, doc, arr[2])
	if alternateName == "" {
		slog.Debug("compliance_2/color: cannot resolve DeviceN alternate space", "page", pageIdx, "cs", csName)
		return
	}

	slog.Debug("compliance_2/color: found DeviceN colour space",
		"page", pageIdx, "cs", csName,
		"colorantCount", len(names),
		"alternate", alternateName,
	)

	// Collect colorant names from the names array.
	var colorantNames []string
	for _, nameObj := range names {
		if ctx.Err() != nil {
			return
		}

		colorantName, err := document.ResolveName(ctx, doc, nameObj)
		if err != nil {
			continue
		}

		cName := string(colorantName)
		colorantNames = append(colorantNames, cName)

		// DeviceN must not use reserved colorant names "All" or "None"
		// as tint transform targets (per §6.2.4.4).
		if cName == "All" || cName == "None" {
			slog.Debug("compliance_2/color: DeviceN uses reserved colorant name",
				"page", pageIdx, "cs", csName, "colorant", cName)
			*results = append(*results, failf(deviceNConsistencyRule(part),
				"page %d, ColorSpace %s: DeviceN uses reserved colorant name %q",
				pageIdx+1, csName, cName)...)
			continue
		}

		if prev, ok := colorantAlternates[cName]; ok {
			if prev != alternateName {
				slog.Debug("compliance_2/color: DeviceN colorant alternate mismatch",
					"colorant", cName, "prevAlternate", prev, "newAlternate", alternateName)
				*results = append(*results, failf(deviceNConsistencyRule(part),
					"page %d, ColorSpace %s: DeviceN colorant %q uses alternate %q but previously used %q",
					pageIdx+1, csName, cName, alternateName, prev)...)
			}
		} else {
			colorantAlternates[cName] = alternateName
		}
	}

	// §6.2.4.4 t1: For each spot colorant in DeviceN/NChannel, an entry in
	// the /Colorants dictionary (inside /Attributes) must be present.
	// Process colorants only: skip if all names are process colors.
	processColors := map[string]bool{
		"Cyan": true, "Magenta": true, "Yellow": true, "Black": true,
		"Red": true, "Green": true, "Blue": true,
	}

	var attrsDict ast.PdfDict
	if len(arr) >= 5 {
		attrsDict, _ = document.ResolveDict(ctx, doc, arr[4])
	}

	var colorantsDict ast.PdfDict
	if attrsDict != nil {
		colorantsDict, _ = document.ResolveDict(ctx, doc, attrsDict["Colorants"])
	}

	for _, cName := range colorantNames {
		if processColors[cName] || cName == "All" || cName == "None" {
			continue
		}
		// Spot colorant — must have a Colorants entry.
		if colorantsDict == nil {
			*results = append(*results, failf(separationConsistencyRule(part),
				"page %d, ColorSpace %s: DeviceN spot colorant %q has no Colorants dictionary",
				pageIdx+1, csName, cName)...)
			break // Report once per DeviceN CS, not per colorant.
		}
		if _, ok := colorantsDict[ast.PdfName(cName)]; !ok {
			*results = append(*results, failf(separationConsistencyRule(part),
				"page %d, ColorSpace %s: DeviceN spot colorant %q not found in Colorants dictionary",
				pageIdx+1, csName, cName)...)
		}
	}

	// §6.2.4.4 t2: Separation arrays in Colorants dictionaries must also be
	// consistent with all other Separation arrays with the same colorant name.
	if colorantsDict != nil {
		for cName, cObj := range colorantsDict {
			cArr, err := document.ResolveArray(ctx, doc, cObj)
			if err != nil || len(cArr) < 3 {
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
			} else {
				colorantAlternates[name] = cAlt
			}
		}
	}
}

// separationConsistencyRule returns the rule for Separation colour space
// consistency based on the PDF/A part.
func separationConsistencyRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_2_4_4_t01
	case 3:
		return Rule_19005_3_6_2_4_4_t01
	case 4:
		return Rule_19005_4_6_2_4_4_t01
	default:
		return Rule_19005_2_6_2_4_4_t01
	}
}

// deviceNConsistencyRule returns the rule for DeviceN colour space consistency
// based on the PDF/A part.
func deviceNConsistencyRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_2_4_4_t02
	case 3:
		return Rule_19005_3_6_2_4_4_t02
	case 4:
		return Rule_19005_4_6_2_4_4_t02
	default:
		return Rule_19005_2_6_2_4_4_t02
	}
}

// checkICCBasedProfiles validates ICCBased colour space profiles per
// §6.2.3.2 (part 1) or §6.2.4.2 (parts 2-4).
func checkICCBasedProfiles(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, results *[]Result) {
	slog.Debug("compliance_2/color: checking ICCBased profiles")

	// Collect the OutputIntent ICC profile info for identity comparison (parts 2+).
	var outputIntentRef ast.PdfIndirectRef
	var outputIntentHdr iccProfileHeader
	var outputIntentData []byte
	if part >= 2 {
		outputIntentRef, outputIntentHdr, outputIntentData = getOutputIntentICCInfo(ctx, doc, catalog)
	}

	pages := collectPages(ctx, doc, catalog)
	for i, page := range pages {
		if ctx.Err() != nil {
			return
		}

		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}

		checkICCBasedInResources(ctx, doc, resources, part, i, outputIntentRef, outputIntentHdr, outputIntentData, results)
	}
}

// getOutputIntentICCInfo returns the indirect reference, parsed header, decoded
// data, and decoded size of the ICC profile from the first OutputIntent with a
// DestOutputProfile.
func getOutputIntentICCInfo(ctx context.Context, doc *document.Document, catalog ast.PdfDict) (ast.PdfIndirectRef, iccProfileHeader, []byte) {
	outputIntents, err := document.ResolveArray(ctx, doc, catalog["OutputIntents"])
	if err != nil {
		return ast.PdfIndirectRef{}, iccProfileHeader{}, nil
	}

	for _, intentObj := range outputIntents {
		intent, err := document.ResolveDict(ctx, doc, intentObj)
		if err != nil || intent == nil {
			continue
		}

		destObj, ok := intent["DestOutputProfile"]
		if !ok {
			continue
		}

		var ref ast.PdfIndirectRef
		if r, ok := destObj.(ast.PdfIndirectRef); ok {
			ref = r
		}

		stream, err := document.ResolveStream(ctx, doc, destObj)
		if err != nil {
			continue
		}

		data, err := doc.DecodeStream(stream)
		if err != nil {
			continue
		}

		hdr, ok := parseICCHeader(data)
		if !ok {
			continue
		}

		slog.Debug("compliance_2/color: loaded OutputIntent ICC info for identity check",
			"objNum", ref.ObjectNum, "colorSpace", hdr.colorSpace, "size", len(data))
		return ref, hdr, data
	}

	return ast.PdfIndirectRef{}, iccProfileHeader{}, nil
}

// checkICCBasedInResources scans a page's resources for ICCBased colour spaces
// and validates them.
func checkICCBasedInResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, results *[]Result) {
	csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err != nil || csDict == nil {
		return
	}

	for name, csObj := range csDict {
		if ctx.Err() != nil {
			return
		}

		arr, err := document.ResolveArray(ctx, doc, csObj)
		if err != nil || len(arr) < 2 {
			continue
		}

		csType, err := document.ResolveName(ctx, doc, arr[0])
		if err != nil || csType != "ICCBased" {
			continue
		}

		slog.Debug("compliance_2/color: found ICCBased colour space", "page", pageIdx, "name", name)

		stream, err := document.ResolveStream(ctx, doc, arr[1])
		if err != nil {
			slog.Debug("compliance_2/color: cannot resolve ICCBased stream", "page", pageIdx, "name", name, "error", err)
			*results = append(*results, failf(iccBasedProfileRule(part),
				"page %d, ColorSpace %s: cannot resolve ICCBased profile stream", pageIdx+1, name)...)
			continue
		}

		// Validate /N matches the ICC profile colour space.
		nVal, err := document.ResolveInt(ctx, doc, stream.Dict["N"])
		if err != nil {
			slog.Debug("compliance_2/color: ICCBased missing /N", "page", pageIdx, "name", name)
			*results = append(*results, failf(iccBasedNRule(part),
				"page %d, ColorSpace %s: ICCBased profile missing /N entry", pageIdx+1, name)...)
			continue
		}

		data, err := doc.DecodeStream(stream)
		if err != nil {
			slog.Debug("compliance_2/color: cannot decode ICCBased stream", "page", pageIdx, "name", name, "error", err)
			*results = append(*results, failf(iccBasedProfileRule(part),
				"page %d, ColorSpace %s: cannot decode ICCBased profile", pageIdx+1, name)...)
			continue
		}

		hdr, ok := parseICCHeader(data)
		if !ok {
			*results = append(*results, failf(iccBasedProfileRule(part),
				"page %d, ColorSpace %s: ICC profile header too short", pageIdx+1, name)...)
			continue
		}

		expectedN := expectedComponentCount(hdr.colorSpace)
		if expectedN == 0 || int(nVal) != expectedN {
			slog.Debug("compliance_2/color: ICCBased /N mismatch",
				"page", pageIdx, "name", name,
				"N", nVal, "expectedN", expectedN,
				"colorSpace", hdr.colorSpace,
			)
			*results = append(*results, failf(iccBasedNRule(part),
				"page %d, ColorSpace %s: /N=%d does not match ICC colour space %q", pageIdx+1, name, nVal, hdr.colorSpace)...)
		}

		// Parts 2+: check whether the ICCBased profile is identical to the
		// OutputIntent profile. Per clause 6.2.4.2, this restriction applies
		// only when the OutputIntent has a CMYK destination profile.
		// Compare by (1) same indirect reference, or (2) byte-equal decoded data.
		if part >= 2 && len(outputIntentData) > 0 && outputIntentHdr.colorSpace == "CMYK" {
			identical := false
			// Same indirect reference.
			if ref, ok := arr[1].(ast.PdfIndirectRef); ok &&
				outputIntentRef.ObjectNum > 0 &&
				ref.ObjectNum == outputIntentRef.ObjectNum &&
				ref.GenNum == outputIntentRef.GenNum {
				identical = true
			}
			// Byte-equal decoded profile data (for different objects with same content).
			if !identical && hdr.colorSpace == "CMYK" && bytesEqual(data, outputIntentData) {
				identical = true
			}
			if identical {
				slog.Debug("compliance_2/color: ICCBased profile is identical to OutputIntent (violation)",
					"page", pageIdx, "name", name,
				)
				*results = append(*results, failf(iccBasedIdentityRule(part),
					"page %d, ColorSpace %s: ICCBased profile is identical to OutputIntent", pageIdx+1, name)...)
			}
		}
	}
}

// expectedComponentCount returns the expected /N value for a given ICC colour
// space signature.
func expectedComponentCount(colorSpace string) int {
	switch colorSpace {
	case "GRAY":
		return 1
	case "RGB ":
		return 3
	case "CMYK":
		return 4
	default:
		return 0
	}
}

// --- Rule selectors ---
//
// Each selector picks the correct Rule_ variable based on the PDF/A part.

func outputIntentICCRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_2_t01
	case 2:
		return Rule_19005_2_6_2_3_t01
	case 3:
		return Rule_19005_3_6_2_3_t01
	case 4:
		return Rule_19005_4_6_2_3_t01
	default:
		return Rule_19005_1_6_2_2_t01
	}
}

func outputIntentConsistencyRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_2_t02
	case 2:
		return Rule_19005_2_6_2_3_t02
	case 3:
		return Rule_19005_3_6_2_3_t02
	case 4:
		return Rule_19005_4_6_2_3_t02
	default:
		return Rule_19005_1_6_2_2_t02
	}
}

func deviceRGBRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_3_3_t01
	case 2:
		return Rule_19005_2_6_2_4_3_t02
	case 3:
		return Rule_19005_3_6_2_4_3_t02
	case 4:
		return Rule_19005_4_6_2_4_3_t02
	default:
		return Rule_19005_1_6_2_3_3_t01
	}
}

func deviceCMYKRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_3_3_t02
	case 2:
		return Rule_19005_2_6_2_4_3_t03
	case 3:
		return Rule_19005_3_6_2_4_3_t03
	case 4:
		return Rule_19005_4_6_2_4_3_t03
	default:
		return Rule_19005_1_6_2_3_3_t02
	}
}

func deviceGrayRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_3_3_t03
	case 2:
		return Rule_19005_2_6_2_4_3_t04
	case 3:
		return Rule_19005_3_6_2_4_3_t04
	case 4:
		return Rule_19005_4_6_2_4_3_t04
	default:
		return Rule_19005_1_6_2_3_3_t03
	}
}

func iccBasedProfileRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_3_2_t01
	case 2:
		return Rule_19005_2_6_2_4_2_t01
	case 3:
		return Rule_19005_3_6_2_4_2_t01
	case 4:
		return Rule_19005_4_6_2_4_2_t01
	default:
		return Rule_19005_1_6_2_3_2_t01
	}
}

func iccBasedNRule(part int) Rule {
	switch part {
	case 1:
		return Rule_19005_1_6_2_3_2_t02
	case 2:
		return Rule_19005_2_6_2_4_2_t01
	case 3:
		return Rule_19005_3_6_2_4_2_t01
	case 4:
		return Rule_19005_4_6_2_4_2_t01
	default:
		return Rule_19005_1_6_2_3_2_t02
	}
}

// iccBasedIdentityRule returns the rule for ICCBased profiles identical to
// the OutputIntent.
func iccBasedIdentityRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_2_4_2_t02
	case 3:
		return Rule_19005_3_6_2_4_2_t02
	case 4:
		return Rule_19005_4_6_2_4_2_t03
	default:
		// Part 1 does not have this rule; return part 2 as fallback.
		return Rule_19005_2_6_2_4_2_t02
	}
}
