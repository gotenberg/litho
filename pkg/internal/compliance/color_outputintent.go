package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func outputIntentICCRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_2_t01, Rule_19005_2_6_2_3_t01, Rule_19005_3_6_2_3_t01, Rule_19005_4_6_2_3_t01)
}

func outputIntentConsistencyRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_2_t02, Rule_19005_2_6_2_3_t02, Rule_19005_3_6_2_3_t02, Rule_19005_4_6_2_3_t02)
}

// destOutputProfileRefRule selects the forbidden-/DestOutputProfileRef rule for
// PDF/X OutputIntents by part: ISO 19005-2/3 §6.2.3 t3.
func destOutputProfileRefRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_2_3_t03, Rule_19005_3_6_2_3_t03, Rule{})
}

// checkOutputIntentICC validates OutputIntent ICC profiles per §6.2.2 (part 1)
// or §6.2.3 (parts 2-4). It returns the ICC colour space string and whether a
// valid ICC profile was found.
func checkOutputIntentICC(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, results *[]Violation) (string, bool) {
	outputIntents, err := document.ResolveArray(ctx, doc, catalog["OutputIntents"])
	if err != nil || len(outputIntents) == 0 {
		doc.Logger.Debug("compliance.color: no OutputIntents found", "error", err)
		return "", false
	}

	// §6.2.2 t2: if multiple OutputIntents exist with subtype GTS_PDFA1,
	// they must all reference the same DestOutputProfile. Track all
	// GTS_PDFA1 subtypes separately for consistency validation.
	var pdfaIntentCount int

	var (
		firstProfileRef ast.PdfObject
		firstColorSpace string
		iccColorSpace   string
		hasValidICC     bool
	)

	for i, intentObj := range outputIntents {
		if ctx.Err() != nil {
			return iccColorSpace, hasValidICC
		}

		intent, err := document.ResolveDict(ctx, doc, intentObj)
		if err != nil || intent == nil {
			doc.Logger.Debug("compliance.color: cannot resolve OutputIntent dict", "index", i, "error", err)
			continue
		}

		// PDF/A-4 §6.2.3 t3: DestOutputProfileRef is forbidden.
		if part == 4 {
			if _, has := intent["DestOutputProfileRef"]; has {
				*results = append(*results, failf(Rule_19005_4_6_2_3_t03,
					"OutputIntent[%d]: contains forbidden /DestOutputProfileRef", i)...)
			}
		}

		// Track OutputIntent subtype (/S key).
		subtypeName, _ := document.ResolveName(ctx, doc, intent["S"])
		if subtypeName == "GTS_PDFA1" {
			pdfaIntentCount++
		}

		// ISO 19005-2/3 §6.2.3 t3: a PDF/X OutputIntent (S == GTS_PDFX) shall
		// not contain DestOutputProfileRef. Unlike PDF/A-4, parts 2/3 forbid
		// the key only on PDF/X intents, so the subtype guard is mandatory.
		if (part == 2 || part == 3) && subtypeName == "GTS_PDFX" {
			if _, has := intent["DestOutputProfileRef"]; has {
				*results = append(*results, failf(destOutputProfileRefRule(part),
					"OutputIntent[%d]: PDF/X intent contains forbidden /DestOutputProfileRef", i)...)
			}
		}

		// Only PDF/A OutputIntents provide device CS coverage. The
		// subtype is GTS_PDFA1 for every PDF/A part (ISO 32000-2 Table
		// 401); "GTS_PDFA" does not exist. Other subtypes (e.g.
		// GTS_PDFX) do not grant coverage.
		isPDFAIntent := subtypeName == "GTS_PDFA1"

		// A /DestOutputProfile that resolves to null names no profile (ISO
		// 32000-1, 7.3.7 and 7.3.10): veraPDF judges no output profile for it,
		// measured on a null and on a reference to a null object.
		destProfileObj, ok := intent["DestOutputProfile"]
		if !ok || resourceEntryNull(ctx, doc, destProfileObj) {
			// §6.2.2: OutputIntent without DestOutputProfile must have
			// OutputConditionIdentifier referencing a well-known registry.
			if _, hasOCI := intent["OutputConditionIdentifier"]; !hasOCI {
				*results = append(*results, failf(outputIntentICCRule(part),
					"OutputIntent[%d]: missing DestOutputProfile and OutputConditionIdentifier", i)...)
			}
			continue
		}

		checkOutputIntentProfileComponents(ctx, doc, destProfileObj, part,
			fmt.Sprintf("OutputIntent[%d]", i), results)

		hdr, ok := checkICCOutputProfileHeader(ctx, doc, destProfileObj, part, fmt.Sprintf("OutputIntent[%d]", i), results)
		if !ok {
			continue
		}

		// Only PDF/A OutputIntents grant device CS coverage.
		if isPDFAIntent && validICCColorSpaces[hdr.colorSpace] {
			iccColorSpace = hdr.colorSpace
			hasValidICC = true
		}

		// §6.2.2 t2 / §6.2.3 t2: multiple OutputIntents must be consistent.
		// All DestOutputProfile entries must reference the same indirect object.
		// The spec requires the same indirect object, not merely identical data.
		if firstProfileRef == nil {
			firstProfileRef = destProfileObj
			firstColorSpace = hdr.colorSpace
		} else {
			if !sameIndirectRef(firstProfileRef, destProfileObj) {
				*results = append(*results, failf(outputIntentConsistencyRule(part),
					"OutputIntent[%d]: DestOutputProfile does not reference same object as first entry", i)...)
			}

			// Flag inconsistent colour spaces across OutputIntents.
			if hdr.colorSpace != firstColorSpace {
				*results = append(*results, failf(outputIntentConsistencyRule(part),
					"OutputIntent[%d]: colour space %q does not match first entry %q", i, hdr.colorSpace, firstColorSpace)...)
			}
		}
	}

	// §6.2.2 t2: if multiple GTS_PDFA1 OutputIntents exist but none has a
	// valid ICC profile, flag the inconsistency.
	if pdfaIntentCount > 1 && !hasValidICC {
		*results = append(*results, failf(outputIntentConsistencyRule(part),
			"%d GTS_PDFA1 OutputIntents but no valid DestOutputProfile found", pdfaIntentCount)...)
	}

	return iccColorSpace, hasValidICC
}

