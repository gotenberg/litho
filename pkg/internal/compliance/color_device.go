package compliance

import (
	"context"
	"fmt"

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

// defaultCS holds the Default colour spaces one resource dictionary sets
// under /ColorSpace, which replace the device families DefaultRGB, DefaultCMYK
// and DefaultGray name (ISO 32000-1, 8.6.5.6), and colorSpaces, the
// dictionary holding them, against which a name any colour space read from
// the same resources holds resolves (see [colorLinkWalk.judgeFamilyName]).
//
// inDefault marks the spaces a Default colour space holds, where veraPDF
// replaces no device family again: a family name the Default gives, one an
// alternate of its Separation or DeviceN gives, and its DeviceN process space.
// The base of an /Indexed space and the Separations of a DeviceN /Colorants
// dictionary are replaced again, as anywhere else (veraPDF-validation 1.30,
// ColorSpaceFactory.getColorSpaceFromName and PDSpecialColorSpace, whose
// wasDefault flag PDIndexed and PDDeviceN.getColorants drop).
type defaultCS struct {
	gray, rgb, cmyk defaultEntry
	colorSpaces     ast.PdfDict
	inDefault       bool
}

// defaultEntry is one Default colour space entry: obj is the value, nil when
// the dictionary has none, and readable reports that veraPDF reads a colour
// space from it (see [defaultSpaceReadable]).
type defaultEntry struct {
	obj      ast.PdfObject
	readable bool
}

// entry returns the Default colour space entry that replaces the device
// family, or the zero entry when d replaces none.
func (d defaultCS) entry(family string) defaultEntry {
	if d.inDefault {
		return defaultEntry{}
	}
	switch family {
	case "DeviceRGB":
		return d.rgb
	case "DeviceCMYK":
		return d.cmyk
	case "DeviceGray":
		return d.gray
	}
	return defaultEntry{}
}

// inside returns d as the spaces a Default colour space holds see it (see
// [defaultCS]).
func (d defaultCS) inside() defaultCS {
	d.inDefault = true
	return d
}

// outside returns d as the base of an /Indexed space or a DeviceN colourant
// sees it, where a device family is replaced again.
func (d defaultCS) outside() defaultCS {
	d.inDefault = false
	return d
}

// deviceCSUsage tracks which device-dependent colour spaces one content
// stream's operators use (see [scanContentStreamDeviceCSFrom]).
type deviceCSUsage struct {
	rgb  bool
	cmyk bool
	gray bool
	// initialGray tracks painting with no colour space selected, which uses
	// the initial DeviceGray of ISO 32000-1, 8.6.8.
	initialGray bool
}

// formTooDeep reports whether a content scope depth levels below the page is
// deeper than the document's form budget, limits.Limits.MaxFormDepth. The
// page's own content, or an annotation's appearance, is at 0 and a form it
// paints at 1; a tiling pattern and a Type 3 glyph description count as a
// level each, as a form does. Anti-DoS guard, not spec-mandated: ISO 32000-1
// sets no limit on how deeply one form may invoke another, so a crafted file
// could nest them until the goroutine stack is exhausted.
//
// It bounds CHAIN LENGTH only. Cycles are broken by the visited sets the callers
// own, not by this cap: [checkFormXObjectFonts], [checkType3CharProcFonts]
// and [colorLinkWalk] key on object number. A depth bound survives a cycle
// without detecting it, so both are needed.
//
// Every recursion site of the package reads it (fonts, transparency, colour,
// file limits, accessibility), and text extraction, image listing and
// auto-tagging read the same budget, so the walks agree on which forms exist.
func formTooDeep(doc *document.Document, depth int) bool {
	return depth > doc.Limits().MaxFormDepth
}

// deviceFamilies are the device families in the order their violations are
// reported, with the rule each breaks and the colour space an output intent
// must name to cover it (ISO 19005-1, 6.2.3.3; ISO 19005-2, 6.2.4.3), where
// "" means any.
//
//nolint:gochecknoglobals // immutable lookup table.
var deviceFamilies = []struct {
	name, defaultName, intent string
	rule                      func(part int) Rule
}{
	{"DeviceRGB", "DefaultRGB", "RGB ", deviceRGBRule},
	{"DeviceCMYK", "DefaultCMYK", "CMYK", deviceCMYKRule},
	{"DeviceGray", "DefaultGray", "", deviceGrayRule},
}

// deviceLinkCounts holds, per device family in [deviceFamilies] order, the
// links of one page's walk that reach it with no Default colour space to
// replace it: plain for every link but the page's transparency group, whose
// blending space group counts.
type deviceLinkCounts struct {
	plain, group [3]int
}

// deviceFamilyIndex returns the position of family in [deviceFamilies].
func deviceFamilyIndex(family string) int {
	for i, f := range deviceFamilies {
		if f.name == family {
			return i
		}
	}
	return -1
}

// checkDeviceColorSpaces validates that device-dependent colour spaces
// (DeviceRGB, DeviceCMYK, DeviceGray) are only used when allowed.
// checkDeviceColorSpaces takes the already-resolved pages so it can reuse their
// parsed operations; the page dictionaries are the same ones [collectPages]
// yields, in the same order.
//
// It fails a rule once per link veraPDF makes to the device family, as
// [colorLinkWalk] follows them, that no Default colour space replaces and no
// output intent covers: an OutputIntent whose profile names the family's
// colour space, any OutputIntent for DeviceGray, and under PDF/A-4 the page's
// output intent or, for DeviceRGB and DeviceCMYK, its transparency group's
// device-independent blending space (see [blendCovers]).
// ISO 19005-2/3, 6.2.4.3 conditions DeviceRGB and DeviceCMYK on the
// OutputIntent alone, so a blending space covers nothing there.
//
// A page whose content stream an earlier page shares uses no colour through
// it, since veraPDF judges that stream on the first page alone, with what it
// draws (see [markJudgedContents]); the Separation and DeviceN walks and the
// ICCBased walk skip it the same way.
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: two pages sharing one
// stream fail the device colour tests, the DeviceN colourant limit, the
// ICCBased profile test and ISO 19005-2, 6.2.4.4 t1 and t2 as often as one of
// them alone, and two pages whose /Contents are direct arrays naming it fail
// them twice as often.
//
// A device blending space of the page's transparency group reports its own
// context, which routes its repair to the group rather than to an output
// intent.
//
// # Corpus evidence
//
//   - pdfjs bug1019475_1.pdf: gives its page a DeviceRGB transparency group
//     beside a DefaultRGB, and veraPDF reports no DeviceRGB.
//   - pdfjs hello_world_rotated.pdf: five pages share one content stream
//     that paints with DeviceGray, and veraPDF fails ISO 19005-1, 6.2.3.3 t3
//     and ISO 19005-2, 6.2.4.3 t4 once, where litho failed them five times.
//   - pdfjs issue18042.pdf: four pages share one content stream that paints a
//     DeviceRGB image, and veraPDF fails ISO 19005-1, 6.2.3.3 t1 and ISO
//     19005-2, 6.2.4.3 t2 once, where litho failed them four times.
func checkDeviceColorSpaces(ctx context.Context, doc *document.Document, pages []pageCtx, part int, hasICC bool, iccColorSpace string, results *[]Violation) {
	counts := make([]deviceLinkCounts, len(pages))
	walkColorLinks(ctx, doc, pages, part, func(n colorNode) {
		i := deviceFamilyIndex(n.family)
		if i < 0 || n.covered {
			return
		}
		if n.group {
			counts[n.page].group[i]++
		} else {
			counts[n.page].plain[i]++
		}
	})
	if ctx.Err() != nil {
		return
	}

	for i := range pages {
		page := pages[i].dict

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
		var blendingCS string
		if part == 4 {
			blendingCS = pageBlendingColorSpace(ctx, doc, page)
		}

		for f, family := range deviceFamilies {
			covered := effectiveHasICC && (family.intent == "" || effectiveICCCS == family.intent)
			if blendCovers(blendingCS, family.name) {
				covered = true
			}
			if covered {
				continue
			}
			rule := family.rule(part)
			if n := counts[i].plain[f]; n > 0 {
				*results = append(*results, repeatViolation(rule, n,
					fmt.Sprintf("page %d: %s used without %s or %s", i+1, family.name, family.defaultName, intentPhrase(family.intent)))...)
			}
			if n := counts[i].group[f]; n > 0 {
				*results = append(*results, repeatViolation(rule, n,
					fmt.Sprintf("page %d: transparency group blending colour space is %s without %s", i+1, family.name, intentPhrase(family.intent)))...)
			}
		}
	}
}

// intentPhrase names the output intent that covers a device family whose
// profile colour space must be intent, or any when intent is "".
func intentPhrase(intent string) string {
	switch intent {
	case "RGB ":
		return "RGB OutputIntent"
	case "CMYK":
		return "CMYK OutputIntent"
	}
	return "OutputIntent"
}

// repeatViolation returns n violations of rule with one context, the
// failures of n links to one colour space on one page.
func repeatViolation(rule Rule, n int, context string) []Violation {
	out := make([]Violation, n)
	for i := range out {
		out[i] = Violation{Rule: rule, Context: context}
	}
	return out
}

// pageBlendingColorSpace returns the type of the blending colour space of the
// page's transparency group (see [blendingSpaceType]), or "" when the page has
// no transparency group or its /CS reads as no colour space.
func pageBlendingColorSpace(ctx context.Context, doc *document.Document, page ast.PdfDict) string {
	group, err := document.ResolveDict(ctx, doc, page["Group"])
	if err != nil || group == nil {
		return ""
	}
	if s, _ := document.ResolveName(ctx, doc, group["S"]); s != "Transparency" { // Best-effort: an unreadable /S is no transparency group.
		return ""
	}
	return blendingSpaceType(ctx, doc, group["CS"])
}

// blendingSpaceType returns the type veraPDF gives csObj, the /CS of a
// transparency group, as the current transparency blending space that ISO
// 19005-4, 6.2.4.3 t2 and t3 test (veraPDF-validation 1.30,
// GFTransparencyColorSpace.getcolorSpaceType): the colour space the header of
// its ICC profile names for an ICCBased space, "RGB " or "CMYK" among them,
// and the family name of any other (ISO 32000-1, 8.6.5). It returns "" when
// csObj reads as no colour space.
//
// veraPDF reads a group's /CS with no resources (PDGroup.getColorSpace), so a
// name reads as a device family or as no colour space, a name of a
// /ColorSpace entry among them. Measured with veraPDF 1.30 at PDF/A-4: a page
// group, or an isolated form group, whose /CS is /CS0, an entry of the page's
// or the form's resources holding an ICCBased space over an RGB profile,
// leaves "1 0 0 rg" and f failing DeviceRGB twice, and the same ICCBased space
// written as the /CS covers both.
func blendingSpaceType(ctx context.Context, doc *document.Document, csObj ast.PdfObject) string {
	for range maxColorSpaceNesting {
		if name, err := document.ResolveName(ctx, doc, csObj); err == nil {
			if deviceFamily(string(name)) == string(name) {
				return string(name)
			}
			return ""
		}
		arr, err := document.ResolveArray(ctx, doc, csObj)
		if err != nil || len(arr) == 0 {
			return ""
		}
		// A one-element array holds the colour space it names (ISO 32000-1,
		// 8.6.3), which veraPDF reads as it reads the element.
		if len(arr) == 1 {
			csObj = arr[0]
			continue
		}
		kind, err := document.ResolveName(ctx, doc, arr[0])
		if err != nil {
			return ""
		}
		if kind != "ICCBased" {
			return string(kind)
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
	}
	return ""
}

// collectDefaultColorSpaces returns the Default colour spaces a resources
// dictionary's /ColorSpace sub-dictionary sets (ISO 32000-1, 8.6.5.6). An
// entry whose value is null sets none, as an absent one (ISO 32000-1, 7.3.7),
// while a reference, even to no object, sets one veraPDF cannot read:
// measured with veraPDF 1.30 at PDF/A-2b and PDF/A-4, a DefaultRGB written as
// null leaves "/DeviceRGB cs", scn and f failing DeviceRGB three times, and
// one referencing no object fails DeviceGray three times and ISO 19005-4,
// 6.2.2 t3.
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

	entry := func(key ast.PdfName) defaultEntry {
		obj, ok := csDict[key]
		if _, null := obj.(ast.PdfNull); !ok || null {
			return defaultEntry{}
		}
		return defaultEntry{obj: obj, readable: defaultSpaceReadable(ctx, doc, obj, csDict, 0)}
	}
	return defaultCS{rgb: entry("DefaultRGB"), cmyk: entry("DefaultCMYK"), gray: entry("DefaultGray"), colorSpaces: csDict}
}

// unreadableDefaultSelected returns the device family a content stream
// selects while defaultRes, the resources whose Default colour spaces replace
// its families, give one for it that veraPDF cannot read (see
// [defaultSpaceReadable]), or "" when it selects none.
//
// veraPDF records such a family as a resource name no dictionary defines,
// which fails ISO 19005-4, 6.2.2 t3, once for the stream (veraPDF-validation
// 1.30, PDResourcesHandler.getColorSpace): wherever it resolves a family
// through the stream's resources, at g, rg, k, G, RG and K and at cs and CS
// naming the family while the stream processes its colour operators (see
// [walkColorSpaceLinksWith]), at an image XObject a Do paints whose
// /ColorSpace names it, looked up in xobjects and then fallback, and, in a
// root content stream, a page's or an appearance stream's, at the initial
// DeviceGray (ISO 32000-1, 8.6.8), which a form, a tiling cell or a Type 3
// glyph inherits instead. colourOff reports that the stream begins with its
// colour operators off.
//
// Measured with veraPDF 1.30 at PDF/A-4 on a DefaultRGB or DefaultGray that is
// a number: a page painting nothing fails t3 through its DefaultGray, and so
// does an appearance stream, but not a form a page paints; a page, a form, a
// Type 3 glyph set with d0 and a page's inherited resources fail it through
// rg, "/DeviceRGB cs" or an image the page paints, a glyph past its d1 does
// not; and the page's DefaultRGB fails nothing through a form's rg.
//
// One case stays unmodelled: veraPDF records an image's family only in the
// stream that first paints it, where litho states it in every stream that
// paints it.
//
// # Corpus evidence
//
//   - All corpora: no file sets a Default colour space veraPDF cannot read.
func unreadableDefaultSelected(ctx context.Context, doc *document.Document, ops []content.Operation, defaultRes, xobjects, fallback ast.PdfDict, root, colourOff bool) string {
	defaults := collectDefaultColorSpaces(ctx, doc, defaultRes)
	unreadable := func(family string) bool {
		e := defaults.entry(family)
		return e.obj != nil && !e.readable
	}
	if !unreadable("DeviceGray") && !unreadable("DeviceRGB") && !unreadable("DeviceCMYK") {
		return ""
	}
	if root && unreadable("DeviceGray") {
		return "DeviceGray"
	}
	off := colourOff
	var stack []bool
	for idx, op := range ops {
		if idx%100 == 0 && ctx.Err() != nil {
			return ""
		}
		switch op.Operator {
		case "q":
			stack = append(stack, off)
		case "Q":
			if len(stack) > 0 {
				off = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "d1":
			off = true
		case "g", "rg", "k", "G", "RG", "K":
			if family := deviceFamilyOperators[op.Operator]; !off && unreadable(family) {
				return family
			}
		case "cs", "CS":
			if name := lastNameOperand(op); !off && deviceFamily(name) == name && unreadable(name) {
				return name
			}
		case "Do":
			if family := imageDeviceFamily(ctx, doc, op, xobjects, fallback); family != "" && unreadable(family) {
				return family
			}
		}
	}
	return ""
}

// imageDeviceFamily returns the device family the image XObject a Do paints
// names as its /ColorSpace, alone or in a one-element array, as veraPDF reads
// a colour space name there (veraPDF-validation 1.30, PDXImage), or "" for
// any other XObject, an image mask or a colour space written out. The name the
// Do gives is looked up in xobjects and then fallback.
func imageDeviceFamily(ctx context.Context, doc *document.Document, op content.Operation, xobjects, fallback ast.PdfDict) string {
	name := ast.PdfName(lastNameOperand(op))
	if name == "" {
		return ""
	}
	var obj ast.PdfObject
	for _, res := range []ast.PdfDict{xobjects, fallback} {
		dict, _ := document.ResolveDict(ctx, doc, res["XObject"]) // Best-effort: unreadable XObjects define no name.
		if o, ok := dict[name]; ok {
			obj = o
			break
		}
	}
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return ""
	}
	if subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); subtype != "Image" { // Best-effort: an unreadable /Subtype is no image.
		return ""
	}
	// ISO 32000-1, 8.9.6.2: an image mask carries no colour space.
	if mask, _ := document.ResolveBool(ctx, doc, stream.Dict["ImageMask"]); mask { // Best-effort: an unreadable /ImageMask is no mask.
		return ""
	}
	csObj := stream.Dict["ColorSpace"]
	if arr, err := document.ResolveArray(ctx, doc, csObj); err == nil && len(arr) == 1 {
		csObj = arr[0]
	}
	cs, _ := document.ResolveName(ctx, doc, csObj) // Best-effort: a colour space written out names no family.
	if family := deviceFamily(string(cs)); family == string(cs) {
		return family
	}
	return ""
}

