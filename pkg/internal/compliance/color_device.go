package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
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

// deviceCSUsage tracks which device-dependent colour spaces one content
// stream's operators use (see [scanContentStreamDeviceCS]).
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
// output intent or its transparency group's device-independent blending space.
// ISO 19005-2/3, 6.2.4.3 conditions DeviceRGB and DeviceCMYK on the
// OutputIntent alone, so a blending space covers nothing there.
//
// A page whose content stream an earlier page shares uses no colour through
// it, since veraPDF judges that stream on the first page alone, with what it
// draws (see [markJudgedContents]); the Separation and DeviceN walk, the
// ICCBased walk and the colour-space link index skip it the same way.
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
			if blendingCS != "" && blendingCS == family.intent {
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
