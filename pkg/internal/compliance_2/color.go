package compliance2

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
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

	var (
		firstProfileRef  ast.PdfObject
		firstProfileData []byte
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

		destProfileObj, ok := intent["DestOutputProfile"]
		if !ok {
			slog.Debug("compliance_2/color: OutputIntent has no DestOutputProfile", "index", i)
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, destProfileObj)
		if err != nil {
			slog.Debug("compliance_2/color: cannot resolve DestOutputProfile stream", "index", i, "error", err)
			continue
		}

		data, err := filter.DecodeStream(stream)
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

		// Check that all DestOutputProfile entries reference the same indirect object.
		if firstProfileRef == nil {
			firstProfileRef = destProfileObj
			firstProfileData = data
		} else {
			// Compare indirect references if available; otherwise compare raw data.
			if !sameIndirectRef(firstProfileRef, destProfileObj) && !bytesEqual(firstProfileData, data) {
				slog.Debug("compliance_2/color: non-matching DestOutputProfile", "index", i)
				*results = append(*results, failf(outputIntentConsistencyRule(part),
					"OutputIntent[%d]: DestOutputProfile does not match first entry", i)...)
			}
		}
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

		deviceCSUsed := scanForDeviceColorSpaces(ctx, doc, csDict, resources)

		// Also scan content stream operators for device CS usage.
		contentCSUsed := scanContentStreamDeviceCS(ctx, doc, page, i)
		deviceCSUsed.rgb = deviceCSUsed.rgb || contentCSUsed.rgb
		deviceCSUsed.cmyk = deviceCSUsed.cmyk || contentCSUsed.cmyk
		deviceCSUsed.gray = deviceCSUsed.gray || contentCSUsed.gray

		slog.Debug("compliance_2/color: device colour spaces found on page",
			"page", i,
			"usesRGB", deviceCSUsed.rgb,
			"usesCMYK", deviceCSUsed.cmyk,
			"usesGray", deviceCSUsed.gray,
		)

		// DeviceRGB: needs DefaultRGB or OutputIntent with RGB.
		if deviceCSUsed.rgb && !defaults.hasRGB {
			if !hasICC || iccColorSpace != "RGB " {
				slog.Debug("compliance_2/color: DeviceRGB used without coverage", "page", i)
				*results = append(*results, failf(deviceRGBRule(part), "page %d: DeviceRGB used without DefaultRGB or RGB OutputIntent", i+1)...)
			}
		}

		// DeviceCMYK: needs DefaultCMYK or OutputIntent with CMYK.
		if deviceCSUsed.cmyk && !defaults.hasCMYK {
			if !hasICC || iccColorSpace != "CMYK" {
				slog.Debug("compliance_2/color: DeviceCMYK used without coverage", "page", i)
				*results = append(*results, failf(deviceCMYKRule(part), "page %d: DeviceCMYK used without DefaultCMYK or CMYK OutputIntent", i+1)...)
			}
		}

		// DeviceGray: needs DefaultGray or any OutputIntent.
		if deviceCSUsed.gray && !defaults.hasGray {
			if !hasICC {
				slog.Debug("compliance_2/color: DeviceGray used without coverage", "page", i)
				*results = append(*results, failf(deviceGrayRule(part), "page %d: DeviceGray used without DefaultGray or OutputIntent", i+1)...)
			}
		}
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
func scanForDeviceColorSpaces(ctx context.Context, doc *document.Document, csDict ast.PdfDict, resources ast.PdfDict) deviceCSUsage {
	var usage deviceCSUsage

	// Check named colour spaces in the ColorSpace dictionary.
	for name, csObj := range csDict {
		if ctx.Err() != nil {
			return usage
		}
		csName := resolveCSName(ctx, doc, csObj)
		slog.Debug("compliance_2/color: checking named colour space", "name", name, "resolved", csName)
		markDeviceCS(csName, &usage)
	}

	// Check inline colour spaces in XObject, Font, Shading, and Pattern resources.
	visited := make(map[string]bool) //nolint:mnd // cycle detection for Form XObject recursion.
	scanXObjectColorSpaces(ctx, doc, resources, &usage, 0, visited)
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
func scanXObjectColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage, depth int, visited map[string]bool) {
	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return
	}

	for name, xobjObj := range xobjDict {
		if ctx.Err() != nil {
			return
		}

		stream, err := document.ResolveStream(ctx, doc, xobjObj)
		if err != nil {
			slog.Debug("compliance_2/color: cannot resolve XObject stream", "name", name, "error", err)
			continue
		}

		// Check the XObject's own /ColorSpace entry.
		csName, _ := document.ResolveName(ctx, doc, stream.Dict["ColorSpace"])
		if csName != "" {
			slog.Debug("compliance_2/color: XObject uses colour space", "name", name, "cs", csName)
			markDeviceCS(string(csName), usage)
		} else {
			// Array form colour space.
			csResolved := resolveCSName(ctx, doc, stream.Dict["ColorSpace"])
			if csResolved != "" {
				slog.Debug("compliance_2/color: XObject uses colour space (array)", "name", name, "cs", csResolved)
				markDeviceCS(csResolved, usage)
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
	data, err := filter.DecodeStream(stream)
	if err != nil {
		slog.Debug("compliance_2/color: cannot decode Form XObject stream", "error", err)
		return usage
	}

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
				}

				if usage.rgb && usage.cmyk && usage.gray {
					break
				}
			}
		}
	}

	// Check nested resources within the Form XObject.
	formRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil || formRes == nil {
		return usage
	}

	// Check ColorSpace dict in nested resources.
	formCSDict, err := document.ResolveDict(ctx, doc, formRes["ColorSpace"])
	if err == nil && formCSDict != nil {
		for csName, csObj := range formCSDict {
			if ctx.Err() != nil {
				return usage
			}
			resolved := resolveCSName(ctx, doc, csObj)
			slog.Debug("compliance_2/color: Form XObject nested colour space", "csName", csName, "resolved", resolved)
			markDeviceCS(resolved, &usage)
		}
	}

	// Recurse into nested XObjects.
	scanXObjectColorSpaces(ctx, doc, formRes, &usage, depth+1, visited)

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
	}
}

