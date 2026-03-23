package compliance

import (
	"context"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkTransparencyGroup validates transparency restrictions required by
// PDF/A parts 1-4. PDF/UA profiles have no transparency checks.
func checkTransparencyGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	part := pdfaPart(profile)
	if part == 0 {
		slog.Debug("compliance_2/transparency: skipping, not a PDF/A profile", "profile", profile)
		return nil
	}

	slog.Debug("compliance_2/transparency: starting checks", "profile", profile, "part", part)

	pages := collectPages(ctx, doc, catalog)

	var results []Result
	if part == 1 {
		results = checkTransparencyPart1(ctx, doc, pages)
	} else {
		results = checkTransparencyParts2Plus(ctx, doc, catalog, pages, profile, part)
	}

	slog.Debug("compliance_2/transparency: checks complete", "violations", len(results))
	return results
}

// --- PDF/A-1: transparency is entirely forbidden (§6.4) ---

func checkTransparencyPart1(ctx context.Context, doc *document.Document, pages []ast.PdfDict) []Result {
	var results []Result
	for i, page := range pages {
		if ctx.Err() != nil {
			slog.Debug("compliance_2/transparency: context cancelled during page iteration")
			return results
		}

		slog.Debug("compliance_2/transparency: checking page (part 1)", "page", i+1)

		// Check page-level transparency group.
		results = append(results, checkPageTransparencyGroup1(ctx, doc, page, i+1)...)

		res := getResources(ctx, doc, page)
		if res == nil {
			continue
		}

		// Check ExtGState entries for transparency.
		results = append(results, checkExtGStateTransparency1(ctx, doc, res, i+1)...)

		// Check XObject entries for SMask.
		results = append(results, checkXObjectTransparency1(ctx, doc, res, i+1)...)
	}
	return results
}

// checkPageTransparencyGroup1 checks for forbidden transparency Group on a page (PDF/A-1).
func checkPageTransparencyGroup1(ctx context.Context, doc *document.Document, page ast.PdfDict, pageNum int) []Result {
	groupObj, hasGroup := page["Group"]
	if !hasGroup {
		return nil
	}

	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return nil
	}

	sName, _ := document.ResolveName(ctx, doc, group["S"])
	if sName == "Transparency" {
		slog.Debug("compliance_2/transparency: page has transparency group (forbidden in part 1)",
			"page", pageNum)
		return failf(Rule_19005_1_6_4_t03, "page %d", pageNum)
	}
	return nil
}

// checkExtGStateTransparency1 checks all ExtGState entries for transparency
// (SMask, BM, CA, ca) which are forbidden in PDF/A-1.
func checkExtGStateTransparency1(ctx context.Context, doc *document.Document, resources ast.PdfDict, pageNum int) []Result {
	gsDict, err := document.ResolveDict(ctx, doc, resources["ExtGState"])
	if err != nil || gsDict == nil {
		return nil
	}

	var results []Result
	for gsName, gsObj := range gsDict {
		if ctx.Err() != nil {
			slog.Debug("compliance_2/transparency: context cancelled during ExtGState iteration")
			return results
		}

		gs, err := document.ResolveDict(ctx, doc, gsObj)
		if err != nil || gs == nil {
			continue
		}

		slog.Debug("compliance_2/transparency: checking ExtGState (part 1)",
			"name", string(gsName), "page", pageNum)

		// SMask must be None or absent.
		if smaskObj, hasSMask := gs["SMask"]; hasSMask {
			smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
			if smaskName != "None" {
				slog.Debug("compliance_2/transparency: ExtGState has SMask other than None",
					"name", string(gsName), "page", pageNum)
				results = append(results, failf(Rule_19005_1_6_4_t01, "page %d, ExtGState %s", pageNum, string(gsName))...)
			}
		}

		// BM must be Normal or Compatible.
		if bmObj, hasBM := gs["BM"]; hasBM {
			bm := resolveBlendMode(ctx, doc, bmObj)
			if bm != "" && bm != "Normal" && bm != "Compatible" {
				slog.Debug("compliance_2/transparency: ExtGState has non-Normal/Compatible BM",
					"name", string(gsName), "page", pageNum, "bm", bm)
				results = append(results, failf(Rule_19005_1_6_4_t04, "page %d, ExtGState %s, BM=%s", pageNum, string(gsName), bm)...)
			}
		}

		// CA (stroke alpha) must be 1.0.
		if caObj, hasCA := gs["CA"]; hasCA {
			ca := document.GetFloat(caObj)
			if !floatEquals1(ca) {
				slog.Debug("compliance_2/transparency: ExtGState has CA != 1.0",
					"name", string(gsName), "page", pageNum, "CA", ca)
				results = append(results, failf(Rule_19005_1_6_4_t05, "page %d, ExtGState %s, CA=%g", pageNum, string(gsName), ca)...)
			}
		}

		// ca (fill alpha) must be 1.0.
		if caObj, hasCa := gs["ca"]; hasCa {
			ca := document.GetFloat(caObj)
			if !floatEquals1(ca) {
				slog.Debug("compliance_2/transparency: ExtGState has ca != 1.0",
					"name", string(gsName), "page", pageNum, "ca", ca)
				results = append(results, failf(Rule_19005_1_6_4_t06, "page %d, ExtGState %s, ca=%g", pageNum, string(gsName), ca)...)
			}
		}
	}
	return results
}

