package compliance2

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// forbiddenActionTypes lists action /S values forbidden by §6.6.1 across all
// PDF/A parts.
//
//nolint:gochecknoglobals // package-level lookup table.
var forbiddenActionTypes = map[ast.PdfName]bool{
	"Launch":     true,
	"Sound":      true,
	"Movie":      true,
	"ResetForm":  true,
	"ImportData": true,
	"JavaScript": true,
	"Hide":       true,
	"SetState":   true,
	"NoOp":       true,
}

// forbiddenActionTypesPart23 extends the forbidden set for PDF/A-2 and PDF/A-3
// (§6.6.1). These parts additionally forbid SetOCGState, Rendition, Trans,
// and GoTo3DView actions.
//
//nolint:gochecknoglobals // package-level lookup table.
var forbiddenActionTypesPart23 = map[ast.PdfName]bool{
	"Launch":      true,
	"Sound":       true,
	"Movie":       true,
	"ResetForm":   true,
	"ImportData":  true,
	"JavaScript":  true,
	"Hide":        true,
	"SetState":    true,
	"NoOp":        true,
	"SetOCGState": true,
	"Rendition":   true,
	"Trans":       true,
	"GoTo3DView":  true,
}

// forbiddenActionTypesPart4 extends the forbidden set for PDF/A-4 (§6.6.1).
//
//nolint:gochecknoglobals // package-level lookup table.
var forbiddenActionTypesPart4 = map[ast.PdfName]bool{
	"Launch":      true,
	"Sound":       true,
	"Movie":       true,
	"ResetForm":   true,
	"ImportData":  true,
	"JavaScript":  true,
	"Hide":        true,
	"SetState":    true,
	"NoOp":        true,
	"Rendition":   true,
	"Trans":       true,
	"SetOCGState": true,
	"GoTo3DView":  true,
}

// allowedNamedActions lists the named actions permitted by §6.6.1.
//
//nolint:gochecknoglobals // package-level lookup table.
var allowedNamedActions = map[ast.PdfName]bool{
	"NextPage":  true,
	"PrevPage":  true,
	"FirstPage": true,
	"LastPage":  true,
}

// allowedAAKeys lists the keys permitted in /AA dicts on catalogs, pages and
// non-widget annotations (§6.6.3, part 4).
//
//nolint:gochecknoglobals // package-level lookup table.
var allowedAAKeys = map[ast.PdfName]bool{
	"E":  true,
	"X":  true,
	"D":  true,
	"U":  true,
	"Fo": true,
	"Bl": true,
}

// maxActionChainDepth limits traversal of /Next action chains to prevent
// infinite loops from circular references.
const maxActionChainDepth = 200

// checkActionGroup validates action restrictions for PDF/A (§6.6.1, §6.6.2,
// §6.6.3, §6.4.1). For PDF/UA profiles this check is not applicable.
func checkActionGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if isPDFUA(profile) {
		return nil
	}

	part := pdfaPart(profile)
	if part == 0 {
		slog.Debug("compliance_2/actions: unknown profile, skipping", "profile", profile)
		return nil
	}

	slog.Debug("compliance_2/actions: starting action checks", "profile", profile, "part", part)

	results := make([]Result, 0, 8) //nolint:mnd // estimated capacity for action checks.

	// Check catalog /OpenAction.
	results = append(results, checkCatalogOpenAction(ctx, doc, catalog, profile, part)...)

	// Check catalog /AA.
	results = append(results, checkCatalogAA(ctx, doc, catalog, profile, part)...)

	// Check page /AA dicts.
	results = append(results, checkPageActions(ctx, doc, catalog, profile, part)...)

	// Check annotation /A and /AA.
	results = append(results, checkAnnotationActions(ctx, doc, catalog, profile, part)...)

	// Check /Names/JavaScript name tree for JavaScript actions.
	results = append(results, checkJavaScriptNameTree(ctx, doc, catalog, part)...)

	slog.Debug("compliance_2/actions: completed action checks",
		"profile", profile,
		"violations", len(results),
	)

	return results
}

