package compliance

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// forbiddenActionsBase lists action /S values forbidden by §6.6.1 across all
// PDF/A parts.
//
//nolint:gochecknoglobals // package-level lookup table.
var forbiddenActionsBase = map[ast.PdfName]bool{
	"Launch":     true,
	"Sound":      true,
	"Movie":      true,
	"ResetForm":  true,
	"ImportData": true,
	"JavaScript": true,
	"Hide":       true,
	"SetState":   true,
	"NoOp":       true,
	"NOP":        true,
}

// forbiddenActionsPart2Plus lists additional action types forbidden for PDF/A-2
// and later (§6.6.1). These extend the base set with SetOCGState, Rendition,
// Trans, and GoTo3DView.
//
//nolint:gochecknoglobals // package-level lookup table.
var forbiddenActionsPart2Plus = map[ast.PdfName]bool{
	"SetOCGState": true,
	"Rendition":   true,
	"Trans":       true,
	"GoTo3DView":  true,
}

// isForbiddenAction reports whether the given action type is forbidden for the
// specified PDF/A part. Part 1 forbids the base set; parts 2+ additionally
// forbid SetOCGState, Rendition, Trans, and GoTo3DView.
func isForbiddenAction(actionType ast.PdfName, part int) bool {
	if forbiddenActionsBase[actionType] {
		return true
	}
	if part >= 2 && forbiddenActionsPart2Plus[actionType] {
		return true
	}
	return false
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
// infinite loops from circular references. Action chains can be long in
// complex interactive documents, so 200 provides headroom.
const maxActionChainDepth = 200

// checkActionGroup validates action restrictions for PDF/A (§6.6.1, §6.6.2,
// §6.6.3, §6.4.1). For PDF/UA profiles this check is not applicable.
func checkActionGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part

	if vctx.ua {
		return nil
	}

	if part == 0 {
		return nil
	}

	complog(doc).Debug("compliance.actions: starting action checks", "profile", profile, "part", part)

	results := make([]Violation, 0, 8)

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

	// Check outline (bookmark) actions.
	results = append(results, checkOutlineActions(ctx, doc, catalog, profile, part)...)

	// Check AcroForm field actions.
	results = append(results, checkFormFieldActions(ctx, doc, catalog, profile, part)...)

	return results
}

// checkCatalogOpenAction checks the catalog /OpenAction entry for forbidden
// action types and invalid named actions.
func checkCatalogOpenAction(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Violation {
	openAction, ok := catalog["OpenAction"]
	if !ok {
		return nil
	}

	// /OpenAction may be an array (destination) or a dict (action).
	actionDict, err := document.ResolveDict(ctx, doc, openAction)
	if err != nil || actionDict == nil {
		return nil
	}

	return walkActionChain(ctx, doc, actionDict, profile, part, "catalog /OpenAction", 0)
}

// checkCatalogAA checks the catalog /AA dictionary.
func checkCatalogAA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Violation {
	aaObj, ok := catalog["AA"]
	if !ok {
		return nil
	}

	// Part 1: catalog /AA is entirely forbidden (§6.6.2).
	if part == 1 {
		return fail(Rule_19005_1_6_6_2_t03, "catalog")
	}

	// Parts 2/3: catalog /AA is also forbidden (§6.5.2 t01).
	if part == 2 {
		return fail(Rule_19005_2_6_5_2_t01, "catalog")
	}
	if part == 3 {
		return fail(Rule_19005_3_6_5_2_t01, "catalog")
	}

	aaDict, err := document.ResolveDict(ctx, doc, aaObj)
	if err != nil || aaDict == nil {
		complog(doc).Debug("compliance.actions: catalog /AA is not a dict", "error", err)
		return nil
	}

	var results []Violation

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
		results = append(results, walkActionChain(ctx, doc, actionDict, profile, part, fmt.Sprintf("catalog /AA/%s", key), 0)...)
	}

	return results
}