// scanContentStreamDeviceCS parses the content stream of a page and detects
// device-dependent colour space usage from operators:
//   - g/G → DeviceGray
//   - rg/RG → DeviceRGB
//   - k/K → DeviceCMYK
//   - cs/CS with operand "DeviceRGB", "DeviceCMYK", or "DeviceGray"
func scanContentStreamDeviceCS(ctx context.Context, doc *document.Document, page ast.PdfDict, pageIdx int) deviceCSUsage {
	var usage deviceCSUsage

	streamData, err := getPageContentBytes(ctx, doc, page)
	if err != nil {
		slog.Debug("compliance_2/color: failed to get content stream bytes for CS scan",
			"page", pageIdx, "error", err)
		return usage
	}

	if len(streamData) == 0 {
		return usage
	}

	ops, err := content.ParseStreamData(streamData)
	if err != nil {
		slog.Debug("compliance_2/color: failed to parse content stream for CS scan",
			"page", pageIdx, "error", err)
		return usage
	}

	slog.Debug("compliance_2/color: scanning content stream operators for device CS",
		"page", pageIdx, "opCount", len(ops))

	for idx, op := range ops {
		// Check context every ~100 operators.
		if idx%100 == 0 && ctx.Err() != nil { //nolint:mnd // per-spec checkpoint interval
			slog.Debug("compliance_2/color: context cancelled during content CS scan",
				"page", pageIdx, "operator_index", idx)
			return usage
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
		}

		// Short-circuit if all three are already found.
		if usage.rgb && usage.cmyk && usage.gray {
			slog.Debug("compliance_2/color: all device CS found, stopping early",
				"page", pageIdx)
			return usage
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
			// May be a shading pattern (dict, not stream). Shading patterns
			// reference a Shading dict which is handled by scanShadingColorSpaces.
			slog.Debug("compliance_2/color: Pattern is not a stream (likely shading pattern)", "name", name)
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

	pages := collectPages(ctx, doc, catalog)
	for i, page := range pages {
		if ctx.Err() != nil {
			return
		}

		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}

		scanSeparationDeviceNInResources(ctx, doc, resources, part, i, colorantAlternates, results)
	}

	slog.Debug("compliance_2/color: Separation/DeviceN check complete",
		"uniqueColorants", len(colorantAlternates))
}

// scanSeparationDeviceNInResources scans a resources dictionary for Separation
// and DeviceN colour space arrays and validates colorant-to-alternate consistency.
func scanSeparationDeviceNInResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, colorantAlternates map[string]string, results *[]Result) {
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
		if err != nil {
			continue
		}

		switch string(csType) {
		case "Separation":
			checkSeparationCS(ctx, doc, arr, part, pageIdx, string(name), colorantAlternates, results)
		case "DeviceN":
			checkDeviceNCS(ctx, doc, arr, part, pageIdx, string(name), colorantAlternates, results)
		}
	}
}

// checkSeparationCS validates a Separation colour space array:
// [/Separation /ColorantName /AlternateSpace /TintTransform]
func checkSeparationCS(ctx context.Context, doc *document.Document, arr ast.PdfArray, part int, pageIdx int, csName string, colorantAlternates map[string]string, results *[]Result) {
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

	for _, nameObj := range names {
		if ctx.Err() != nil {
			return
		}

		colorantName, err := document.ResolveName(ctx, doc, nameObj)
		if err != nil {
			continue
		}

		cName := string(colorantName)

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

	// Collect the OutputIntent ICC profile data for identity comparison (parts 2+).
	var outputIntentData []byte
	if part >= 2 {
		outputIntentData = getOutputIntentICCData(ctx, doc, catalog)
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

		checkICCBasedInResources(ctx, doc, resources, part, i, outputIntentData, results)
	}
}

// getOutputIntentICCData returns the decoded ICC profile data from the first
// OutputIntent with a DestOutputProfile, or nil if none exists.
func getOutputIntentICCData(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []byte {
	outputIntents, err := document.ResolveArray(ctx, doc, catalog["OutputIntents"])
	if err != nil {
		return nil
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

		stream, err := document.ResolveStream(ctx, doc, destObj)
		if err != nil {
			continue
		}

		data, err := filter.DecodeStream(stream)
		if err != nil {
			continue
		}

		slog.Debug("compliance_2/color: loaded OutputIntent ICC data for identity check", "len", len(data))
		return data
	}

	return nil
}

// checkICCBasedInResources scans a page's resources for ICCBased colour spaces
// and validates them.
func checkICCBasedInResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, outputIntentData []byte, results *[]Result) {
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

		data, err := filter.DecodeStream(stream)
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

		// Parts 2+: check whether the ICCBased profile is byte-identical to the
		// OutputIntent profile. Per clause 6.2.4.2, an ICCBased colour space
		// shall not use the same profile as the OutputIntent — it is redundant
		// and constitutes a violation.
		if part >= 2 && outputIntentData != nil && bytesEqual(data, outputIntentData) {
			slog.Debug("compliance_2/color: ICCBased profile is identical to OutputIntent (violation)",
				"page", pageIdx, "name", name,
			)
			*results = append(*results, failf(iccBasedIdentityRule(part),
				"page %d, ColorSpace %s: ICCBased profile is identical to OutputIntent", pageIdx+1, name)...)
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