// checkCatalogOpenAction checks the catalog /OpenAction entry for forbidden
// action types and invalid named actions.
func checkCatalogOpenAction(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Result {
	openAction, ok := catalog["OpenAction"]
	if !ok {
		slog.Debug("compliance_2/actions: catalog has no /OpenAction")
		return nil
	}

	// /OpenAction may be an array (destination) or a dict (action).
	actionDict, err := document.ResolveDict(ctx, doc, openAction)
	if err != nil || actionDict == nil {
		slog.Debug("compliance_2/actions: /OpenAction is not a dict (likely a destination array)")
		return nil
	}

	slog.Debug("compliance_2/actions: checking catalog /OpenAction")
	return walkActionChain(ctx, doc, actionDict, profile, part, "catalog /OpenAction", 0)
}

// checkCatalogAA checks the catalog /AA dictionary.
func checkCatalogAA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Result {
	aaObj, ok := catalog["AA"]
	if !ok {
		slog.Debug("compliance_2/actions: catalog has no /AA")
		return nil
	}

	// Part 1: catalog /AA is entirely forbidden (§6.6.2).
	if part == 1 {
		slog.Debug("compliance_2/actions: catalog /AA forbidden for part 1")
		return fail(Rule_19005_1_6_6_2_t03, "catalog")
	}

	// Parts 2/3: catalog /AA is also forbidden (§6.5.2 t01).
	if part == 2 {
		slog.Debug("compliance_2/actions: catalog /AA forbidden for part 2")
		return fail(Rule_19005_2_6_5_2_t01, "catalog")
	}
	if part == 3 {
		slog.Debug("compliance_2/actions: catalog /AA forbidden for part 3")
		return fail(Rule_19005_3_6_5_2_t01, "catalog")
	}

	aaDict, err := document.ResolveDict(ctx, doc, aaObj)
	if err != nil || aaDict == nil {
		slog.Debug("compliance_2/actions: catalog /AA is not a dict", "error", err)
		return nil
	}

	var results []Result

	// Part 4: check AA key restrictions (§6.6.3).
	if part == 4 {
		results = append(results, checkAAKeyRestrictions(ctx, doc, aaDict, "catalog")...)
	}

	// Walk each action in the AA dict for forbidden types.
	for key, actionObj := range aaDict {
		if ctx.Err() != nil {
			return results
		}
		actionDict, err := document.ResolveDict(ctx, doc, actionObj)
		if err != nil || actionDict == nil {
			continue
		}
		slog.Debug("compliance_2/actions: checking catalog /AA action", "key", key)
		results = append(results, walkActionChain(ctx, doc, actionDict, profile, part, fmt.Sprintf("catalog /AA/%s", key), 0)...)
	}

	return results
}

// checkPageActions checks /AA dicts on all pages.
func checkPageActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Result {
	pages := collectPages(ctx, doc, catalog)

	var results []Result
	for i, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		aaObj, ok := page["AA"]
		if !ok {
			continue
		}

		// Parts 2/3: page /AA is entirely forbidden (§6.5.2 t02).
		if part == 2 {
			slog.Debug("compliance_2/actions: page /AA forbidden for part 2", "page", i+1)
			results = append(results, failf(Rule_19005_2_6_5_2_t02, "page %d", i+1)...)
			continue
		}
		if part == 3 {
			slog.Debug("compliance_2/actions: page /AA forbidden for part 3", "page", i+1)
			results = append(results, failf(Rule_19005_3_6_5_2_t02, "page %d", i+1)...)
			continue
		}

		aaDict, err := document.ResolveDict(ctx, doc, aaObj)
		if err != nil || aaDict == nil {
			continue
		}

		slog.Debug("compliance_2/actions: checking page /AA", "page", i+1)

		// Part 4: check AA key restrictions (§6.6.3).
		if part == 4 {
			results = append(results, checkAAKeyRestrictions(ctx, doc, aaDict, fmt.Sprintf("page %d", i+1))...)
		}

		// Walk each action in the page AA dict for forbidden types.
		for key, actionObj := range aaDict {
			if ctx.Err() != nil {
				return results
			}
			actionDict, err := document.ResolveDict(ctx, doc, actionObj)
			if err != nil || actionDict == nil {
				continue
			}
			results = append(results, walkActionChain(ctx, doc, actionDict, profile, part, fmt.Sprintf("page %d /AA/%s", i+1, key), 0)...)
		}
	}

	return results
}

