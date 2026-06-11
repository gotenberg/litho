package compliance

import (
	"context"

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

		destProfileObj, ok := intent["DestOutputProfile"]
		if !ok {
			// §6.2.2: OutputIntent without DestOutputProfile must have
			// OutputConditionIdentifier referencing a well-known registry.
			if _, hasOCI := intent["OutputConditionIdentifier"]; !hasOCI {
				*results = append(*results, failf(outputIntentICCRule(part),
					"OutputIntent[%d]: missing DestOutputProfile and OutputConditionIdentifier", i)...)
			}
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, destProfileObj)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot resolve DestOutputProfile stream", "index", i, "error", err)
			continue
		}

		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("compliance.color: cannot decode ICC profile stream", "index", i, "error", err)
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
			*results = append(*results, failf(outputIntentICCRule(part),
				"OutputIntent[%d]: invalid device class %q (expected prtr or mntr)", i, hdr.deviceClass)...)
		}

		// Validate colour space.
		if !validICCColorSpaces[hdr.colorSpace] {
			*results = append(*results, failf(outputIntentICCRule(part),
				"OutputIntent[%d]: invalid colour space %q (expected GRAY, RGB  or CMYK)", i, hdr.colorSpace)...)
		} else if isPDFAIntent {
			// Only PDF/A OutputIntents grant device CS coverage.
			iccColorSpace = hdr.colorSpace
			hasValidICC = true
		}

		// Validate ICC profile version (must be 2 or 4, not 5+).
		if hdr.version != 2 && hdr.version != 4 {
			*results = append(*results, failf(outputIntentICCRule(part),
				"OutputIntent[%d]: invalid ICC profile version %d (expected 2 or 4)", i, hdr.version)...)
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
		if !ok {
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