// iccColorSpaceComponents maps an ICC data colour space signature to the number
// of components /N must state for it, per ISO 19005-1 §6.2.3.2 t2, whose test
// reads (N == 1 && GRAY) || (N == 3 && (RGB or Lab)) || (N == 4 && CMYK).
// Signatures are exactly four bytes, so "RGB " and "Lab " carry a trailing
// space.
//
//nolint:gochecknoglobals // static spec table.
var iccColorSpaceComponents = map[string]int{
	"GRAY": 1,
	"RGB ": 3, //nolint:gocritic // ICC colour space signatures are exactly 4 bytes.
	"Lab ": 3, //nolint:gocritic // ICC colour space signatures are exactly 4 bytes.
	"CMYK": 4,
}

// checkOutputIntentProfileComponents validates that an OutputIntent's
// /DestOutputProfile states an /N matching the component count of the colour
// space its ICC profile declares (ISO 19005-1 §6.2.3.2 t2). Only part 1 states
// the requirement, the same scope [iccBasedNRule] records for an ICCBased
// colour space; veraPDF hangs the rule on its ICCProfile object, which covers
// an OutputIntent's destination profile as well.
//
// A profile litho cannot read answers the test no better than a mismatched one:
// veraPDF reports the same rule for both, and measured over 1984
// /DestOutputProfile streams in the corpora only seven answer it at all, five
// by a genuine mismatch and two by being undecodable.
func checkOutputIntentProfileComponents(ctx context.Context, doc *document.Document, destProfileObj ast.PdfObject, part int, label string, results *[]Violation) {
	rule := iccBasedNRule(part)
	if rule.Specification == "" {
		return
	}

	stream, err := document.ResolveStream(ctx, doc, destProfileObj)
	if err != nil {
		// checkICCOutputProfileHeader reports the unresolvable stream.
		doc.Logger.Debug("compliance.color: cannot resolve output profile stream", "label", label, "error", err)
		return
	}

	nVal, err := document.ResolveInt(ctx, doc, stream.Dict["N"])
	if err != nil {
		*results = append(*results, failf(rule, "%s: /DestOutputProfile has no /N entry", label)...)
		return
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		*results = append(*results, failf(rule,
			"%s: /DestOutputProfile states /N %d but its ICC profile will not decode", label, nVal)...)
		return
	}

	hdr, ok := parseICCHeader(data)
	if !ok {
		*results = append(*results, failf(rule,
			"%s: /DestOutputProfile states /N %d but its ICC profile header is too short", label, nVal)...)
		return
	}

	want, known := iccColorSpaceComponents[hdr.colorSpace]
	if !known || int(nVal) != want {
		*results = append(*results, failf(rule,
			"%s: /DestOutputProfile states /N %d for ICC colour space %q", label, nVal, hdr.colorSpace)...)
	}
}