// checkAnnotationActions checks /A and /AA entries on all annotations across
// all pages.
func checkAnnotationActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Result {
	pages := collectPages(ctx, doc, catalog)

	var results []Result
	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}
		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			slog.Debug("compliance_2/actions: failed to resolve /Annots array", "page", pageIdx+1, "error", err)
			continue
		}

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			annotDict, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annotDict == nil {
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annotDict["Subtype"])
			isWidget := subtype == "Widget"
			location := fmt.Sprintf("page %d annot %d (/%s)", pageIdx+1, annotIdx, subtype)

			// --- Widget annotation /A check ---
			if isWidget {
				results = append(results, checkWidgetActions(ctx, doc, annotDict, profile, part, location)...)
			}

			// --- Non-widget annotation /A ---
			if !isWidget {
				if aObj, hasA := annotDict["A"]; hasA {
					aDict, err := document.ResolveDict(ctx, doc, aObj)
					if err == nil && aDict != nil {
						slog.Debug("compliance_2/actions: checking annotation /A", "location", location)
						results = append(results, walkActionChain(ctx, doc, aDict, profile, part, location+" /A", 0)...)
					}
				}
			}

			// --- Non-widget annotation /AA ---
			if !isWidget {
				if aaObj, hasAA := annotDict["AA"]; hasAA {
					aaDict, err := document.ResolveDict(ctx, doc, aaObj)
					if err != nil || aaDict == nil {
						continue
					}

					// Part 4: check AA key restrictions (§6.6.3).
					if part == 4 {
						results = append(results, checkAAKeyRestrictions(ctx, doc, aaDict, location)...)
					}

					for key, actionObj := range aaDict {
						if ctx.Err() != nil {
							return results
						}
						actionDict, err := document.ResolveDict(ctx, doc, actionObj)
						if err != nil || actionDict == nil {
							continue
						}
						slog.Debug("compliance_2/actions: checking annotation /AA action",
							"location", location, "key", key)
						results = append(results, walkActionChain(ctx, doc, actionDict, profile, part, fmt.Sprintf("%s /AA/%s", location, key), 0)...)
					}
				}
			}
		}
	}

	return results
}

// checkWidgetActions validates action restrictions specific to Widget
// annotations.
func checkWidgetActions(ctx context.Context, doc *document.Document, annotDict ast.PdfDict, profile string, part int, location string) []Result {
	var results []Result

	_, hasA := annotDict["A"]
	_, hasAA := annotDict["AA"]

	switch part {
	case 1:
		// §6.6.1 test 3: Widget annotations must not perform actions (/A).
		if hasA {
			slog.Debug("compliance_2/actions: widget has /A in part 1", "location", location)
			results = append(results, fail(Rule_19005_1_6_6_1_t03, location)...)
		}
		// §6.6.2 test 1: Widget must not have /AA.
		if hasAA {
			slog.Debug("compliance_2/actions: widget has /AA in part 1", "location", location)
			results = append(results, fail(Rule_19005_1_6_6_2_t01, location)...)
		}
	case 2:
		// §6.4.1 test 1: Widget must not contain /A or /AA.
		if hasA || hasAA {
			keys := actionKeysPresent(hasA, hasAA)
			slog.Debug("compliance_2/actions: widget has forbidden keys in part 2",
				"location", location, "keys", keys)
			results = append(results, failf(Rule_19005_2_6_4_1_t01, "%s", keys)...)
		}
	case 3:
		// §6.4.1 test 1: Widget must not contain /A or /AA.
		if hasA || hasAA {
			keys := actionKeysPresent(hasA, hasAA)
			slog.Debug("compliance_2/actions: widget has forbidden keys in part 3",
				"location", location, "keys", keys)
			results = append(results, failf(Rule_19005_3_6_4_1_t01, "%s", keys)...)
		}
	case 4:
		// §6.4.1 test 1: Widget must not contain /A (AA is allowed in part 4).
		if hasA {
			slog.Debug("compliance_2/actions: widget has /A in part 4", "location", location)
			results = append(results, fail(Rule_19005_4_6_4_1_t01, location)...)
		}
	}

	// Walk widget /A action chain for forbidden types (when /A is present and
	// not already flagged as structurally forbidden — we still report the type
	// violations for completeness).
	if hasA {
		aDict, err := document.ResolveDict(ctx, doc, annotDict["A"])
		if err == nil && aDict != nil {
			results = append(results, walkActionChain(ctx, doc, aDict, profile, part, location+" /A", 0)...)
		}
	}

	return results
}