// checkXObjectTransparency1 checks XObject entries for forbidden SMask (PDF/A-1).
func checkXObjectTransparency1(ctx context.Context, doc *document.Document, resources ast.PdfDict, pageNum int) []Result {
	xobjDict, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil || xobjDict == nil {
		return nil
	}

	var results []Result
	for xoName, xoObj := range xobjDict {
		if ctx.Err() != nil {
			slog.Debug("compliance_2/transparency: context cancelled during XObject iteration")
			return results
		}

		xo, err := document.ResolveDict(ctx, doc, xoObj)
		if err != nil || xo == nil {
			continue
		}

		// SMask key must not be present on XObjects.
		if _, hasSMask := xo["SMask"]; hasSMask {
			slog.Debug("compliance_2/transparency: XObject has SMask key (forbidden in part 1)",
				"name", string(xoName), "page", pageNum)
			results = append(results, failf(Rule_19005_1_6_4_t02, "page %d, XObject %s", pageNum, string(xoName))...)
		}

		// Check for transparency Group on form XObjects.
		if groupObj, hasGroup := xo["Group"]; hasGroup {
			group, err := document.ResolveDict(ctx, doc, groupObj)
			if err == nil && group != nil {
				sName, _ := document.ResolveName(ctx, doc, group["S"])
				if sName == "Transparency" {
					slog.Debug("compliance_2/transparency: form XObject has transparency group (forbidden in part 1)",
						"name", string(xoName), "page", pageNum)
					results = append(results, failf(Rule_19005_1_6_4_t03, "page %d, XObject %s", pageNum, string(xoName))...)
				}
			}
		}
	}
	return results
}

// --- PDF/A-2+/4: blend mode and blending color space checks ---

// standardBlendModes lists all blend modes defined in ISO 32000-1:2008.
var standardBlendModes = map[string]bool{ //nolint:gochecknoglobals // constant lookup table.
	"Normal":     true,
	"Compatible": true,
	"Multiply":   true,
	"Screen":     true,
	"Overlay":    true,
	"Darken":     true,
	"Lighten":    true,
	"ColorDodge": true,
	"ColorBurn":  true,
	"HardLight":  true,
	"SoftLight":  true,
	"Difference": true,
	"Exclusion":  true,
	"Hue":        true,
	"Saturation": true,
	"Color":      true,
	"Luminosity": true,
}

// blendModeRule returns the rule for invalid blend mode, varying by part.
func blendModeRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_2_10_t01
	case 3:
		return Rule_19005_3_6_2_10_t01
	case 4:
		return Rule_19005_4_6_2_9_t01
	}
	return Rule{}
}

// blendingCSRule returns the rule for missing blending color space on
// transparent pages.
func blendingCSRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_2_10_t02
	case 3:
		return Rule_19005_3_6_2_10_t02
	case 4:
		return Rule_19005_4_6_2_9_t02
	}
	return Rule{}
}