// defaultSpaceReadable reports whether veraPDF reads a colour space from the
// Default colour space entry obj of the /ColorSpace dictionary colorSpaces,
// which a device family it replaces then links to in its place.
//
// veraPDF reads a name as a device family, as Pattern, or as another entry of
// colorSpaces; an array as the family its first element names, a one-element
// array as that element, and [/CalCMYK ...] as DeviceCMYK; and anything else
// as no colour space (veraPDF-validation 1.30, ColorSpaceFactory.getColorSpace
// of the parser). What it does with a Default it cannot read depends on what
// reaches it (see [colorLinkWalk.deviceRef]). A dictionary, which veraPDF
// reads as a pattern when it carries a /PatternType, is read as nothing here.
func defaultSpaceReadable(ctx context.Context, doc *document.Document, obj ast.PdfObject, colorSpaces ast.PdfDict, nesting int) bool {
	if nesting > maxColorSpaceNesting {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return false
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		switch name := string(v); {
		case deviceFamily(name) == name, name == "Pattern":
			return true
		default:
			entry, ok := colorSpaces[v]
			return ok && defaultSpaceReadable(ctx, doc, entry, colorSpaces, nesting+1)
		}
	case ast.PdfArray:
		if len(v) == 1 {
			return defaultSpaceReadable(ctx, doc, v[0], colorSpaces, nesting+1)
		}
		if len(v) == 0 {
			return false
		}
		kind, _ := document.ResolveName(ctx, doc, v[0]) // Best-effort: an unreadable family reads as no colour space.
		switch kind {
		case "CalGray", "CalRGB", "Lab", "ICCBased", "Separation", "DeviceN", "Indexed", "Pattern", "CalCMYK":
			return true
		}
	}
	return false
}

// collectPatternCSNames returns the /Resources/ColorSpace keys that name a
// Pattern colour space, so the content scan can tell that "/cs1 cs" selects
// one.
//
// A pattern space is written [/Pattern] or [/Pattern base] (ISO 32000-1
// §8.7.3.3) and is normally reached through a resource name rather than the
// literal /Pattern. Recognising only the literal spelling left every such
// pattern looking unpainted. An entry naming another entry reads as that
// entry (see [resolveColorSpaceName]).
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
		if resolveCSName(ctx, doc, resolveColorSpaceName(ctx, doc, csObj, csDict)) != "Pattern" {
			continue
		}
		if names == nil {
			names = make(map[string]bool)
		}
		names[string(name)] = true
	}
	return names
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
