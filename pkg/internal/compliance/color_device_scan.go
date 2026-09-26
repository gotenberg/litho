package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

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

// scanContentStreamDeviceCS surveys one page's already-parsed operations and
// detects device-dependent colour space usage from explicit colour operators.
// It also returns the names of XObjects invoked by Do, the colour-space names
// selected by cs/CS, so the resource scan can distinguish colour spaces that are
// actually referenced from ones merely declared in /Resources/ColorSpace, and
// the colour-space state in effect at each Do, which a Form XObject inherits.
//
// It takes the operations rather than the page, because three separate check
// groups (colour, graphics, XObject) each survey every page and each used to
// resolve and re-parse the same content stream to do it. The page's operations
// are parsed once by [pageCtx.parsedOps] and shared from there. A page whose
// content is missing or will not parse yields no operations, which is the same
// empty survey the resolve and parse failures returned before.
func scanContentStreamDeviceCS(ctx context.Context, ops []content.Operation, patternCSNames map[string]bool) (deviceCSUsage, map[string]bool, map[string]bool, *usedResources, map[string]inheritedCS) {
	return scanContentStreamDeviceCSFrom(ctx, ops, patternCSNames, inheritedCSDefault())
}

// scanContentStreamDeviceCSFrom surveys operations that begin from an inherited
// colour-space state.
//
// A page content stream inherits nothing, which is the state
// [scanContentStreamDeviceCS] passes. A Form XObject instead begins from the
// state in effect at the Do that invoked it (ISO 32000-1 §8.10.2), so a bare
// scn there can name a pattern the invoking stream selected the space for.
func scanContentStreamDeviceCSFrom(ctx context.Context, ops []content.Operation, patternCSNames map[string]bool, entry inheritedCS) (deviceCSUsage, map[string]bool, map[string]bool, *usedResources, map[string]inheritedCS) {
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
	scanContentColorOps(ctx, ops, &usage, referencedXObjects, referencedCSNames, used, inherited, entry, patternCSNames)

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
	fill, stroke               bool
	fillPattern, strokePattern bool
}