// checkICCOutputProfileHeader validates the ICC profile stream referenced by an
// output intent's /DestOutputProfile: device class prtr or mntr, colour space
// GRAY, RGB or CMYK, ICC major version below 5 (ISO 19005-1 §6.2.2 t1,
// ISO 19005-2/3/4 §6.2.3 t1). label locates the intent in every message.
// It returns the parsed header and whether the profile decoded and parsed.
func checkICCOutputProfileHeader(ctx context.Context, doc *document.Document, destProfileObj ast.PdfObject, part int, label string, results *[]Violation) (iccProfileHeader, bool) {
	stream, err := document.ResolveStream(ctx, doc, destProfileObj)
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot resolve DestOutputProfile stream", "intent", label, "error", err)
		return iccProfileHeader{}, false
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.color: cannot decode ICC profile stream", "intent", label, "error", err)
		*results = append(*results, failf(outputIntentICCRule(part), "%s: failed to decode ICC profile", label)...)
		return iccProfileHeader{}, false
	}

	hdr, ok := parseICCHeader(data)
	if !ok {
		*results = append(*results, failf(outputIntentICCRule(part), "%s: ICC profile header too short", label)...)
		return iccProfileHeader{}, false
	}

	if !validICCDeviceClasses[hdr.deviceClass] {
		*results = append(*results, failf(outputIntentICCRule(part),
			"%s: invalid device class %q (expected prtr or mntr)", label, hdr.deviceClass)...)
	}

	if !validICCColorSpaces[hdr.colorSpace] {
		*results = append(*results, failf(outputIntentICCRule(part),
			"%s: invalid colour space %q (expected GRAY, RGB  or CMYK)", label, hdr.colorSpace)...)
	}

	if hdr.version != 2 && hdr.version != 4 {
		*results = append(*results, failf(outputIntentICCRule(part),
			"%s: invalid ICC profile version %d (expected 2 or 4)", label, hdr.version)...)
	}

	return hdr, true
}

