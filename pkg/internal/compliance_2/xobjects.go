package compliance2

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkXObjectGroup validates XObject restrictions for PDF/A conformance.
// It checks reference XObjects, form XObject forbidden keys, image /Alternates,
// image /OPI, and image /Interpolate constraints across all pages.
func checkXObjectGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if isPDFUA(profile) {
		slog.Debug("compliance_2: xobjects: skipping for PDF/UA profile", "profile", profile)
		return nil
	}

	part := pdfaPart(profile)
	if part == 0 {
		slog.Debug("compliance_2: xobjects: unknown profile part", "profile", profile)
		return nil
	}

	slog.Debug("compliance_2: xobjects: starting checks", "profile", profile, "part", part)

	pages := collectPages(ctx, doc, catalog)
	var results []Result

	for i, page := range pages {
		if ctx.Err() != nil {
			slog.Debug("compliance_2: xobjects: context cancelled")
			return results
		}

		res := getResources(ctx, doc, page)
		if res == nil {
			continue
		}

		xobjectDict, err := document.ResolveDict(ctx, doc, res["XObject"])
		if err != nil || xobjectDict == nil {
			continue
		}

		slog.Debug("compliance_2: xobjects: checking page", "page", i+1, "xobjects", len(xobjectDict))

		for name, obj := range xobjectDict {
			if ctx.Err() != nil {
				slog.Debug("compliance_2: xobjects: context cancelled")
				return results
			}

			xobjDict, err := document.ResolveDict(ctx, doc, obj)
			if err != nil || xobjDict == nil {
				slog.Debug("compliance_2: xobjects: could not resolve XObject", "name", name, "error", err)
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, xobjDict["Subtype"])

			slog.Debug("compliance_2: xobjects: checking XObject", "page", i+1, "name", name, "subtype", subtype)

			results = append(results, checkXObject(ctx, doc, xobjDict, subtype, name, i+1, part, profile)...)
		}
	}

	slog.Debug("compliance_2: xobjects: checks complete", "violations", len(results))
	return results
}

// checkXObject validates a single XObject against the constraints for the given
// PDF/A part.
func checkXObject(
	ctx context.Context,
	doc *document.Document,
	xobjDict ast.PdfDict,
	subtype ast.PdfName,
	name ast.PdfName,
	pageNum int,
	part int,
	profile string,
) []Result {
	var results []Result

	// §6.2.9 — Reference XObjects forbidden (all parts).
	// A /Ref key in the XObject dict indicates a reference XObject.
	if _, hasRef := xobjDict["Ref"]; hasRef {
		slog.Debug("compliance_2: xobjects: found /Ref key (reference XObject)",
			"page", pageNum, "name", name)
		results = append(results, refXObjectRule(part, pageNum, name)...)
	}

	// PostScript XObjects forbidden (all parts).
	if subtype == "PS" {
		slog.Debug("compliance_2: xobjects: found PostScript XObject",
			"page", pageNum, "name", name)
		results = append(results, psXObjectRule(part, pageNum, name)...)
	}

	// Parts 2+/4 have additional form and image XObject checks.
	if part >= 2 {
		switch subtype {
		case "Form":
			results = append(results, checkFormXObject(ctx, doc, xobjDict, name, pageNum, part, profile)...)
		case "Image":
			results = append(results, checkImageXObject(ctx, doc, xobjDict, name, pageNum, part)...)
		}
	} else {
		// PDF/A-1 also checks form XObject forbidden keys (§6.2.5).
		if subtype == "Form" {
			results = append(results, checkFormXObjectPart1(ctx, doc, xobjDict, name, pageNum)...)
		}
		// PDF/A-1 also checks image constraints (§6.2.4).
		if subtype == "Image" {
			results = append(results, checkImageXObjectPart1(ctx, doc, xobjDict, name, pageNum)...)
		}
	}

	return results
}

// checkFormXObjectPart1 checks form XObject constraints for PDF/A-1 (§6.2.5).
func checkFormXObjectPart1(
	ctx context.Context,
	doc *document.Document,
	formDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
) []Result {
	var results []Result

	// /Subtype2 with value /PS is forbidden.
	if sub2Obj, hasSub2 := formDict["Subtype2"]; hasSub2 {
		sub2Name, _ := document.ResolveName(ctx, doc, sub2Obj)
		if sub2Name == "PS" {
			slog.Debug("compliance_2: xobjects: form has /Subtype2 = /PS",
				"page", pageNum, "name", name)
			results = append(results, failf(Rule_19005_1_6_2_5_t01, "page %d, XObject %s", pageNum, name)...)
		}
	}

	// /PS key is forbidden in form XObject.
	if _, hasPS := formDict["PS"]; hasPS {
		slog.Debug("compliance_2: xobjects: form has /PS key",
			"page", pageNum, "name", name)
		results = append(results, failf(Rule_19005_1_6_2_5_t01, "page %d, XObject %s", pageNum, name)...)
	}

	return results
}