// recordInheritedCS notes the state in effect at one Do. An XObject invoked
// more than once inherits a different state each time, and veraPDF reports the
// invocation that runs with no space selected, so the states combine by AND:
// the nested scope counts a space as selected only if every invocation selected
// one.
func recordInheritedCS(states map[string]inheritedCS, name string, fillCSSet, strokeCSSet, fillPattern, strokePattern bool) {
	if states == nil {
		return
	}
	if prev, seen := states[name]; seen {
		fillCSSet = fillCSSet && prev.fill
		strokeCSSet = strokeCSSet && prev.stroke
		fillPattern = fillPattern && prev.fillPattern
		strokePattern = strokePattern && prev.strokePattern
	}
	states[name] = inheritedCS{
		fill:          fillCSSet,
		stroke:        strokeCSSet,
		fillPattern:   fillPattern,
		strokePattern: strokePattern,
	}
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

// textModePaints reports whether a text rendering mode paints glyphs with
// the fill colour and with the stroke colour (ISO 32000-1, Table 106): fill in
// modes 0, 2, 4 and 6, stroke in modes 1, 2, 5 and 6, neither in 3 and 7. A
// mode outside 0 to 7 fills, as the default mode 0 does. Measured against
// veraPDF for every mode.
func textModePaints(mode int) (fill, stroke bool) {
	switch mode {
	case 1, 5: //nolint:mnd // Table 106 stroke-only modes.
		return false, true
	case 2, 6: //nolint:mnd // Table 106 fill-and-stroke modes.
		return true, true
	case textRenderInvisible, textRenderClip:
		return false, false
	default:
		return true, false
	}
}

// scanContentColorOps records device colour usage from a content stream:
// explicit colour operators (g/G, rg/RG, k/K, cs/CS with a direct device
// name), the colour-space names selected by cs/CS (referencedCSNames), and the
// XObjects invoked by Do (referencedXObjects). A painting operator that runs
// with no colour space yet selected uses the initial graphics-state colour
// space, DeviceGray; veraPDF flags that implicit default use, so it counts as
// DeviceGray usage here too (ISO 19005-2 §6.2.4.3), recorded as initialGray
// because the root content stream's DefaultGray is what covers it.
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
//
// # Corpus evidence
//
//   - pdfjs issue18466.pdf: writes "/P12 scn /P12 SCN" without ever selecting
//     /Pattern, and veraPDF reports nothing for the image inside that
//     pattern; treating the name as a use judged a pattern the file never
//     paints.
//   - pdfjs issue20513.pdf: strokes its text (Tr 1) after only K, and veraPDF
//     reports DeviceCMYK at strokeCS and no DeviceGray.
func scanContentColorOps(ctx context.Context, ops []content.Operation, usage *deviceCSUsage, referencedXObjects, referencedCSNames map[string]bool, used *usedResources, inheritedXObjectCS map[string]inheritedCS, entry inheritedCS, patternCSNames map[string]bool) {
	fillCSSet, strokeCSSet := entry.fill, entry.stroke
	// Whether the space currently selected is specifically a Pattern space. A
	// name operand to scn/SCN names a /Resources/Pattern key only once one is
	// (ISO 32000-1 §8.6.8, Table 74).
	//
	// A nested scope inherits that selection from the state in effect at the Do
	// that invoked it (ISO 32000-1 §8.10.2): veraPDF judges a pattern a form
	// selects with a bare scn after the invoking stream chose the Pattern
	// space. A scope no Do invokes inherits nothing, so a bare scn with no
	// Pattern space selected anywhere still paints nothing, and veraPDF
	// reports nothing for the pattern's content.
	fillPattern, strokePattern := entry.fillPattern, entry.strokePattern
	// Text rendering mode is part of the text state, which q/Q saves and
	// restores with the rest of the graphics state (ISO 32000-1, 9.3.1).
	textRender := 0
	type csState struct {
		fill, stroke               bool
		fillPattern, strokePattern bool
		textRender                 int
	}
	var csStack []csState

	for idx, op := range ops {
		if idx%100 == 0 && ctx.Err() != nil {
			return
		}
		switch op.Operator {
		case "q":
			csStack = append(csStack, csState{fillCSSet, strokeCSSet, fillPattern, strokePattern, textRender})
		case "Q":
			if len(csStack) > 0 {
				prev := csStack[len(csStack)-1]
				csStack = csStack[:len(csStack)-1]
				fillCSSet, strokeCSSet, textRender = prev.fill, prev.stroke, prev.textRender
				fillPattern, strokePattern = prev.fillPattern, prev.strokePattern
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
				fillCSSet, fillPattern = true, false
			} else {
				strokeCSSet, strokePattern = true, false
			}
		case "rg", "RG":
			usage.rgb = true
			if op.Operator == "rg" {
				fillCSSet, fillPattern = true, false
			} else {
				strokeCSSet, strokePattern = true, false
			}
		case "k", "K":
			usage.cmyk = true
			if op.Operator == "k" {
				fillCSSet, fillPattern = true, false
			} else {
				strokeCSSet, strokePattern = true, false
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
					// Selecting a Pattern space is what lets a later scn/SCN
					// name operand select a pattern; any other space clears it.
					// The space is usually named in /Resources/ColorSpace as
					// [/Pattern base] rather than written literally, so
					// patternCSNames carries the keys that resolve to one.
					selectsPattern := name == "Pattern" || patternCSNames[string(name)]
					if op.Operator == "cs" {
						fillPattern = selectsPattern
					} else {
						strokePattern = selectsPattern
					}
				}
			}
		case "f", "F", "f*":
			if !fillCSSet {
				usage.initialGray = true
			}
		case "S", "s":
			if !strokeCSSet {
				usage.initialGray = true
			}
		case "B", "B*", "b", "b*":
			if !fillCSSet || !strokeCSSet {
				usage.initialGray = true
			}
		case "Tj", "TJ", "'", "\"":
			// Per ISO 32000-1, Table 106, modes 3 and 7 add no marks: 3 is
			// invisible and 7 only adds the glyphs to the clipping path. Text
			// shown in either paints nothing, so it selects no colour space,
			// which is what veraPDF models. Reporting the initial DeviceGray
			// there flagged an uncalibrated colour space the file never uses.
			// The other modes paint with the fill colour, the stroke colour or
			// both, so the space each one needs is the one that must be
			// selected: stroked text (Tr 1) after only K is a DeviceCMYK use
			// at strokeCS, and no DeviceGray one.
			fills, strokes := textModePaints(textRender)
			if (fills && !fillCSSet) || (strokes && !strokeCSSet) {
				usage.initialGray = true
			}
		case "BI":
			// An inline image mask carries no colour space: ISO 32000-1, 8.9.7
			// (Table 93 abbreviates ImageMask as IM) paints it like an image
			// mask XObject, with the current fill colour (8.9.6.2), so with no
			// fill space selected it paints the initial DeviceGray. veraPDF
			// reports that at .../inlineImage[0]/imageCS[0], in page content and
			// in a form alike, and the page's DefaultGray covers it.
			if !fillCSSet && inlineImageIsMask(op) {
				usage.initialGray = true
			}
		case "sc", "scn", "SC", "SCN":
			// Per ISO 32000-1, Table 74 the only form of scn/SCN that takes a
			// name operand is the pattern form, so a trailing name is a
			// /Resources/Pattern key. sc/SC never take one; they are listed here
			// only so a malformed stream cannot smuggle a name past this case.
			//
			// The operand names a pattern only when a Pattern space is current
			// on the side it sets: scn sets the fill colour, SCN the stroke
			// colour (ISO 32000-1 §8.6.8). A name with no Pattern space
			// selected is malformed and paints nothing, and veraPDF reports
			// nothing for the content inside that pattern; treating the name
			// as a use judged a pattern the file never paints.
			stroking := op.Operator == "SC" || op.Operator == "SCN"
			patternSelected := fillPattern
			if stroking {
				patternSelected = strokePattern
			}
			if patternSelected && len(op.Operands) > 0 && used != nil {
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
					recordInheritedCS(inheritedXObjectCS, string(name), fillCSSet, strokeCSSet, fillPattern, strokePattern)
				}
			}
		}
	}
}

