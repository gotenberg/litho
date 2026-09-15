package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// scanForDeviceColorSpaces looks through page resources for uses of DeviceRGB,
// DeviceCMYK, and DeviceGray colour spaces.
func scanForDeviceColorSpaces(ctx context.Context, doc *document.Document, csDict ast.PdfDict, resources ast.PdfDict, referencedXObjects map[string]bool, used *usedResources, inherited map[string]inheritedCS, part int, contentCSNames map[string]bool) deviceCSUsage {
	var usage deviceCSUsage

	// A named colour space in /Resources/ColorSpace counts only when it is
	// actually referenced: selected by a cs/CS operator, or named as the
	// /ColorSpace of a painted image or shading. A colour space that is merely
	// DECLARED but never referenced is not "used" and does not trip §6.2.4.3.
	// veraPDF, the PDF/A reference validator, is usage-driven here, so keying
	// off declarations produced false positives on real documents that carry
	// unused DeviceGray entries in their resource dictionaries.
	usedNames := collectReferencedCSNames(ctx, doc, resources, referencedXObjects, contentCSNames)
	for key, csObj := range csDict {
		if ctx.Err() != nil {
			return usage
		}
		if !usedNames[string(key)] {
			continue
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
	scanXObjectColorSpaces(ctx, doc, resources, &usage, 0, visited, referencedXObjects, inherited, part)
	scanFontColorSpaces(ctx, doc, resources, &usage, used)
	scanShadingColorSpaces(ctx, doc, resources, &usage, used)
	scanPatternColorSpaces(ctx, doc, resources, &usage, 0, visited, used, part)

	return usage
}

// collectReferencedCSNames returns the set of /Resources/ColorSpace keys that
// are actually referenced from a content scope: names selected by cs/CS
// (contentCSNames), plus names used directly as the /ColorSpace of a painted
// (Do-referenced) image or a shading. A colour space named this way resolves
// against the resource ColorSpace dictionary, so its key must be treated as
// used even when no cs/CS operator selects it.
func collectReferencedCSNames(ctx context.Context, doc *document.Document, resources ast.PdfDict, referencedXObjects, contentCSNames map[string]bool) map[string]bool {
	used := make(map[string]bool, len(contentCSNames))
	for n := range contentCSNames {
		used[n] = true
	}

	// Images invoked by Do whose /ColorSpace is a bare name reference.
	if xobjDict, _ := document.ResolveDict(ctx, doc, resources["XObject"]); xobjDict != nil {
		for name, obj := range xobjDict {
			if referencedXObjects != nil && !referencedXObjects[string(name)] {
				continue
			}
			stream, err := document.ResolveStream(ctx, doc, obj)
			if err != nil {
				continue
			}
			if n, err := document.ResolveName(ctx, doc, stream.Dict["ColorSpace"]); err == nil && n != "" {
				used[string(n)] = true
			}
		}
	}

	// Shadings whose /ColorSpace is a bare name reference.
	if shDict, _ := document.ResolveDict(ctx, doc, resources["Shading"]); shDict != nil {
		for _, sh := range shDict {
			sd := resolveShadingDict(ctx, doc, sh)
			if sd == nil {
				continue
			}
			if n, err := document.ResolveName(ctx, doc, sd["ColorSpace"]); err == nil && n != "" {
				used[string(n)] = true
			}
		}
	}

	return used
}

// scanXObjectColorSpaces checks XObject resources for device colour space usage.
// For Form XObjects, it recurses into their content streams and nested resources.
func scanXObjectColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage, depth int, visited map[string]bool, referencedNames map[string]bool, inherited map[string]inheritedCS, part int) {
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

		// An image mask carries no colour space of its own: ISO 32000-1, 8.9.6.2
		// paints its unmasked areas with the current fill colour, so drawing one
		// selects a colour space exactly as "f" does. When no cs/rg/g/k set the
		// fill space before the Do, that colour is the initial DeviceGray of
		// 8.6.8. pdfjs issue7308.pdf draws a JBIG2 image mask on a page that
		// sets no colour at all, which veraPDF reports under ISO 19005-2
		// §6.2.4.3 t4 and litho counted as no colour use whatever.
		if mask, err := document.ResolveBool(ctx, doc, stream.Dict["ImageMask"]); err == nil && mask {
			if !inheritedCSFor(inherited, string(name)).fill {
				usage.gray = true
			}
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
			formUsage := scanFormXObjectCS(ctx, doc, stream, depth, visited, inheritedCSFor(inherited, string(name)), part, resources)
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
//
// entryCS is the colour-space state the scope inherits (ISO 32000-1 §8.10.2):
// the state observed at the invoking Do, or [inheritedCSDefault] when this scope
// is a tiling pattern or an appearance stream that no Do invokes.
func scanFormXObjectCS(ctx context.Context, doc *document.Document, stream ast.PdfStream, depth int, visited map[string]bool, entryCS inheritedCS, part int, parentRes ast.PdfDict) deviceCSUsage {
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

	// Track Do targets and cs/CS names so only referenced nested XObjects and
	// declared colour spaces are treated as used.
	nestedXObjects := make(map[string]bool)
	formCSNames := make(map[string]bool)
	nestedInherited := make(map[string]inheritedCS)
	nestedUsed := newUsedResources()

	if len(data) > 0 {
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot parse Form XObject content stream", "error", err)
		} else {
			// Check inline image colour spaces from parsed BI operations.
			scanInlineImageCSOps(ops, &usage, formCSNames)
			scanContentColorOps(ctx, ops, &usage, nestedXObjects, formCSNames, nestedUsed, nestedInherited, entryCS)
		}
	}

	// A device transparency-group blending colour space is itself a device-colour
	// usage coverable only by a matching OutputIntent (veraPDF reports PDDeviceRGB
	// at context /Group/colorSpace), independent of the form's content or its
	// resources. Record it as a sub-scope usage so neither DefaultCS suppression
	// nor a missing /Resources early-return can drop it.
	switch pageTransparencyGroupCSName(ctx, doc, stream.Dict) {
	case "DeviceRGB":
		usage.rgb, usage.subScopeRGB = true, true
	case "DeviceCMYK":
		usage.cmyk, usage.subScopeCMYK = true, true
	case "DeviceGray":
		usage.gray, usage.subScopeGray = true, true
	}

	// Check nested resources within the Form XObject.
	//
	// ISO 32000-1, 8.10.1 makes /Resources optional: a form XObject that omits
	// it draws on the resource dictionary of the content stream that invoked
	// it, so the names its stream references still resolve. Returning early
	// here left every nested XObject, shading and pattern of such a form
	// unscanned. pdfjs issue1055r.pdf draws a form with no /Resources whose
	// stream invokes a second form by a name only the page declares, and that
	// one fills with an "rg" operator; veraPDF reports ISO 19005-2 §6.2.4.3 t2
	// at .../xObject[0]/contentStream[0]/operators[3]/fillCS[0].
	formRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil || formRes == nil {
		formRes = parentRes
	}
	if formRes == nil {
		return usage
	}

	// Check ColorSpace dict in nested resources, including Indexed base
	// spaces and Separation/DeviceN alternates. Device CS reached this way is
	// covered by the Form XObject's own DefaultCS or (part 4) Group CS, both
	// applied at the end of this function.
	formCSDict, err := document.ResolveDict(ctx, doc, formRes["ColorSpace"])
	if err == nil && formCSDict != nil {
		formDefaults := collectDefaultColorSpaces(ctx, doc, formRes)
		usedFormNames := collectReferencedCSNames(ctx, doc, formRes, nestedXObjects, formCSNames)
		for key, csObj := range formCSDict {
			if ctx.Err() != nil {
				return usage
			}
			if !usedFormNames[string(key)] {
				continue
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
	scanXObjectColorSpaces(ctx, doc, formRes, &usage, depth+1, visited, nestedXObjects, nestedInherited, part)

	// Check nested Shading and Pattern resources.
	scanShadingColorSpaces(ctx, doc, formRes, &usage, nestedUsed)
	scanPatternColorSpaces(ctx, doc, formRes, &usage, depth+1, visited, nestedUsed, part)

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
func scanFontColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage, used *usedResources) {
	fontDict, err := document.ResolveDict(ctx, doc, resources["Font"])
	if err != nil || fontDict == nil {
		return
	}

	for name, fontObj := range fontDict {
		if ctx.Err() != nil {
			return
		}

		// A Type3 font's CharProcs paint only once a Tf has selected the font.
		// A font merely declared in /Resources/Font draws nothing.
		if used != nil && !used.fonts[string(name)] {
			continue
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
				// A glyph description that opens with d1 is a stencil mask painted
				// in the current colour: any colour operator in it shall be ignored
				// (ISO 32000-1, 9.6.5, Table 113), so it uses no colour space of its
				// own. veraPDF counts none for it, and does count them in a d0
				// glyph. Measured.
				if len(ops) > 0 && ops[0].Operator == "d1" {
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
// device-dependent colour space usage from explicit colour operators. It also
// returns the names of XObjects invoked by Do, the colour-space names selected
// by cs/CS, so the resource scan can distinguish colour spaces that are
// actually referenced from ones merely declared in /Resources/ColorSpace, and
// the colour-space state in effect at each Do, which a Form XObject inherits.
// scanContentStreamDeviceCS surveys one page's already-parsed operations.
//
// It takes the operations rather than the page, because three separate check
// groups (colour, graphics, XObject) each survey every page and each used to
// resolve and re-parse the same content stream to do it. The page's operations
// are parsed once by [pageCtx.parsedOps] and shared from there. A page whose
// content is missing or will not parse yields no operations, which is the same
// empty survey the resolve and parse failures returned before.
func scanContentStreamDeviceCS(ctx context.Context, ops []content.Operation) (deviceCSUsage, map[string]bool, map[string]bool, *usedResources, map[string]inheritedCS) {
	var usage deviceCSUsage
	referencedXObjects := make(map[string]bool)
	referencedCSNames := make(map[string]bool)
	used := newUsedResources()
	inherited := make(map[string]inheritedCS)

	if len(ops) == 0 {
		return usage, referencedXObjects, referencedCSNames, used, inherited
	}

	// Check inline image colour spaces from parsed BI operations.
	scanInlineImageCSOps(ops, &usage, referencedCSNames)
	scanContentColorOps(ctx, ops, &usage, referencedXObjects, referencedCSNames, used, inherited, inheritedCSDefault())

	return usage, referencedXObjects, referencedCSNames, used, inherited
}

// Text rendering modes that add no marks to the page. Per ISO 32000-1,
// Table 106, mode 3 neither fills nor strokes and mode 7 only adds the glyphs
// to the clipping path, so text shown in either selects no colour space.
const (
	textRenderInvisible = 3
	textRenderClip      = 7
)

// usedResources holds the resource-dictionary keys a content stream actually
// paints with, per category. ISO 19005-2 §6.2.4.3 is usage-driven: a resource
// merely declared in /Resources paints nothing and so uses no colour space, which
// veraPDF was measured to agree with for patterns, shadings and Type3 fonts
// alike.
type usedResources struct {
	patterns map[string]bool
	shadings map[string]bool
	fonts    map[string]bool
}

func newUsedResources() *usedResources {
	return &usedResources{
		patterns: make(map[string]bool),
		shadings: make(map[string]bool),
		fonts:    make(map[string]bool),
	}
}

// inheritedCS is the colour-space selection state a nested scope inherits from
// the scope that invokes it: whether a fill and a stroke space were selected at
// that point (ISO 32000-1 §8.10.2).
type inheritedCS struct {
	fill, stroke bool
}

// recordInheritedCS notes the state in effect at one Do. An XObject invoked
// more than once inherits a different state each time, and veraPDF reports the
// invocation that runs with no space selected, so the states combine by AND:
// the nested scope counts a space as selected only if every invocation selected
// one.
func recordInheritedCS(states map[string]inheritedCS, name string, fillCSSet, strokeCSSet bool) {
	if states == nil {
		return
	}
	if prev, seen := states[name]; seen {
		fillCSSet = fillCSSet && prev.fill
		strokeCSSet = strokeCSSet && prev.stroke
	}
	states[name] = inheritedCS{fill: fillCSSet, stroke: strokeCSSet}
}

// inheritedCSDefault is the entry state of a scope that inherits nothing: a page
// content stream, which begins in the default graphics state with no colour
// space selected (ISO 32000-1 §8.4.1), and equally a tiling pattern or an
// annotation appearance stream. Neither of the latter two is invoked by a Do,
// and veraPDF was measured to start both in the default state rather than in the
// state selected where the pattern or annotation is painted: a bare sc/scn in
// either reports the initial DeviceGray on both tools.
func inheritedCSDefault() inheritedCS {
	return inheritedCS{}
}

// inheritedCSFor reports the state a nested scope starts from, falling back to
// [inheritedCSDefault] for an XObject with no recorded Do.
func inheritedCSFor(states map[string]inheritedCS, name string) inheritedCS {
	if state, ok := states[name]; ok {
		return state
	}
	return inheritedCSDefault()
}

// scanContentColorOps records device colour usage from a content stream:
// explicit colour operators (g/G, rg/RG, k/K, cs/CS with a direct device
// name), the colour-space names selected by cs/CS (referencedCSNames), and the
// XObjects invoked by Do (referencedXObjects). A painting operator that runs
// with no colour space yet selected uses the initial graphics-state colour
// space, DeviceGray; veraPDF flags that implicit default use, so it counts as
// DeviceGray usage here too (ISO 19005-2 §6.2.4.3).
//
// Only cs/CS and the shorthands g/G, rg/RG, k/K select a colour space. sc/scn
// and SC/SCN set components within the space cs/CS already selected (ISO
// 32000-1 §8.6.8, Table 74), so a bare "0.3 SC" leaves the initial DeviceGray
// in place and veraPDF reports DeviceGray at the painting operator that
// follows it.
//
// entry carries the colour spaces already selected when this scope begins. A
// page content stream begins in the default graphics state with none selected
// (ISO 32000-1 §8.4.1). A Form XObject instead inherits the graphics state in
// effect at the invoking Do (ISO 32000-1 §8.10.2), so it begins from the state
// recorded there, which is what lets a bare sc/scn name a component of an
// inherited space without being read as a DeviceGray use.
//
// used collects the resource keys the stream actually paints with, so each
// resource walk can skip entries that are merely declared. See [usedResources].
//
// inheritedXObjectCS records, per XObject name, the state in effect at each Do
// that invokes it, so a nested form scan starts from the observed state.
func scanContentColorOps(ctx context.Context, ops []content.Operation, usage *deviceCSUsage, referencedXObjects, referencedCSNames map[string]bool, used *usedResources, inheritedXObjectCS map[string]inheritedCS, entry inheritedCS) {
	fillCSSet, strokeCSSet := entry.fill, entry.stroke
	// Text rendering mode is part of the text state, which q/Q saves and
	// restores with the rest of the graphics state (ISO 32000-1, 9.3.1).
	textRender := 0
	type csState struct {
		fill, stroke bool
		textRender   int
	}
	var csStack []csState

	for idx, op := range ops {
		if idx%100 == 0 && ctx.Err() != nil {
			return
		}
		switch op.Operator {
		case "q":
			csStack = append(csStack, csState{fillCSSet, strokeCSSet, textRender})
		case "Q":
			if len(csStack) > 0 {
				prev := csStack[len(csStack)-1]
				csStack = csStack[:len(csStack)-1]
				fillCSSet, strokeCSSet, textRender = prev.fill, prev.stroke, prev.textRender
			}
		case "Tr":
			if len(op.Operands) >= 1 {
				if mode, ok := op.Operands[0].(ast.PdfInt); ok {
					textRender = int(mode)
				}
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
		case "cs", "CS":
			if op.Operator == "cs" {
				fillCSSet = true
			} else {
				strokeCSSet = true
			}
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					markDeviceCS(string(name), usage)
					if referencedCSNames != nil {
						referencedCSNames[string(name)] = true
					}
				}
			}
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
			// Per ISO 32000-1, Table 106, modes 3 and 7 add no marks: 3 is
			// invisible and 7 only adds the glyphs to the clipping path. Text
			// shown in either paints nothing, so it selects no colour space,
			// which is what veraPDF models. Reporting the initial DeviceGray
			// there flagged an uncalibrated colour space the file never uses.
			if textRender != textRenderInvisible && textRender != textRenderClip && !fillCSSet {
				usage.gray = true
			}
		case "sc", "scn", "SC", "SCN":
			// Per ISO 32000-1, Table 74 the only form of scn/SCN that takes a
			// name operand is the pattern form, so a trailing name is a
			// /Resources/Pattern key. sc/SC never take one; they are listed here
			// only so a malformed stream cannot smuggle a name past this case.
			if len(op.Operands) > 0 && used != nil {
				if name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName); ok {
					used.patterns[string(name)] = true
				}
			}
		case "sh":
			// sh names a /Resources/Shading key and paints it (ISO 32000-1
			// §8.7.4.2). It is the only operator that does.
			if len(op.Operands) >= 1 && used != nil {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					used.shadings[string(name)] = true
				}
			}
		case "Tf":
			// Tf selects a /Resources/Font key (ISO 32000-1 §9.3.1). A Type3
			// font's CharProcs paint only once some Tf has selected it.
			if len(op.Operands) >= 1 && used != nil {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					used.fonts[string(name)] = true
				}
			}
		case "Do":
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					if referencedXObjects != nil {
						referencedXObjects[string(name)] = true
					}
					recordInheritedCS(inheritedXObjectCS, string(name), fillCSSet, strokeCSSet)
				}
			}
		}
	}
}

// scanInlineImageCSOps checks parsed BI operations for inline image colour
// space usage. Inline images use abbreviated key names: /CS for ColorSpace.
// Abbreviations: /CS /G = DeviceGray, /CS /RGB = DeviceRGB, /CS /CMYK = DeviceCMYK.
// Also handles /CS /DeviceRGB etc. and /ColorSpace variants. A /CS naming
// anything else references a /Resources/ColorSpace entry (ISO 32000-1 §8.9.7):
// its key is added to referencedCSNames so the declaration loop resolves the
// underlying colour space (e.g. an Indexed base of DeviceRGB). veraPDF, the
// reference validator, treats such an inline reference as real usage under
// §6.2.4.3.
func scanInlineImageCSOps(ops []content.Operation, usage *deviceCSUsage, referencedCSNames map[string]bool) {
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
			default:
				if referencedCSNames != nil {
					referencedCSNames[string(csName)] = true
				}
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

	// An appearance stream that omits /Resources falls back to the page's, the
	// same inheritance ISO 32000-1, 8.10.1 states for any form XObject.
	pageRes, err := document.ResolveDict(ctx, doc, page["Resources"])
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve page resources", "error", err)
		pageRes = nil
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
				formUsage := scanFormXObjectCS(ctx, doc, stream, 0, visited, inheritedCSDefault(), part, pageRes)
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
				formUsage := scanFormXObjectCS(ctx, doc, subStream, 0, visited, inheritedCSDefault(), part, pageRes)
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

// resolveShadingDict returns a shading's dictionary whichever form it takes.
// Per ISO 32000-1, 8.7.4.5, a shading of type 1 to 3 is a dictionary while a
// mesh shading of type 4 to 7 is a stream carrying the same entries in its
// stream dictionary. Resolving only the dictionary form dropped every mesh:
// pdfjs issue17848.pdf paints a type 4 shading whose /ColorSpace is DeviceRGB,
// and litho counted no device colour space for the page at all.
func resolveShadingDict(ctx context.Context, doc *document.Document, obj ast.PdfObject) ast.PdfDict {
	if d, err := document.ResolveDict(ctx, doc, obj); err == nil && d != nil {
		return d
	}
	if stream, err := document.ResolveStream(ctx, doc, obj); err == nil && stream.Dict != nil {
		return stream.Dict
	}
	return nil
}

// scanShadingColorSpaces checks Shading resources for device colour space usage.
// Shading dictionaries contain a /ColorSpace entry that may reference a device CS.
func scanShadingColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage, used *usedResources) {
	shadingDict, err := document.ResolveDict(ctx, doc, resources["Shading"])
	if err != nil || shadingDict == nil {
		return
	}

	for name, shadingObj := range shadingDict {
		if ctx.Err() != nil {
			return
		}

		// Only shadings an sh operator paints are used. A shading merely
		// declared in /Resources/Shading paints nothing.
		if used != nil && !used.shadings[string(name)] {
			continue
		}

		sd := resolveShadingDict(ctx, doc, shadingObj)
		if sd == nil {
			doc.Logger.Debug("compliance.color: cannot resolve Shading dict", "name", name)
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
func scanPatternColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict, usage *deviceCSUsage, depth int, visited map[string]bool, used *usedResources, part int) {
	patternDict, err := document.ResolveDict(ctx, doc, resources["Pattern"])
	if err != nil || patternDict == nil {
		return
	}

	for name, patternObj := range patternDict {
		if ctx.Err() != nil {
			return
		}

		// Only patterns a scn/SCN operator actually names are used. A pattern
		// merely declared in /Resources/Pattern paints nothing, so its content
		// cannot be a device-colour use (ISO 19005-2 §6.2.4.3 is usage-driven).
		if used != nil && !used.patterns[string(name)] {
			continue
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

		// An uncoloured tiling pattern (/PaintType 2) shall not specify any
		// colour in its content stream: the colour comes from the operands of
		// the scn/SCN that selects it, interpreted in the underlying space of
		// [/Pattern base] (ISO 32000-1 §8.7.3.3, Table 75). The initial
		// DeviceGray of §8.6.8 therefore never applies inside the cell, so a
		// bare painting operator there is not a DeviceGray use. veraPDF reports
		// nothing for such a cell, not even an explicit "0 g" in it.
		paintType, _ := document.ResolveInt(ctx, doc, stream.Dict["PaintType"])
		if paintType == 2 {
			continue
		}

		// Scan the tiling pattern's content stream as a Form XObject.
		formUsage := scanFormXObjectCS(ctx, doc, stream, depth, visited, inheritedCSDefault(), part, resources)
		usage.rgb = usage.rgb || formUsage.rgb
		usage.cmyk = usage.cmyk || formUsage.cmyk
		usage.gray = usage.gray || formUsage.gray
		usage.subScopeRGB = usage.subScopeRGB || formUsage.subScopeRGB
		usage.subScopeCMYK = usage.subScopeCMYK || formUsage.subScopeCMYK
		usage.subScopeGray = usage.subScopeGray || formUsage.subScopeGray
	}
}