// checkPageOutputIntents applies ISO 19005-4 §6.2.3 to page-level
// /OutputIntents arrays. PDF 2.0 admits /OutputIntents in a page dictionary
// (ISO 32000-2:2020, 14.11.5), so the clause governs those arrays exactly as it
// governs the catalog's. Parts 1-3 derive from PDF 1.4/1.7, where a page
// dictionary has no /OutputIntents entry, so they stay catalog-only; veraPDF
// 1.30.2 likewise reports §6.2.3 against page output intents for flavour 4
// alone.
//
// Scope matches the profile rules: t1 and t3 apply to every output intent
// whatever its /S subtype, and t2 compares entries within one array, so a page
// array is never checked against the catalog array.
func checkPageOutputIntents(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, results *[]Violation) {
	if part != 4 {
		return
	}

	for pageIdx, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return
		}

		oiObj, ok := page["OutputIntents"]
		if !ok {
			continue
		}

		intents, err := document.ResolveArray(ctx, doc, oiObj)
		if err != nil || len(intents) == 0 {
			doc.Logger.Debug("compliance.color: cannot resolve page OutputIntents", "page", pageIdx+1, "error", err)
			continue
		}

		var firstProfileRef ast.PdfObject

		for i, intentObj := range intents {
			intent, err := document.ResolveDict(ctx, doc, intentObj)
			if err != nil || intent == nil {
				doc.Logger.Debug("compliance.color: cannot resolve page OutputIntent dict", "page", pageIdx+1, "index", i, "error", err)
				continue
			}

			label := fmt.Sprintf("page %d OutputIntent[%d]", pageIdx+1, i)

			// §6.2.3 t3: /DestOutputProfileRef shall not appear in any output
			// intent dictionary (ISO 32000-2:2020, 14.11.5, Table 401).
			if _, has := intent["DestOutputProfileRef"]; has {
				*results = append(*results, failf(Rule_19005_4_6_2_3_t03,
					"%s: contains forbidden /DestOutputProfileRef", label)...)
			}

			destProfileObj, ok := intent["DestOutputProfile"]
			if !ok || resourceEntryNull(ctx, doc, destProfileObj) {
				continue
			}

			if _, ok := checkICCOutputProfileHeader(ctx, doc, destProfileObj, part, label, results); !ok {
				continue
			}

			// §6.2.3 t2: every entry of one /OutputIntents array that carries
			// /DestOutputProfile shall name the same indirect object.
			if firstProfileRef == nil {
				firstProfileRef = destProfileObj
				continue
			}

			if !sameIndirectRef(firstProfileRef, destProfileObj) {
				*results = append(*results, failf(outputIntentConsistencyRule(part),
					"%s: DestOutputProfile does not reference same object as first entry", label)...)
			}
		}
	}
}

// resolveOutputIntentICCInfo returns the indirect reference, parsed header, decoded
// data, and decoded size of the ICC profile from the first OutputIntent with a
// DestOutputProfile.
func resolveOutputIntentICCInfo(ctx context.Context, doc *document.Document, catalog ast.PdfDict) (ast.PdfIndirectRef, iccProfileHeader, []byte) {
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
		if !ok || resourceEntryNull(ctx, doc, destObj) {
			continue
		}

		var ref ast.PdfIndirectRef
		if r, ok := destObj.(ast.PdfIndirectRef); ok {
			ref = r
		}

		stream, err := document.ResolveStream(ctx, doc, destObj)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve output profile stream", "error", err)
			continue
		}

		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot decode output profile stream", "error", err)
			continue
		}

		hdr, ok := parseICCHeader(data)
		if !ok {
			continue
		}

		return ref, hdr, data
	}

	return ast.PdfIndirectRef{}, iccProfileHeader{}, nil
}

// outputIntentHasDestProfile reports whether the catalog declares an
// OutputIntent that carries a /DestOutputProfile, regardless of whether that
// profile can be decoded. The §6.5.3 t3 annotation-colour check uses it to tell
// "no output profile to read" (definitively not RGB) from "a profile is present
// but litho could not parse it" (stay lenient to avoid a false reject).
func outputIntentHasDestProfile(ctx context.Context, doc *document.Document, catalog ast.PdfDict) bool {
	outputIntents, err := document.ResolveArray(ctx, doc, catalog["OutputIntents"])
	if err != nil {
		return false
	}
	for _, intentObj := range outputIntents {
		intent, err := document.ResolveDict(ctx, doc, intentObj)
		if err != nil || intent == nil {
			continue
		}
		if nonNullKeyPresent(ctx, doc, intent, "DestOutputProfile") {
			return true
		}
	}
	return false
}