// inlineImageIsMask reports whether a BI operation's dictionary marks the
// inline image as a stencil mask, by /IM or its full name /ImageMask.
func inlineImageIsMask(op content.Operation) bool {
	if len(op.Operands) == 0 {
		return false
	}
	dict, ok := op.Operands[0].(ast.PdfDict)
	if !ok {
		return false
	}
	for _, key := range []ast.PdfName{"IM", "ImageMask"} {
		if b, ok := dict[key].(ast.PdfBool); ok && bool(b) {
			return true
		}
	}
	return false
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
		// An inline image mask carries no colour space: ISO 32000-1, 8.9.5,
		// Table 89 does not allow the entry on a mask, which paints with the
		// current fill colour instead (scanContentColorOps). veraPDF ignores a
		// /CS written on one, as it does on an image mask XObject: a mask
		// declaring /CS /RGB or /CS /G under an ICCBased fill reports nothing.
		if inlineImageIsMask(op) {
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

// resolveShadingDict returns a shading's dictionary whichever form it takes.
// Per ISO 32000-1, 8.7.4.5, a shading of type 1 to 3 is a dictionary while a
// mesh shading of type 4 to 7 is a stream carrying the same entries in its
// stream dictionary. Resolving only the dictionary form dropped every mesh.
//
// # Corpus evidence
//
//   - pdfjs issue17848.pdf: paints a type 4 shading whose /ColorSpace is
//     DeviceRGB, and litho counted no device colour space for the page at all.
func resolveShadingDict(ctx context.Context, doc *document.Document, obj ast.PdfObject) ast.PdfDict {
	if d, err := document.ResolveDict(ctx, doc, obj); err == nil && d != nil {
		return d
	}
	if stream, err := document.ResolveStream(ctx, doc, obj); err == nil && stream.Dict != nil {
		return stream.Dict
	}
	return nil
}
