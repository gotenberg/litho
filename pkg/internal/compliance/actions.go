package compliance

import (
	"context"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// allowedActionsPart1 is the §6.6.1 t1 action-type whitelist for PDF/A-1: the
// action /S must be one of GoTo, GoToR, Thread, URI, Named or SubmitForm, so
// any other /S value (Rendition, GoToE, Launch, SetOCGState, ...) fails.
// Modelling this as a whitelist, not a blacklist, is required to flag types
// the spec never enumerated as forbidden but that are still not in the allowed
// set.
//
//nolint:gochecknoglobals // package-level lookup table mirroring PDFA-1B.xml.
var allowedActionsPart1 = map[ast.PdfName]bool{
	"GoTo":       true,
	"GoToR":      true,
	"Thread":     true,
	"URI":        true,
	"Named":      true,
	"SubmitForm": true,
}

// allowedActionsPart23 is the §6.5.1 t1 whitelist for PDF/A-2 and PDF/A-3: the
// part-1 set plus GoToE (embedded-document go-to).
//
//nolint:gochecknoglobals // package-level lookup table mirroring PDFA-2B.xml.
var allowedActionsPart23 = map[ast.PdfName]bool{
	"GoTo":       true,
	"GoToR":      true,
	"GoToE":      true,
	"Thread":     true,
	"URI":        true,
	"Named":      true,
	"SubmitForm": true,
}

// allowedActionsPart4 is the §6.6.1 t1 whitelist for PDF/A-4 and PDF/A-4f: the
// part-2/3 set plus JavaScript, RichMediaExecute, and GoToDp (ISO 19005-4
// relaxes the JavaScript prohibition).
//
//nolint:gochecknoglobals // package-level lookup table mirroring PDFA-4.xml.
var allowedActionsPart4 = map[ast.PdfName]bool{
	"GoTo":             true,
	"GoToR":            true,
	"GoToE":            true,
	"Thread":           true,
	"URI":              true,
	"Named":            true,
	"SubmitForm":       true,
	"JavaScript":       true,
	"RichMediaExecute": true,
	"GoToDp":           true,
}

// allowedActionsPart4e extends the PDF/A-4 set for PDF/A-4e with SetOCGState
// and GoTo3DView (3D/interactive content is permitted in the -e conformance).
//
//nolint:gochecknoglobals // package-level lookup table mirroring PDFA-4E.xml.
var allowedActionsPart4e = map[ast.PdfName]bool{
	"GoTo":             true,
	"GoToR":            true,
	"GoToE":            true,
	"Thread":           true,
	"URI":              true,
	"Named":            true,
	"SubmitForm":       true,
	"JavaScript":       true,
	"RichMediaExecute": true,
	"GoToDp":           true,
	"SetOCGState":      true,
	"GoTo3DView":       true,
}

// allowedActionTypes returns the permitted action-type set for a PDF/A profile.
func allowedActionTypes(profile string) map[ast.PdfName]bool {
	switch profile {
	case "PDF/A-4e":
		return allowedActionsPart4e
	case "PDF/A-4", "PDF/A-4f":
		return allowedActionsPart4
	}
	switch PDFAPart(profile) {
	case 1:
		return allowedActionsPart1
	case 4:
		return allowedActionsPart4
	default:
		return allowedActionsPart23
	}
}

// isForbiddenAction reports whether an action /S value is forbidden for the
// given PDF/A profile: any type outside the profile's whitelist.
func isForbiddenAction(actionType ast.PdfName, profile string) bool {
	return !allowedActionTypes(profile)[actionType]
}

// AllowedNamedActions lists the named actions permitted by §6.6.1. It is
// exported so the remediation fixer strips exactly the named actions the
// validator flags, leaving no room for the two sets to diverge.
//
//nolint:gochecknoglobals // package-level lookup table.
var AllowedNamedActions = map[ast.PdfName]bool{
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

	doc.Logger.Debug("compliance.actions: starting action checks", "profile", profile, "part", part)

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
	results = append(results, checkJavaScriptNameTree(ctx, doc, catalog, profile, part)...)

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

	return walkActionChain(ctx, doc, actionDict, profile, part, "catalog /OpenAction", 0, make(map[int]bool))
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
		doc.Logger.Debug("compliance.actions: catalog /AA is not a dict", "error", err)
		return nil
	}

	var results []Violation

	// Part 4: check AA key restrictions (§6.6.3).
	if part == 4 {
		results = append(results, checkAAKeyRestrictions(ctx, doc, aaDict, "catalog")...)
	}

	// Walk each action in the AA dict for forbidden types.
	for _, key := range sortedNames(aaDict) {
		actionObj := aaDict[key]
		if ctx.Err() != nil {
			return results
		}
		actionDict, err := document.ResolveDict(ctx, doc, actionObj)
		if err != nil || actionDict == nil {
			continue
		}
		results = append(results, walkActionChain(ctx, doc, actionDict, profile, part, fmt.Sprintf("catalog /AA/%s", key), 0, make(map[int]bool))...)
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

		// Parts 2/3: the page /AA dictionary itself is forbidden (§6.5.2 t02).
		// Record that, but do not stop here: per ISO 19005-2/3 §6.5.1, the
		// action-type restrictions apply to every action in the file, so the
		// actions inside a forbidden /AA are still checked for a disallowed /S
		// (§6.5.1 t01) or named action (§6.5.1 t02). veraPDF fires both 6.5.2
		// and 6.5.1 on such a page.
		if part == 2 {
			results = append(results, failf(Rule_19005_2_6_5_2_t02, "page %d", i+1)...)
		}
		if part == 3 {
			results = append(results, failf(Rule_19005_3_6_5_2_t02, "page %d", i+1)...)
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
		for _, key := range sortedNames(aaDict) {
			actionObj := aaDict[key]
			if ctx.Err() != nil {
				return results
			}
			actionDict, err := document.ResolveDict(ctx, doc, actionObj)
			if err != nil || actionDict == nil {
				continue
			}
			results = append(results, walkActionChain(ctx, doc, actionDict, profile, part, fmt.Sprintf("page %d /AA/%s", i+1, key), 0, make(map[int]bool))...)
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
			doc.Logger.Debug("compliance.actions: failed to resolve /Annots array", "page", pageIdx+1, "error", err)
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

			if isWidget {
				results = append(results, checkWidgetActions(ctx, doc, annotDict, profile, part, location)...)
			}

			if !isWidget {
				if aObj, hasA := annotDict["A"]; hasA {
					aDict, err := document.ResolveDict(ctx, doc, aObj)
					if err == nil && aDict != nil {
						results = append(results, walkActionChain(ctx, doc, aDict, profile, part, location+" /A", 0, make(map[int]bool))...)
					}
				}
			}

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

					for _, key := range sortedNames(aaDict) {
						actionObj := aaDict[key]
						if ctx.Err() != nil {
							return results
						}
						actionDict, err := document.ResolveDict(ctx, doc, actionObj)
						if err != nil || actionDict == nil {
							continue
						}
						results = append(results, walkActionChain(ctx, doc, actionDict, profile, part, fmt.Sprintf("%s /AA/%s", location, key), 0, make(map[int]bool))...)
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
	// not already flagged as structurally forbidden). Type violations still
	// fire for completeness.
	if hasA {
		aDict, err := document.ResolveDict(ctx, doc, annotDict["A"])
		if err == nil && aDict != nil {
			results = append(results, walkActionChain(ctx, doc, aDict, profile, part, location+" /A", 0, make(map[int]bool))...)
		}
	}

	// Walk widget /AA action chains for forbidden types. In part 4, widget
	// /AA is structurally allowed but individual actions must still be
	// validated against the forbidden action type list.
	// Note: Widget annotations have additional allowed AA keys (K, V, F, C)
	// beyond the standard E, X, D, U, Fo, Bl. Do NOT apply the non-widget
	// AA key restriction check here.
	if hasAA {
		aaDict, err := document.ResolveDict(ctx, doc, annotDict["AA"])
		if err == nil && aaDict != nil {
			for _, key := range sortedNames(aaDict) {
				actionObj := aaDict[key]
				if ctx.Err() != nil {
					return results
				}
				actionDict, err := document.ResolveDict(ctx, doc, actionObj)
				if err != nil || actionDict == nil {
					continue
				}
				results = append(results, walkActionChain(ctx, doc, actionDict, profile, part, fmt.Sprintf("%s /AA/%s", location, key), 0, make(map[int]bool))...)
			}
		}
	}

	return results
}

// walkActionChain recursively checks an action dict and its /Next chain for
// forbidden action types and invalid named actions. visited records the object
// numbers of actions already walked: a crafted /Next cycle would otherwise
// recurse to the depth cap on every entry point, and a /Next array that lists
// the same action repeatedly would fan out 2^depth (a denial of service).
func walkActionChain(ctx context.Context, doc *document.Document, action ast.PdfDict, profile string, part int, location string, depth int, visited map[int]bool) []Violation {
	if ctx.Err() != nil || depth > maxActionChainDepth {
		return nil
	}

	var results []Violation

	// Check the action type (/S).
	actionType, _ := document.ResolveName(ctx, doc, action["S"])
	if actionType != "" {
		if isForbiddenAction(actionType, profile) {
			rule := forbiddenActionTypeRule(part)
			results = append(results, failf(rule, "%s at %s", actionType, location)...)
		}

		// Check named actions (§6.6.1 test 2).
		if actionType == "Named" {
			actionName, _ := document.ResolveName(ctx, doc, action["N"])
			if actionName != "" && !AllowedNamedActions[actionName] {
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

	// Recurse into one /Next action, skipping any action object already walked
	// so a cyclic or repeated /Next cannot loop forever or fan out.
	walkNext := func(obj ast.PdfObject, loc string) {
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if visited[ref.ObjectNum] {
				return
			}
			visited[ref.ObjectNum] = true
		}
		d, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || d == nil {
			return
		}
		results = append(results, walkActionChain(ctx, doc, d, profile, part, loc, depth+1, visited)...)
	}

	// /Next can be a single action dict or an array of action dicts.
	if nextArr, err := document.ResolveArray(ctx, doc, nextObj); err == nil {
		for i, item := range nextArr {
			if ctx.Err() != nil {
				return results
			}
			walkNext(item, fmt.Sprintf("%s /Next[%d]", location, i))
		}
	} else {
		walkNext(nextObj, location+" /Next")
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
	slices.Sort(forbidden) // deterministic message order

	return failf(Rule_19005_4_6_6_3_t01, "%v at %s", forbidden, location)
}

// forbiddenActionTypeRule returns the appropriate Rule for forbidden action
// types based on the PDF/A part.
func forbiddenActionTypeRule(part int) Rule {
	// PDF/A-1 §6.6.1 t1; PDF/A-2/3 §6.5.1 t1; PDF/A-4 §6.6.1 t1.
	return ruleByPart(part, Rule_19005_1_6_6_1_t01, Rule_19005_2_6_5_1_t01, Rule_19005_3_6_5_1_t01, Rule_19005_4_6_6_1_t01)
}

// namedActionRule returns the appropriate Rule for forbidden named actions
// based on the PDF/A part.
func namedActionRule(part int) Rule {
	// PDF/A-1 §6.6.1 t2; PDF/A-2/3 §6.5.1 t2; PDF/A-4 §6.6.1 t2.
	return ruleByPart(part, Rule_19005_1_6_6_1_t02, Rule_19005_2_6_5_1_t02, Rule_19005_3_6_5_1_t02, Rule_19005_4_6_6_1_t02)
}

// checkJavaScriptNameTree checks for JavaScript actions in the /Names/JavaScript
// name tree. JavaScript is forbidden by §6.6.1 for PDF/A-1 through -3; PDF/A-4
// permits it, so the check is gated on the profile's action whitelist.
func checkJavaScriptNameTree(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int) []Violation {
	if allowedActionTypes(profile)["JavaScript"] {
		return nil
	}

	namesObj, ok := catalog["Names"]
	if !ok {
		return nil
	}

	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil || namesDict == nil {
		return nil
	}

	_, ok = namesDict["JavaScript"]
	if !ok {
		return nil
	}

	// The presence of a /JavaScript name tree means JavaScript actions exist.
	rule := forbiddenActionTypeRule(part)
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
	walkOutlineItems(ctx, doc, outlines["First"], profile, part, &results, 0, make(map[int]bool))
	return results
}

// walkOutlineItems checks one outline level for forbidden actions, walking
// siblings iteratively via /Next and recursing into children via /First.
// visited tracks resolved item object numbers across the whole walk: outline
// items form a linked list, and a crafted /Next or /First cycle would
// otherwise recurse forever. Stack exhaustion is not recoverable in Go, so
// the cycle must be broken here, not at the pkg/pdf recover boundary.
func walkOutlineItems(ctx context.Context, doc *document.Document, itemObj ast.PdfObject, profile string, part int, results *[]Violation, depth int, visited map[int]bool) {
	if depth > maxActionChainDepth {
		return
	}
	for itemObj != nil {
		if ctx.Err() != nil {
			return
		}
		if ref, ok := itemObj.(ast.PdfIndirectRef); ok {
			if visited[ref.ObjectNum] {
				return
			}
			visited[ref.ObjectNum] = true
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
		// but outline /A is explicit user interaction, so it carries the
		// per-profile action-type whitelist via the outline-specific handling
		// below rather than the /AA key restrictions.
		if aObj, hasA := item["A"]; hasA {
			aDict, err := document.ResolveDict(ctx, doc, aObj)
			if err == nil && aDict != nil {
				// Verify it's actually an action (has /S key), not a destination.
				if _, hasS := aDict["S"]; hasS {
					if IsPDFUA(profile) {
						violations := walkActionChain(ctx, doc, aDict, profile, part, "outline item", 0, make(map[int]bool))
						*results = append(*results, violations...)
					} else {
						// PDF/A: check with outline-specific exemptions.
						violations := checkOutlineActionPDFA(ctx, doc, aDict, profile, part)
						*results = append(*results, violations...)
					}
				}
			}
		}

		// Recurse into children; siblings continue the loop.
		walkOutlineItems(ctx, doc, item["First"], profile, part, results, depth+1, visited)

		itemObj = item["Next"]
	}
}

// checkOutlineActionPDFA checks an outline item action for PDF/A against the
// per-profile action-type whitelist.
func checkOutlineActionPDFA(ctx context.Context, doc *document.Document, action ast.PdfDict, profile string, part int) []Violation {
	actionType, _ := document.ResolveName(ctx, doc, action["S"])
	if actionType == "" {
		return nil
	}

	var results []Violation

	// The §6.6.1/§6.5.1 t1 action-type whitelist applies to every PDAction
	// site, including outline /A actions: the spec grants no outline exemption.
	if isForbiddenAction(actionType, profile) {
		rule := forbiddenActionTypeRule(part)
		results = append(results, failf(rule, "%s at outline item", actionType)...)
	}

	// Also check named actions (§6.6.1 test 2 / §6.5.1 test 2).
	if actionType == "Named" {
		actionName, _ := document.ResolveName(ctx, doc, action["N"])
		if actionName != "" && !AllowedNamedActions[actionName] {
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
		doc.Logger.Debug("compliance.actions: cannot resolve form fields array", "error", err)
		return nil
	}

	var results []Violation
	walkFormFields(ctx, doc, fields, profile, part, &results, 0, make(map[int]bool))
	return results
}

// walkFormFields recursively checks form field dicts for forbidden actions. seen
// processes each field object once, so a shared or cyclic /Kids tree (a node
// whose children list the same object repeatedly) cannot force exponential work.
func walkFormFields(ctx context.Context, doc *document.Document, fields ast.PdfArray, profile string, part int, results *[]Violation, depth int, seen map[int]bool) {
	if depth > maxActionChainDepth {
		return
	}
	for i, fieldObj := range fields {
		if ctx.Err() != nil {
			return
		}
		if ref, ok := fieldObj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
		}
		field, err := document.ResolveDict(ctx, doc, fieldObj)
		if err != nil || field == nil {
			continue
		}
		loc := fmt.Sprintf("form field %d", i)

		// A /Kids entry that is a widget annotation without its own field
		// identity (no /FT and no /T) is a pure widget, not a form field:
		// ISO 32000-1 §12.7.3.1 lets a field's appearance be carried by child
		// widget annotations that share the field's name. veraPDF scopes §6.4.1
		// t2 to the PDFormField object, so such a widget's /AA is a t1
		// (PDWidgetAnnot) violation only. A terminal field merged with its
		// widget keeps /FT (or /T), so it stays a field here.
		isPureWidget := false
		if st, _ := document.ResolveName(ctx, doc, field["Subtype"]); st == "Widget" {
			_, hasFT := field["FT"]
			_, hasT := field["T"]
			isPureWidget = !hasFT && !hasT
		}

		// §6.6.2 t2 (PDF/A-1) / §6.4.1 t2 (PDF/A-2/3): a form field dictionary
		// shall not contain an /AA additional-actions entry. The prohibition
		// is on the key itself, so mere presence of /AA is the violation,
		// independent of the actions it holds.
		if _, hasAA := field["AA"]; hasAA && !isPureWidget {
			if rule := fieldAARule(part); rule != (Rule{}) {
				*results = append(*results, failf(rule, "%s contains forbidden /AA entry", loc)...)
			}
		}

		// Check /A on the field.
		if aObj, hasA := field["A"]; hasA {
			aDict, err := document.ResolveDict(ctx, doc, aObj)
			if err == nil && aDict != nil {
				if _, hasS := aDict["S"]; hasS {
					*results = append(*results, walkActionChain(ctx, doc, aDict, profile, part, loc, 0, make(map[int]bool))...)
				}
			}
		}

		// Check /AA on the field.
		if aaObj, hasAA := field["AA"]; hasAA {
			aaDict, err := document.ResolveDict(ctx, doc, aaObj)
			if err == nil && aaDict != nil {
				for _, key := range sortedNames(aaDict) {
					valObj := aaDict[key]
					actDict, err := document.ResolveDict(ctx, doc, valObj)
					if err != nil || actDict == nil {
						continue
					}
					if _, hasS := actDict["S"]; hasS {
						*results = append(*results, walkActionChain(ctx, doc, actDict, profile, part,
							fmt.Sprintf("%s /AA/%s", loc, key), 0, make(map[int]bool))...)
					}
				}
			}
		}

		// Recurse into /Kids.
		if kidsObj, hasKids := field["Kids"]; hasKids {
			kids, err := document.ResolveArray(ctx, doc, kidsObj)
			if err == nil {
				walkFormFields(ctx, doc, kids, profile, part, results, depth+1, seen)
			}
		}
	}
}

// fieldAARule selects the "form field shall not contain /AA" rule by PDF/A
// part: ISO 19005-1 §6.6.2 t2 and ISO 19005-2/3 §6.4.1 t2. It returns the zero
// Rule for parts outside 1-3, since PDF/A-4 permits a restricted /AA key set
// (§6.6.3) and PDF/UA has no equivalent prohibition.
func fieldAARule(part int) Rule {
	return ruleByPart(part, Rule_19005_1_6_6_2_t02, Rule_19005_2_6_4_1_t02, Rule_19005_3_6_4_1_t02, Rule{})
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