// checkPageActions checks /AA dicts on all pages.
func checkPageActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Violation {
	pages := collectPages(ctx, doc, catalog)

	var results []Violation
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
			results = append(results, failf(Rule_19005_2_6_5_2_t02, "page %d", i+1)...)
			continue
		}
		if part == 3 {
			results = append(results, failf(Rule_19005_3_6_5_2_t02, "page %d", i+1)...)
			continue
		}

		aaDict, err := document.ResolveDict(ctx, doc, aaObj)
		if err != nil || aaDict == nil {
			continue
		}

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
func checkAnnotationActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Violation {
	pages := collectPages(ctx, doc, catalog)

	var results []Violation
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
			complog(doc).Debug("compliance.actions: failed to resolve /Annots array", "page", pageIdx+1, "error", err)
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
func checkWidgetActions(ctx context.Context, doc *document.Document, annotDict ast.PdfDict, profile string, part int, location string) []Violation {
	var results []Violation

	_, hasA := annotDict["A"]
	_, hasAA := annotDict["AA"]

	switch part {
	case 1:
		// §6.6.1 test 3: Widget annotations must not perform actions (/A).
		if hasA {
			results = append(results, fail(Rule_19005_1_6_6_1_t03, location)...)
		}
		// §6.6.2 test 1: Widget must not have /AA.
		if hasAA {
			results = append(results, fail(Rule_19005_1_6_6_2_t01, location)...)
		}
	case 2:
		// §6.4.1 test 1: Widget must not contain /A or /AA.
		if hasA || hasAA {
			keys := actionKeysPresent(hasA, hasAA)
			results = append(results, failf(Rule_19005_2_6_4_1_t01, "%s", keys)...)
		}
	case 3:
		// §6.4.1 test 1: Widget must not contain /A or /AA.
		if hasA || hasAA {
			keys := actionKeysPresent(hasA, hasAA)
			results = append(results, failf(Rule_19005_3_6_4_1_t01, "%s", keys)...)
		}
	case 4:
		// §6.4.1 test 1: Widget must not contain /A (AA is allowed in part 4).
		if hasA {
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

	// Walk widget /AA action chains for forbidden types. In part 4, widget
	// /AA is structurally allowed but individual actions must still be
	// validated against the forbidden action type list.
	// Note: Widget annotations have additional allowed AA keys (K, V, F, C)
	// beyond the standard E, X, D, U, Fo, Bl — do NOT apply the non-widget
	// AA key restriction check here.
	if hasAA {
		aaDict, err := document.ResolveDict(ctx, doc, annotDict["AA"])
		if err == nil && aaDict != nil {
			for key, actionObj := range aaDict {
				if ctx.Err() != nil {
					return results
				}
				actionDict, err := document.ResolveDict(ctx, doc, actionObj)
				if err != nil || actionDict == nil {
					continue
				}
				results = append(results, walkActionChain(ctx, doc, actionDict, profile, part, fmt.Sprintf("%s /AA/%s", location, key), 0)...)
			}
		}
	}

	return results
}

// walkActionChain recursively checks an action dict and its /Next chain for
// forbidden action types and invalid named actions.
func walkActionChain(ctx context.Context, doc *document.Document, action ast.PdfDict, profile string, part int, location string, depth int) []Violation {
	if ctx.Err() != nil || depth > maxActionChainDepth {
		return nil
	}

	var results []Violation

	// Check the action type (/S).
	actionType, _ := document.ResolveName(ctx, doc, action["S"])
	if actionType != "" {
		if isForbiddenAction(actionType, part) {
			rule := forbiddenActionTypeRule(part)
			results = append(results, failf(rule, "%s at %s", actionType, location)...)
		}

		// Check named actions (§6.6.1 test 2).
		if actionType == "Named" {
			actionName, _ := document.ResolveName(ctx, doc, action["N"])
			if actionName != "" && !allowedNamedActions[actionName] {
				rule := namedActionRule(part)
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
func checkAAKeyRestrictions(_ context.Context, _ *document.Document, aaDict ast.PdfDict, location string) []Violation {
	var forbidden []ast.PdfName
	for key := range aaDict {
		if !allowedAAKeys[key] {
			forbidden = append(forbidden, key)
		}
	}
	if len(forbidden) == 0 {
		return nil
	}

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
func checkJavaScriptNameTree(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Violation {
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
	rule := forbiddenActionTypeRule(part)
	_ = jsObj // value unused, presence is sufficient
	return failf(rule, "JavaScript at catalog /Names/JavaScript")
}

// checkOutlineActions walks the document outline (bookmarks) and checks
// /A entries on outline items for forbidden action types.
func checkOutlineActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Violation {
	outlinesObj, ok := catalog["Outlines"]
	if !ok {
		return nil
	}

	outlines, err := document.ResolveDict(ctx, doc, outlinesObj)
	if err != nil || outlines == nil {
		return nil
	}

	var results []Violation
	walkOutlineItems(ctx, doc, outlines["First"], profile, part, &results, 0)
	return results
}

// walkOutlineItems recursively walks outline items and checks for forbidden actions.
func walkOutlineItems(ctx context.Context, doc *document.Document, itemObj ast.PdfObject, profile string, part int, results *[]Violation, depth int) {
	if ctx.Err() != nil || depth > maxActionChainDepth || itemObj == nil {
		return
	}

	item, err := document.ResolveDict(ctx, doc, itemObj)
	if err != nil || item == nil {
		return
	}

	// Verify this looks like an outline item (has /Title or /Parent).
	_, hasTitle := item["Title"]
	_, hasParent := item["Parent"]
	if !hasTitle && !hasParent {
		return
	}

	// Check /A action on this outline item.
	// For PDF/A, outline items are user-initiated (not automatic triggers).
	// The spec restricts certain action types in /AA (additional actions)
	// but outline /A is explicit user interaction. VeraPDF does not flag
	// /JavaScript or /SetOCGState on outline items for PDF/A.
	if aObj, hasA := item["A"]; hasA {
		aDict, err := document.ResolveDict(ctx, doc, aObj)
		if err == nil && aDict != nil {
			// Verify it's actually an action (has /S key), not a destination.
			if _, hasS := aDict["S"]; hasS {
				if IsPDFUA(profile) {
					violations := walkActionChain(ctx, doc, aDict, profile, part, "outline item", 0)
					*results = append(*results, violations...)
				} else {
					// PDF/A: check with outline-specific exemptions.
					violations := checkOutlineActionPDFA(ctx, doc, aDict, profile, part)
					*results = append(*results, violations...)
				}
			}
		}
	}

	// Recurse into children.
	walkOutlineItems(ctx, doc, item["First"], profile, part, results, depth+1)

	// Recurse to next sibling — use depth (not depth+1) since siblings are at same level.
	walkOutlineItems(ctx, doc, item["Next"], profile, part, results, depth)
}

// outlineExemptActionsPart4 are action types that VeraPDF does not flag on
// outline items for PDF/A-4. Outline /A is user-initiated, not an automatic trigger.
//
//nolint:gochecknoglobals // package-level lookup table.
var outlineExemptActionsPart4 = map[ast.PdfName]bool{
	"JavaScript": true,
}

// outlineExemptActionsPart4e extends Part4 exemptions for PDF/A-4e, which
// additionally allows SetOCGState and GoTo3DView on outline items.
//
//nolint:gochecknoglobals // package-level lookup table.
var outlineExemptActionsPart4e = map[ast.PdfName]bool{
	"JavaScript":  true,
	"SetOCGState": true,
	"GoTo3DView":  true,
}

// checkOutlineActionPDFA checks an outline item action for PDF/A, using the
// standard forbidden set minus outline-exempt types.
func checkOutlineActionPDFA(ctx context.Context, doc *document.Document, action ast.PdfDict, profile string, part int) []Violation {
	actionType, _ := document.ResolveName(ctx, doc, action["S"])
	if actionType == "" {
		return nil
	}

	var results []Violation

	// Determine outline-exempt actions based on profile.
	var exempt map[ast.PdfName]bool
	switch profile {
	case "PDF/A-4e":
		exempt = outlineExemptActionsPart4e
	case "PDF/A-4", "PDF/A-4f":
		exempt = outlineExemptActionsPart4
	}

	// Check forbidden action types, skipping outline-exempt types.
	if !exempt[actionType] && isForbiddenAction(actionType, part) {
		rule := forbiddenActionTypeRule(part)
		results = append(results, failf(rule, "%s at outline item", actionType)...)
	}

	// Also check named actions (§6.6.1 test 2 / §6.5.1 test 2).
	if actionType == "Named" {
		actionName, _ := document.ResolveName(ctx, doc, action["N"])
		if actionName != "" && !allowedNamedActions[actionName] {
			rule := namedActionRule(part)
			results = append(results, failf(rule, "%s at outline item", actionName)...)
		}
	}

	return results
}

// checkFormFieldActions walks the AcroForm /Fields array and checks form field
// dicts for forbidden actions (/A and /AA keys).
func checkFormFieldActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Violation {
	acroFormObj, ok := catalog["AcroForm"]
	if !ok {
		return nil
	}
	acroForm, err := document.ResolveDict(ctx, doc, acroFormObj)
	if err != nil || acroForm == nil {
		return nil
	}
	fieldsObj, ok := acroForm["Fields"]
	if !ok {
		return nil
	}
	fields, err := document.ResolveArray(ctx, doc, fieldsObj)
	if err != nil {
		complog(doc).Debug("compliance.actions: cannot resolve form fields array", "error", err)
		return nil
	}

	var results []Violation
	walkFormFields(ctx, doc, fields, profile, part, &results, 0)
	return results
}

// walkFormFields recursively checks form field dicts for forbidden actions.
func walkFormFields(ctx context.Context, doc *document.Document, fields ast.PdfArray, profile string, part int, results *[]Violation, depth int) {
	if depth > maxActionChainDepth {
		return
	}
	for i, fieldObj := range fields {
		if ctx.Err() != nil {
			return
		}
		field, err := document.ResolveDict(ctx, doc, fieldObj)
		if err != nil || field == nil {
			continue
		}
		loc := fmt.Sprintf("form field %d", i)

		// Check /A on the field.
		if aObj, hasA := field["A"]; hasA {
			aDict, err := document.ResolveDict(ctx, doc, aObj)
			if err == nil && aDict != nil {
				if _, hasS := aDict["S"]; hasS {
					*results = append(*results, walkActionChain(ctx, doc, aDict, profile, part, loc, 0)...)
				}
			}
		}

		// Check /AA on the field.
		if aaObj, hasAA := field["AA"]; hasAA {
			aaDict, err := document.ResolveDict(ctx, doc, aaObj)
			if err == nil && aaDict != nil {
				for key, valObj := range aaDict {
					actDict, err := document.ResolveDict(ctx, doc, valObj)
					if err != nil || actDict == nil {
						continue
					}
					if _, hasS := actDict["S"]; hasS {
						*results = append(*results, walkActionChain(ctx, doc, actDict, profile, part,
							fmt.Sprintf("%s /AA/%s", loc, key), 0)...)
					}
				}
			}
		}

		// Recurse into /Kids.
		if kidsObj, hasKids := field["Kids"]; hasKids {
			kids, err := document.ResolveArray(ctx, doc, kidsObj)
			if err == nil {
				walkFormFields(ctx, doc, kids, profile, part, results, depth+1)
			}
		}
	}
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