// walkActionChain recursively checks an action dict and its /Next chain for
// forbidden action types and invalid named actions.
func walkActionChain(ctx context.Context, doc *document.Document, action ast.PdfDict, profile string, part int, location string, depth int) []Result {
	if ctx.Err() != nil || depth > maxActionChainDepth {
		if depth > maxActionChainDepth {
			slog.Debug("compliance_2/actions: max action chain depth reached", "location", location)
		}
		return nil
	}

	var results []Result

	// Check the action type (/S).
	actionType, _ := document.ResolveName(ctx, doc, action["S"])
	if actionType != "" {
		slog.Debug("compliance_2/actions: checking action type",
			"type", actionType, "location", location)

		forbidden := forbiddenActionTypes
		switch part {
		case 2, 3: //nolint:mnd // PDF/A-2 and PDF/A-3.
			forbidden = forbiddenActionTypesPart23
		case 4: //nolint:mnd // PDF/A-4.
			forbidden = forbiddenActionTypesPart4
		}

		if forbidden[actionType] {
			rule := forbiddenActionTypeRule(part)
			slog.Debug("compliance_2/actions: forbidden action type found",
				"type", actionType, "location", location)
			results = append(results, failf(rule, "%s at %s", actionType, location)...)
		}

		// Check named actions (§6.6.1 test 2).
		if actionType == "Named" {
			actionName, _ := document.ResolveName(ctx, doc, action["N"])
			if actionName != "" && !allowedNamedActions[actionName] {
				rule := namedActionRule(part)
				slog.Debug("compliance_2/actions: forbidden named action",
					"name", actionName, "location", location)
				results = append(results, failf(rule, "%s at %s", actionName, location)...)
			}
		}
	}

	// Walk /Next chain.
	nextObj, ok := action["Next"]
	if !ok {
		return results
	}

	// /Next can be a single action dict or an array of action dicts.
	nextDict, err := document.ResolveDict(ctx, doc, nextObj)
	if err == nil && nextDict != nil {
		results = append(results, walkActionChain(ctx, doc, nextDict, profile, part, location+" /Next", depth+1)...)
		return results
	}

	nextArr, err := document.ResolveArray(ctx, doc, nextObj)
	if err != nil {
		return results
	}
	for i, item := range nextArr {
		if ctx.Err() != nil {
			return results
		}
		itemDict, err := document.ResolveDict(ctx, doc, item)
		if err != nil || itemDict == nil {
			continue
		}
		results = append(results, walkActionChain(ctx, doc, itemDict, profile, part, fmt.Sprintf("%s /Next[%d]", location, i), depth+1)...)
	}

	return results
}

// checkAAKeyRestrictions checks that an /AA dictionary only contains permitted
// keys (§6.6.3, part 4).
func checkAAKeyRestrictions(_ context.Context, _ *document.Document, aaDict ast.PdfDict, location string) []Result {
	var forbidden []ast.PdfName
	for key := range aaDict {
		if !allowedAAKeys[key] {
			forbidden = append(forbidden, key)
		}
	}
	if len(forbidden) == 0 {
		return nil
	}

	slog.Debug("compliance_2/actions: forbidden AA keys found",
		"location", location, "keys", forbidden)
	return failf(Rule_19005_4_6_6_3_t01, "%v at %s", forbidden, location)
}

// forbiddenActionTypeRule returns the appropriate Rule for forbidden action
// types based on the PDF/A part.
func forbiddenActionTypeRule(part int) Rule {
	switch part {
	case 4:
		return Rule_19005_4_6_6_1_t01
	default:
		// Parts 1, 2, 3 share the same rule.
		return Rule_19005_1_6_6_1_t01
	}
}

// namedActionRule returns the appropriate Rule for forbidden named actions
// based on the PDF/A part.
func namedActionRule(part int) Rule {
	switch part {
	case 4:
		return Rule_19005_4_6_6_1_t02
	default:
		// Parts 1, 2, 3 share the same rule.
		return Rule_19005_1_6_6_1_t02
	}
}

// checkJavaScriptNameTree checks for JavaScript actions in the /Names/JavaScript
// name tree, which is forbidden by §6.6.1.
func checkJavaScriptNameTree(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Result {
	namesObj, ok := catalog["Names"]
	if !ok {
		return nil
	}

	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil || namesDict == nil {
		return nil
	}

	jsObj, ok := namesDict["JavaScript"]
	if !ok {
		return nil
	}

	// The presence of a /JavaScript name tree means JavaScript actions exist.
	slog.Debug("compliance_2/actions: /Names/JavaScript tree found (forbidden)")
	rule := forbiddenActionTypeRule(part)
	_ = jsObj // value unused, presence is sufficient
	return failf(rule, "JavaScript at catalog /Names/JavaScript")
}

// actionKeysPresent returns a human-readable string listing which of /A and
// /AA keys are present.
func actionKeysPresent(hasA, hasAA bool) string {
	switch {
	case hasA && hasAA:
		return "A, AA"
	case hasA:
		return "A"
	default:
		return "AA"
	}
}
