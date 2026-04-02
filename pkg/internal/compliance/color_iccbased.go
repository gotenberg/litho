package compliance

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// --- Rule selectors ---

func iccBasedProfileRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_2_t01, Rule_19005_2_6_2_4_2_t01, Rule_19005_3_6_2_4_2_t01, Rule_19005_4_6_2_4_2_t01)
}

func iccBasedNRule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_2_3_2_t02, Rule_19005_2_6_2_4_2_t01, Rule_19005_3_6_2_4_2_t01, Rule_19005_4_6_2_4_2_t01)
}

// iccBasedIdentityRule returns the rule for ICCBased profiles identical to
// the OutputIntent.
func iccBasedIdentityRule(part int) Rule {
	// Part 1 does not have this rule; return part 2 as fallback.
	return ruleByPart(part, Rule_19005_2_6_2_4_2_t02, Rule_19005_2_6_2_4_2_t02, Rule_19005_3_6_2_4_2_t02, Rule_19005_4_6_2_4_2_t03)
}

// --- Check implementations ---

// checkICCBasedProfiles validates ICCBased colour space profiles per
// §6.2.3.2 (part 1) or §6.2.4.2 (parts 2-4).
func checkICCBasedProfiles(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int, results *[]Violation) {
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

		// For page-level OutputIntents (PDF/A-4 allows them), use the page's
		// OutputIntent ICC info if the catalog-level one is absent.
		pageOIRef, pageOIHdr, pageOIData := outputIntentRef, outputIntentHdr, outputIntentData
		if part >= 2 && len(outputIntentData) == 0 {
			pageOIRef, pageOIHdr, pageOIData = getOutputIntentICCInfo(ctx, doc, page)
		}

		// Get page blending CS ICC ref for identity comparison.
		blendingCSRef := getPageBlendingICCRef(ctx, doc, page)

		// Collect CS names actually used by cs/CS operators in the content stream.
		usedCSNames := collectUsedCSNames(ctx, doc, page)

		checkICCBasedInResources(ctx, doc, resources, part, i, pageOIRef, pageOIHdr, pageOIData, blendingCSRef, usedCSNames, results)

		// Recursively scan Form XObject and annotation sub-resources.
		visited := make(map[string]bool)
		scanICCBasedRecursive(ctx, doc, resources, part, i, pageOIRef, pageOIHdr, pageOIData, blendingCSRef, results, 0, visited)

		// Scan annotation appearance streams.
		annotsArr, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annotsArr {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			apDict, _ := document.ResolveDict(ctx, doc, annot["AP"])
			if apDict == nil {
				continue
			}
			for _, key := range []ast.PdfName{"N", "R", "D"} {
				apObj, ok := apDict[key]
				if !ok {
					continue
				}
				stream, err := document.ResolveStream(ctx, doc, apObj)
				if err != nil {
					slog.Debug("compliance.color: cannot resolve appearance stream", "error", err)
					continue
				}
				subRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
				if subRes != nil {
					formBlendRef := resolveFormBlendingRef(ctx, doc, stream, blendingCSRef)
					checkICCBasedInResources(ctx, doc, subRes, part, i, pageOIRef, pageOIHdr, pageOIData, formBlendRef, nil, results)
					scanICCBasedRecursive(ctx, doc, subRes, part, i, pageOIRef, pageOIHdr, pageOIData, formBlendRef, results, 1, visited)
				}
			}
		}
	}
}

// scanICCBasedRecursive scans Form XObject sub-resources recursively for
// ICCBased colour spaces.
func scanICCBasedRecursive(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation, depth int, visited map[string]bool) {
	if depth > maxFormXObjectDepth || ctx.Err() != nil {
		return
	}

	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return
	}

	for _, xobjObj := range xobjDict {
		if ctx.Err() != nil {
			return
		}
		// Cycle detection.
		if ref, ok := xobjObj.(ast.PdfIndirectRef); ok {
			key := fmt.Sprintf("icc-%d", ref.ObjectNum)
			if visited[key] {
				continue
			}
			visited[key] = true
		}
		stream, err := document.ResolveStream(ctx, doc, xobjObj)
		if err != nil {
			slog.Debug("compliance.color: cannot resolve XObject stream", "error", err)
			continue
		}
		st, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		if st != "Form" {
			continue
		}
		subRes, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		if subRes == nil {
			continue
		}
		formBlendRef := resolveFormBlendingRef(ctx, doc, stream, blendingCSRef)
		checkICCBasedInResources(ctx, doc, subRes, part, pageIdx, outputIntentRef, outputIntentHdr, outputIntentData, formBlendRef, nil, results)
		scanICCBasedRecursive(ctx, doc, subRes, part, pageIdx, outputIntentRef, outputIntentHdr, outputIntentData, formBlendRef, results, depth+1, visited)
	}
}

