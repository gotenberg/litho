package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// scanForDeviceColorSpaces looks through page resources for uses of DeviceRGB,
// DeviceCMYK, and DeviceGray colour spaces.
func scanForDeviceColorSpaces(ctx context.Context, doc *document.Document, csDict ast.PdfDict, resources ast.PdfDict, referencedXObjects map[string]bool, part int) deviceCSUsage {
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
	scanXObjectColorSpaces(ctx, doc, resources, &usage, 0, visited, referencedXObjects, part)
	scanFontColorSpaces(ctx, doc, resources, &usage)
	scanShadingColorSpaces(ctx, doc, resources, &usage)
	scanPatternColorSpaces(ctx, doc, resources, &usage, 0, visited, part)

	return usage
}

// scanXObjectColorSpaces checks XObject resources for device colour space usage.
// For Form XObjects, it recurses into their content streams and nested resources.
func scanXObjectColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage, depth int, visited map[string]bool, referencedNames map[string]bool, part int) {
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
			doc.Logger.Debug("compliance.color: cannot resolve XObject stream", "name", name, "error", err)
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
			formUsage := scanFormXObjectCS(ctx, doc, stream, depth, visited, part)
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
func scanFormXObjectCS(ctx context.Context, doc *document.Document, stream ast.PdfStream, depth int, visited map[string]bool, part int) deviceCSUsage {
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
		doc.Logger.Debug("compliance.color: cannot decode Form XObject stream", "error", err)
		return usage
	}

	// Track Do operator targets so only referenced nested XObjects get scanned.
	nestedXObjects := make(map[string]bool)
	formFillCSSet := false
	formStrokeCSSet := false

	if len(data) > 0 {
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot parse Form XObject content stream", "error", err)
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

	// Check ColorSpace dict in nested resources, including Indexed base
	// spaces and Separation/DeviceN alternates. Device CS reached this way is
	// covered by the Form XObject's own DefaultCS or (part 4) Group CS, both
	// applied at the end of this function.
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

			// Check Separation/DeviceN alternate space (arr[2]). The colorant
			// tint transform targets this alternate; an uncovered device
			// alternate fails ISO 19005-1 §6.2.3.3 / 19005-2/3 §6.2.4.3. The
			// page-level scan already marks these via markDeviceCS; the form
			// scan must too. End-of-function DefaultCS/Group suppression applies.
			if resolved == "Separation" || resolved == "DeviceN" {
				if arr, err := document.ResolveArray(ctx, doc, csObj); err == nil && len(arr) >= 3 {
					markDeviceCS(resolveCSName(ctx, doc, arr[2]), &usage)
				}
			}
		}
	}

	// Recurse into nested XObjects (only those referenced by Do operators).
	scanXObjectColorSpaces(ctx, doc, formRes, &usage, depth+1, visited, nestedXObjects, part)

	// Check nested Shading and Pattern resources.
	scanShadingColorSpaces(ctx, doc, formRes, &usage)
	scanPatternColorSpaces(ctx, doc, formRes, &usage, depth+1, visited, part)

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
	// independent blending colour space that covers the device CS. Only
	// PDF/A-4 (ISO 19005-4, PDF 2.0) lets a transparency-group blending
	// colour space cover device colour usage inside the group. Under
	// PDF/A-2 and PDF/A-3 (ISO 19005-2/-3, PDF 1.7) device-colour
	// conformance depends solely on the document OutputIntent destination
	// colour space, so a CalRGB form /Group does not cover DeviceRGB when
	// the OutputIntent is CMYK and §6.2.4.3 still fires. Mirror the
	// page-level part==4 gate.
	if part == 4 {
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
	if !iVal {
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
		// CharProcs are a sub-scope like Form XObjects: only the FONT's
		// own /Resources DefaultCS covers their operators, never the
		// page's (ISO 32000-2 §8.6.5.7). A charproc rg therefore still
		// trips §6.2.4.3 even when the page defines a DefaultRGB, since
		// that page-level default does not reach into the font sub-scope.
		var charProcUsage deviceCSUsage
		charProcs, err := document.ResolveDict(ctx, doc, fd["CharProcs"])
		if err == nil && charProcs != nil {
			for _, cpObj := range charProcs {
				stream, err := document.ResolveStream(ctx, doc, cpObj)
				if err != nil {
					doc.Logger.Debug("compliance.color: cannot resolve CharProc stream", "error", err)
					continue
				}
				streamData, err := doc.DecodeStream(ctx, stream)
				if err != nil {
					doc.Logger.Debug("compliance.color: cannot decode CharProc stream", "error", err)
					continue
				}
				ops, err := content.ParseStreamData(ctx, streamData)
				if err != nil {
					doc.Logger.Debug("compliance.color: cannot parse CharProc stream", "error", err)
					continue
				}
				for _, op := range ops {
					switch op.Operator {
					case "g", "G":
						charProcUsage.gray = true
					case "rg", "RG":
						charProcUsage.rgb = true
					case "k", "K":
						charProcUsage.cmyk = true
					case "cs", "CS":
						if len(op.Operands) >= 1 {
							if csN, ok := op.Operands[0].(ast.PdfName); ok {
								markDeviceCS(string(csN), &charProcUsage)
							}
						}
					}
				}
			}
		}

		// Suppress charproc usage covered by the font's own Resources
		// DefaultCS, then record the remainder as sub-scope usage that a
		// parent DefaultCS cannot cover.
		if fontRes, err := document.ResolveDict(ctx, doc, fd["Resources"]); err == nil && fontRes != nil {
			fontDefaults := collectDefaultColorSpaces(ctx, doc, fontRes)
			if fontDefaults.hasRGB {
				charProcUsage.rgb = false
			}
			if fontDefaults.hasCMYK {
				charProcUsage.cmyk = false
			}
			if fontDefaults.hasGray {
				charProcUsage.gray = false
			}
		}
		usage.subScopeRGB = usage.subScopeRGB || charProcUsage.rgb
		usage.subScopeCMYK = usage.subScopeCMYK || charProcUsage.cmyk
		usage.subScopeGray = usage.subScopeGray || charProcUsage.gray
		usage.rgb = usage.rgb || charProcUsage.rgb
		usage.cmyk = usage.cmyk || charProcUsage.cmyk
		usage.gray = usage.gray || charProcUsage.gray

		// Scan font's own ColorSpace resource dict (if present).
		fontRes, err := document.ResolveDict(ctx, doc, fd["Resources"])
		if err != nil || fontRes == nil {
			continue
		}
		fontCSDict, err := document.ResolveDict(ctx, doc, fontRes["ColorSpace"])
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve font ColorSpace dict", "error", err)
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

	streamData, err := resolvePageContentBytes(ctx, doc, page)
	if err != nil {
		doc.Logger.Debug("compliance.color: failed to get content stream bytes for CS scan",
			"page", pageIdx, "error", err)
		return usage, referencedXObjects
	}

	if len(streamData) == 0 {
		return usage, referencedXObjects
	}

	ops, err := content.ParseStreamData(ctx, streamData)
	if err != nil {
		doc.Logger.Debug("compliance.color: failed to parse content stream for CS scan",
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
// 3D annotations also carry a /3DD stream whose own /ColorSpace and
// /Resources reach device-colour surfaces that the /AP tree does not cover.
func scanAnnotationAppearanceCS(ctx context.Context, doc *document.Document, page ast.PdfDict, part int) deviceCSUsage {
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

		// 3D annotations embed their colour dependencies in the /3DD
		// stream dict (parallel to how image XObjects expose them). A
		// DeviceRGB reference there is invisible to the /AP walk.
		if annotSubtype, err := document.ResolveName(ctx, doc, annot["Subtype"]); err == nil && annotSubtype == "3D" {
			scan3DAnnotationCS(ctx, doc, annot, &usage)
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
				formUsage := scanFormXObjectCS(ctx, doc, stream, 0, visited, part)
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
					doc.Logger.Debug("compliance.color: cannot resolve sub-XObject stream", "error", err)
					continue
				}
				formUsage := scanFormXObjectCS(ctx, doc, subStream, 0, visited, part)
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

// scan3DAnnotationCS walks a /Subtype /3D annotation for device colour
// space usage. The 3D annotation dict references a 3D stream via /3DD
// (or an /3DV default view); that stream's own dict may declare
// /ColorSpace directly (image-XObject style) or embed a /Resources
// dictionary whose /ColorSpace entry carries the actual bindings
// (form-XObject style). Both shapes land at top-scope device colour
// usage, since 3D content renders into the page area.
func scan3DAnnotationCS(ctx context.Context, doc *document.Document, annot ast.PdfDict, usage *deviceCSUsage) {
	// Primary: /3DD points to the 3D stream that backs the annotation.
	if stream, err := document.ResolveStream(ctx, doc, annot["3DD"]); err == nil {
		mark3DStreamCS(ctx, doc, stream, usage)
	}
	// Secondary: /3DV default view can name its own resources.
	if view, err := document.ResolveDict(ctx, doc, annot["3DV"]); err == nil && view != nil {
		if stream, err := document.ResolveStream(ctx, doc, view["3DD"]); err == nil {
			mark3DStreamCS(ctx, doc, stream, usage)
		}
	}
}

// mark3DStreamCS checks a 3D stream dict for device colour space
// references, both in the stream's own /ColorSpace entry and in any
// nested /Resources/ColorSpace dictionary.
func mark3DStreamCS(ctx context.Context, doc *document.Document, stream ast.PdfStream, usage *deviceCSUsage) {
	if csObj, ok := stream.Dict["ColorSpace"]; ok {
		markDeviceCS(resolveCSName(ctx, doc, csObj), usage)
	}
	resources, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil || resources == nil {
		return
	}
	csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err != nil || csDict == nil {
		return
	}
	for _, csObj := range csDict {
		markDeviceCS(resolveCSName(ctx, doc, csObj), usage)
	}
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
			doc.Logger.Debug("compliance.color: cannot resolve Shading dict", "name", name, "error", err)
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
func scanPatternColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage, depth int, visited map[string]bool, part int) {
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
			// Shading pattern (Type 2): check its /Shading dict for device CS.
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
		formUsage := scanFormXObjectCS(ctx, doc, stream, depth, visited, part)
		usage.rgb = usage.rgb || formUsage.rgb
		usage.cmyk = usage.cmyk || formUsage.cmyk
		usage.gray = usage.gray || formUsage.gray
		usage.subScopeRGB = usage.subScopeRGB || formUsage.subScopeRGB
		usage.subScopeCMYK = usage.subScopeCMYK || formUsage.subScopeCMYK
		usage.subScopeGray = usage.subScopeGray || formUsage.subScopeGray
	}
}