// checkImageXObjectPart1 checks image XObject constraints for PDF/A-1 (§6.2.4).
func checkImageXObjectPart1(
	ctx context.Context,
	doc *document.Document,
	imgDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
) []Result {
	var results []Result

	// /Alternates forbidden.
	if _, hasAlt := imgDict["Alternates"]; hasAlt {
		slog.Debug("compliance_2: xobjects: image has /Alternates",
			"page", pageNum, "name", name)
		results = append(results, failf(Rule_19005_1_6_2_4_t01, "page %d, XObject %s", pageNum, name)...)
	}

	// /OPI forbidden.
	if _, hasOPI := imgDict["OPI"]; hasOPI {
		slog.Debug("compliance_2: xobjects: image has /OPI",
			"page", pageNum, "name", name)
		results = append(results, failf(Rule_19005_1_6_2_4_t02, "page %d, XObject %s", pageNum, name)...)
	}

	// /Interpolate must be false or absent.
	if interpObj, hasInterp := imgDict["Interpolate"]; hasInterp {
		interpVal, err := document.ResolveBool(ctx, doc, interpObj)
		if err == nil && bool(interpVal) {
			slog.Debug("compliance_2: xobjects: image has /Interpolate = true",
				"page", pageNum, "name", name)
			results = append(results, failf(Rule_19005_1_6_2_4_t03, "page %d, XObject %s", pageNum, name)...)
		}
	}

	return results
}

// checkFormXObject checks form XObject constraints for PDF/A parts 2+ and 4.
func checkFormXObject(
	ctx context.Context,
	doc *document.Document,
	formDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
	part int,
	profile string,
) []Result {
	var results []Result

	// Select the appropriate rule based on part.
	var formRule Rule
	switch part {
	case 2, 3:
		formRule = Rule_19005_2_6_2_9_t01
	case 4:
		formRule = Rule_19005_4_6_2_8_1_t01
	}

	// /OPI key forbidden in form XObject.
	if _, hasOPI := formDict["OPI"]; hasOPI {
		slog.Debug("compliance_2: xobjects: form has /OPI key",
			"page", pageNum, "name", name)
		results = append(results, failf(formRule, "page %d, XObject %s: OPI key present", pageNum, name)...)
	}

	// Parts 2/3: /Subtype2 with value /PS forbidden, /PS key forbidden.
	if part == 2 || part == 3 {
		if sub2Obj, hasSub2 := formDict["Subtype2"]; hasSub2 {
			sub2Name, _ := document.ResolveName(ctx, doc, sub2Obj)
			if sub2Name == "PS" {
				slog.Debug("compliance_2: xobjects: form has /Subtype2 = /PS",
					"page", pageNum, "name", name)
				results = append(results, failf(Rule_19005_2_6_2_9_t01, "page %d, XObject %s: Subtype2 is PS", pageNum, name)...)
			}
		}

		if _, hasPS := formDict["PS"]; hasPS {
			slog.Debug("compliance_2: xobjects: form has /PS key",
				"page", pageNum, "name", name)
			results = append(results, failf(Rule_19005_2_6_2_9_t01, "page %d, XObject %s: PS key present", pageNum, name)...)
		}
	}

	return results
}

// checkImageXObject checks image XObject constraints for PDF/A parts 2+ and 4.
func checkImageXObject(
	ctx context.Context,
	doc *document.Document,
	imgDict ast.PdfDict,
	name ast.PdfName,
	pageNum int,
	part int,
) []Result {
	var results []Result

	// Select the appropriate rules based on part.
	var altRule, opiRule, interpRule Rule
	switch part {
	case 2, 3:
		altRule = Rule_19005_2_6_2_8_t01
		opiRule = Rule_19005_2_6_2_8_t02
		interpRule = Rule_19005_2_6_2_8_t03
	case 4:
		altRule = Rule_19005_4_6_2_7_1_t01
		opiRule = Rule_19005_4_6_2_7_1_t02
		interpRule = Rule_19005_4_6_2_7_1_t03
	}

	// /Alternates forbidden (§6.2.7/6.2.8).
	if _, hasAlt := imgDict["Alternates"]; hasAlt {
		slog.Debug("compliance_2: xobjects: image has /Alternates",
			"page", pageNum, "name", name)
		results = append(results, failf(altRule, "page %d, XObject %s", pageNum, name)...)
	}

	// /OPI forbidden (§6.2.8).
	if _, hasOPI := imgDict["OPI"]; hasOPI {
		slog.Debug("compliance_2: xobjects: image has /OPI",
			"page", pageNum, "name", name)
		results = append(results, failf(opiRule, "page %d, XObject %s", pageNum, name)...)
	}

	// /Interpolate must be false or absent (§6.2.8).
	if interpObj, hasInterp := imgDict["Interpolate"]; hasInterp {
		interpVal, err := document.ResolveBool(ctx, doc, interpObj)
		if err == nil && bool(interpVal) {
			slog.Debug("compliance_2: xobjects: image has /Interpolate = true",
				"page", pageNum, "name", name)
			results = append(results, failf(interpRule, "page %d, XObject %s", pageNum, name)...)
		}
	}

	return results
}

// refXObjectRule returns the appropriate reference XObject rule violation for
// the given PDF/A part.
func refXObjectRule(part int, pageNum int, name ast.PdfName) []Result {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_2_6_t01, "page %d, XObject %s", pageNum, name)
	case 2, 3:
		return failf(Rule_19005_2_6_2_9_t02, "page %d, XObject %s", pageNum, name)
	case 4:
		return failf(Rule_19005_4_6_2_8_2_t01, "page %d, XObject %s", pageNum, name)
	}
	return nil
}

// psXObjectRule returns the appropriate PostScript XObject rule violation for
// the given PDF/A part.
func psXObjectRule(part int, pageNum int, name ast.PdfName) []Result {
	switch part {
	case 1:
		return failf(Rule_19005_1_6_2_7_t01, "page %d, XObject %s", pageNum, name)
	case 2, 3:
		return failf(Rule_19005_2_6_2_9_t03, "page %d, XObject %s", pageNum, name)
		// PDF/A-4 does not have a separate PS XObject rule; it's covered by form checks.
	}
	return nil
}