func checkTransparencyParts2Plus(ctx context.Context, doc *document.Document, catalog ast.PdfDict, pages []ast.PdfDict, profile string, part int) []Result {
	hasOutputIntent := documentHasOutputIntent(ctx, doc, catalog)
	slog.Debug("compliance_2/transparency: output intent check", "hasOutputIntent", hasOutputIntent)

	var results []Result
	for i, page := range pages {
		if ctx.Err() != nil {
			slog.Debug("compliance_2/transparency: context cancelled during page iteration")
			return results
		}

		slog.Debug("compliance_2/transparency: checking page (parts 2+)", "page", i+1)

		res := getResources(ctx, doc, page)
		if res == nil {
			continue
		}

		pageUsesTransparency := false

		// Check blend modes in ExtGState.
		gsDict, err := document.ResolveDict(ctx, doc, res["ExtGState"])
		if err == nil && gsDict != nil {
			for gsName, gsObj := range gsDict {
				if ctx.Err() != nil {
					return results
				}

				gs, err := document.ResolveDict(ctx, doc, gsObj)
				if err != nil || gs == nil {
					continue
				}

				slog.Debug("compliance_2/transparency: checking ExtGState (parts 2+)",
					"name", string(gsName), "page", i+1)

				// Validate blend mode.
				if bmObj, hasBM := gs["BM"]; hasBM {
					bm := resolveBlendMode(ctx, doc, bmObj)
					if bm != "" && !standardBlendModes[bm] {
						slog.Debug("compliance_2/transparency: non-standard blend mode",
							"name", string(gsName), "page", i+1, "bm", bm)
						results = append(results, failf(blendModeRule(part), "page %d, ExtGState %s, BM=%s", i+1, string(gsName), bm)...)
					}
					if bm != "" && bm != "Normal" && bm != "Compatible" {
						pageUsesTransparency = true
					}
				}

				// SMask (other than None) indicates transparency.
				if smaskObj, hasSMask := gs["SMask"]; hasSMask {
					smaskName, _ := document.ResolveName(ctx, doc, smaskObj)
					if smaskName != "None" {
						pageUsesTransparency = true
					}
				}

				// Non-1.0 alpha values indicate transparency.
				if caObj, hasCA := gs["CA"]; hasCA {
					if !floatEquals1(document.GetFloat(caObj)) {
						pageUsesTransparency = true
					}
				}
				if caObj, hasCa := gs["ca"]; hasCa {
					if !floatEquals1(document.GetFloat(caObj)) {
						pageUsesTransparency = true
					}
				}
			}
		}

		// If the page uses transparency, it must have a blending colour space.
		// The OutputIntent ICC profile provides a default blending CS for the
		// entire document, so pages without an explicit /Group /CS are valid
		// when an OutputIntent exists.
		if pageUsesTransparency && !hasOutputIntent {
			if !pageHasBlendingCS(ctx, doc, page) {
				slog.Debug("compliance_2/transparency: page uses transparency without blending CS",
					"page", i+1)
				results = append(results, failf(blendingCSRule(part), "page %d", i+1)...)
			}
		} else if pageUsesTransparency && hasOutputIntent {
			slog.Debug("compliance_2/transparency: page uses transparency, OutputIntent provides default blending CS",
				"page", i+1)
		}
	}
	return results
}

// --- Helpers ---

// resolveBlendMode resolves a blend mode value which may be a name or the
// first element of an array of names.
func resolveBlendMode(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	// BM can be a single name.
	if name, _ := document.ResolveName(ctx, doc, obj); name != "" {
		return string(name)
	}
	// Or an array of names — use the first entry.
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) == 0 {
		return ""
	}
	name, _ := document.ResolveName(ctx, doc, arr[0])
	return string(name)
}

// floatEquals1 tests whether a float64 is approximately 1.0.
const alphaEpsilon = 1e-6

func floatEquals1(v float64) bool {
	return math.Abs(v-1.0) < alphaEpsilon
}

// documentHasOutputIntent checks whether the catalog contains an OutputIntents
// array with at least one entry that has a valid DestOutputProfile stream.
// A bare OutputIntents entry without a DestOutputProfile does not provide a
// default blending colour space.
func documentHasOutputIntent(ctx context.Context, doc *document.Document, catalog ast.PdfDict) bool {
	oiObj, hasOI := catalog["OutputIntents"]
	if !hasOI {
		return false
	}

	arr, err := document.ResolveArray(ctx, doc, oiObj)
	if err != nil || len(arr) == 0 {
		return false
	}

	slog.Debug("compliance_2/transparency: found OutputIntents", "count", len(arr))

	// At least one OutputIntent must have a DestOutputProfile stream.
	for _, intentObj := range arr {
		intent, err := document.ResolveDict(ctx, doc, intentObj)
		if err != nil || intent == nil {
			continue
		}
		if destObj, ok := intent["DestOutputProfile"]; ok {
			_, err := document.ResolveStream(ctx, doc, destObj)
			if err == nil {
				slog.Debug("compliance_2/transparency: OutputIntent has DestOutputProfile stream")
				return true
			}
		}
	}

	slog.Debug("compliance_2/transparency: no OutputIntent has a DestOutputProfile stream")
	return false
}

// pageHasBlendingCS checks whether a page dictionary has a Group dict with
// /S = Transparency and a /CS entry.
func pageHasBlendingCS(ctx context.Context, doc *document.Document, page ast.PdfDict) bool {
	groupObj, hasGroup := page["Group"]
	if !hasGroup {
		return false
	}

	group, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || group == nil {
		return false
	}

	sName, _ := document.ResolveName(ctx, doc, group["S"])
	if sName != "Transparency" {
		return false
	}

	_, hasCS := group["CS"]
	return hasCS
}