// resolveFormBlendingRef extracts the ICCBased blending CS ref from a Form
// XObject's Group CS, falling back to the parent's blending ref.
func resolveFormBlendingRef(ctx context.Context, doc *document.Document, stream ast.PdfStream, parentRef ast.PdfIndirectRef) ast.PdfIndirectRef {
	grp, _ := document.ResolveDict(ctx, doc, stream.Dict["Group"])
	if grp == nil {
		return parentRef
	}
	csObj, ok := grp["CS"]
	if !ok {
		return parentRef
	}
	csArr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(csArr) < 2 {
		return parentRef
	}
	csT, _ := document.ResolveName(ctx, doc, csArr[0])
	if csT != "ICCBased" {
		return parentRef
	}
	if ref, ok := csArr[1].(ast.PdfIndirectRef); ok {
		return ref
	}
	return parentRef
}

// checkICCBasedInResources scans a page's resources for ICCBased colour spaces
// and validates them.
func checkICCBasedInResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, part int, pageIdx int, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, usedCSNames map[string]bool, results *[]Violation) {
	csDict, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
	if err != nil || csDict == nil {
		return
	}

	for name, csObj := range csDict {
		if ctx.Err() != nil {
			return
		}

		// Skip CS names not referenced by cs/CS operators in the content stream.
		// A CS declared in Resources but never used is not a violation.
		if usedCSNames != nil && !usedCSNames[string(name)] {
			continue
		}

		arr, err := document.ResolveArray(ctx, doc, csObj)
		if err != nil || len(arr) < 2 {
			continue
		}

		csType, err := document.ResolveName(ctx, doc, arr[0])
		if err != nil {
			slog.Debug("compliance.color: cannot resolve color space type name", "error", err)
			continue
		}

		// For Separation/DeviceN, check the alternate colour space for ICCBased.
		if csType == "Separation" && len(arr) >= 3 {
			altArr, altErr := document.ResolveArray(ctx, doc, arr[2])
			if altErr == nil && len(altArr) >= 2 {
				altType, _ := document.ResolveName(ctx, doc, altArr[0])
				if altType == "ICCBased" {
					checkSingleICCBased(ctx, doc, altArr[1], part, pageIdx, name, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
				}
			}
			continue
		}
		if csType == "DeviceN" && len(arr) >= 4 {
			altArr, altErr := document.ResolveArray(ctx, doc, arr[2])
			if altErr == nil && len(altArr) >= 2 {
				altType, _ := document.ResolveName(ctx, doc, altArr[0])
				if altType == "ICCBased" {
					checkSingleICCBased(ctx, doc, altArr[1], part, pageIdx, name, outputIntentRef, outputIntentHdr, outputIntentData, blendingCSRef, results)
				}
			}
			continue
		}

		if csType != "ICCBased" {
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, arr[1])
		if err != nil {
			slog.Debug("compliance.color: cannot resolve ICCBased stream", "page", pageIdx, "name", name, "error", err)
			*results = append(*results, failf(iccBasedProfileRule(part),
				"page %d, ColorSpace %s: cannot resolve ICCBased profile stream", pageIdx+1, name)...)
			continue
		}

		// Validate /N matches the ICC profile colour space.
		nVal, err := document.ResolveInt(ctx, doc, stream.Dict["N"])
		if err != nil {
			*results = append(*results, failf(iccBasedNRule(part),
				"page %d, ColorSpace %s: ICCBased profile missing /N entry", pageIdx+1, name)...)
			continue
		}

		data, err := doc.DecodeStream(stream)
		if err != nil {
			slog.Debug("compliance.color: cannot decode ICCBased stream", "page", pageIdx, "name", name, "error", err)
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
			*results = append(*results, failf(iccBasedNRule(part),
				"page %d, ColorSpace %s: /N=%d does not match ICC colour space %q", pageIdx+1, name, nVal, hdr.colorSpace)...)
		}

		// Parts 2+: check whether the ICCBased profile is identical to the
		// OutputIntent profile OR the transparency blending CS profile.
		// Per clause 6.2.4.2, this restriction applies only when the profile
		// is CMYK. Compare by (1) same indirect reference, or (2) byte-equal data.
		if part >= 2 && hdr.colorSpace == "CMYK" {
			iccRef, _ := arr[1].(ast.PdfIndirectRef)
			identical := false

			// Check against OutputIntent profile.
			if len(outputIntentData) > 0 && outputIntentHdr.colorSpace == "CMYK" {
				if iccRef.ObjectNum > 0 &&
					outputIntentRef.ObjectNum > 0 &&
					iccRef.ObjectNum == outputIntentRef.ObjectNum &&
					iccRef.GenNum == outputIntentRef.GenNum {
					identical = true
				}
				if !identical && iccProfilesEqual(data, outputIntentData) {
					identical = true
				}
			}

			// Check against page transparency blending CS profile.
			if !identical && blendingCSRef.ObjectNum > 0 {
				if iccRef.ObjectNum > 0 &&
					iccRef.ObjectNum == blendingCSRef.ObjectNum &&
					iccRef.GenNum == blendingCSRef.GenNum {
					identical = true
				}
				if !identical {
					// Compare by decoded data if references differ.
					blendStream, err := document.ResolveStream(ctx, doc, blendingCSRef)
					if err == nil {
						blendData, err := doc.DecodeStream(blendStream)
						if err == nil && iccProfilesEqual(data, blendData) {
							identical = true
						}
					}
				}
			}

			if identical {
				*results = append(*results, failf(iccBasedIdentityRule(part),
					"page %d, ColorSpace %s: ICCBased profile is identical to OutputIntent", pageIdx+1, name)...)
			}
		}
	}
}

// checkSingleICCBased validates a single ICCBased profile stream for the CMYK
// identity restriction (§6.2.4.2 t3). Used for ICCBased profiles found inside
// Separation/DeviceN alternate colour spaces.
func checkSingleICCBased(ctx context.Context, doc *document.Document, profileObj ast.PdfObject, part int, pageIdx int, csName ast.PdfName, outputIntentRef ast.PdfIndirectRef, outputIntentHdr iccProfileHeader, outputIntentData []byte, blendingCSRef ast.PdfIndirectRef, results *[]Violation) {
	if part < 2 {
		return
	}

	stream, err := document.ResolveStream(ctx, doc, profileObj)
	if err != nil {
		return
	}
	data, err := doc.DecodeStream(stream)
	if err != nil {
		return
	}
	hdr, ok := parseICCHeader(data)
	if !ok || hdr.colorSpace != "CMYK" {
		return
	}

	iccRef, _ := profileObj.(ast.PdfIndirectRef)
	identical := false

	if len(outputIntentData) > 0 && outputIntentHdr.colorSpace == "CMYK" {
		if iccRef.ObjectNum > 0 && outputIntentRef.ObjectNum > 0 &&
			iccRef.ObjectNum == outputIntentRef.ObjectNum && iccRef.GenNum == outputIntentRef.GenNum {
			identical = true
		}
		if !identical && iccProfilesEqual(data, outputIntentData) {
			identical = true
		}
	}

	if !identical && blendingCSRef.ObjectNum > 0 {
		if iccRef.ObjectNum > 0 && iccRef.ObjectNum == blendingCSRef.ObjectNum && iccRef.GenNum == blendingCSRef.GenNum {
			identical = true
		}
		if !identical {
			blendStream, blendErr := document.ResolveStream(ctx, doc, blendingCSRef)
			if blendErr == nil {
				blendData, blendErr := doc.DecodeStream(blendStream)
				if blendErr == nil && iccProfilesEqual(data, blendData) {
					identical = true
				}
			}
		}
	}

	if identical {
		*results = append(*results, failf(iccBasedIdentityRule(part),
			"page %d, ColorSpace %s alternate: ICCBased CMYK profile identical to OutputIntent", pageIdx+1, csName)...)
	}
}

// getPageBlendingICCRef returns the indirect reference to the ICCBased profile
// stream used as the page's transparency blending CS, or a zero ref if absent.
func getPageBlendingICCRef(ctx context.Context, doc *document.Document, page ast.PdfDict) ast.PdfIndirectRef {
	groupObj, ok := page["Group"]
	if !ok {
		return ast.PdfIndirectRef{}
	}
	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return ast.PdfIndirectRef{}
	}
	s, _ := document.ResolveName(ctx, doc, group["S"])
	if s != "Transparency" {
		return ast.PdfIndirectRef{}
	}
	csObj, ok := group["CS"]
	if !ok {
		return ast.PdfIndirectRef{}
	}
	arr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(arr) < 2 {
		return ast.PdfIndirectRef{}
	}
	csType, _ := document.ResolveName(ctx, doc, arr[0])
	if csType != "ICCBased" {
		return ast.PdfIndirectRef{}
	}
	if ref, ok := arr[1].(ast.PdfIndirectRef); ok {
		return ref
	}
	return ast.PdfIndirectRef{}
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
