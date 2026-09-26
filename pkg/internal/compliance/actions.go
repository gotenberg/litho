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

// IsForbiddenActionType reports whether a PDF/A profile forbids an action whose
// /S is actionType: a type outside the whitelist of ISO 19005-1 6.6.1 test 1,
// ISO 19005-2/3 6.5.1 test 1 or ISO 19005-4 6.6.1 test 1 for the profile's
// part and conformance. A profile that is not PDF/A states no action-type
// restriction, so it forbids none.
//
// Exported so the remediation fixer strips exactly the action types the
// validator flags, as [AllowedNamedActions] does for named actions.
func IsForbiddenActionType(actionType ast.PdfName, profile string) bool {
	return PDFAPart(profile) != 0 && isForbiddenAction(actionType, profile)
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

// maxActionChainDepth bounds how deep [walkActionChain] follows the /Next tree
// of one action (ISO 32000-1, 12.6.2, Table 193), which it walks recursively.
// Anti-DoS guard, not spec-mandated: the spec puts no length on a chain, and a
// crafted one of direct dictionaries nested thousands deep would otherwise grow
// the goroutine stack with it. Fan-out needs no cap of its own: the run judges
// each action object once ([objectJudgedOnce]), so a /Next array that repeats
// an action, or a chain that loops back on itself, adds no work, and the direct
// dictionaries a chain can hold are bounded by the file's size. Action chains
// in real documents are a few actions long, so 200 leaves ample headroom.
//
// The outline and form-field walks reuse the value as their own depth bound,
// and remediation's action-chain splice descends as deep (its
// maxActionChainDepth), so it cuts every action this walk judges.
//
// # Corpus evidence
//
//   - All corpora: 5 files hold an action with /Next.
//   - pdfjs bug1737260.pdf: holds the longest chain, 6 actions.
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

	// One action object is judged once, however many parents or /Next arrays
	// reach it; see objectJudgedOnce.
	judged := make(map[int]bool)

	// Check catalog /OpenAction.
	results = append(results, checkCatalogOpenAction(ctx, doc, catalog, profile, part, judged)...)

	// Check catalog /AA.
	results = append(results, checkCatalogAA(ctx, doc, catalog, profile, part, judged)...)

	// Check page /AA dicts.
	results = append(results, checkPageActions(ctx, doc, catalog, profile, part, judged)...)

	// Check annotation /A and /AA.
	results = append(results, checkAnnotationActions(ctx, doc, catalog, profile, part, judged)...)

	// Check the actions of the /Names/JavaScript name tree.
	results = append(results, checkJavaScriptNameTree(ctx, doc, catalog, profile, part, judged)...)

	// Check outline (bookmark) actions.
	results = append(results, checkOutlineActions(ctx, doc, catalog, profile, part, judged)...)

	// Check AcroForm field actions.
	results = append(results, checkFormFieldActions(ctx, doc, catalog, profile, part, judged)...)

	return results
}

// checkCatalogOpenAction checks the catalog /OpenAction entry for forbidden
// action types and invalid named actions.
func checkCatalogOpenAction(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, judged map[int]bool) []Violation {
	openAction, ok := catalog["OpenAction"]
	if !ok {
		return nil
	}

	// /OpenAction may be an array (destination) or a dict (action); judgeAction
	// judges only a dictionary.
	return judgeAction(ctx, doc, openAction, profile, part, "catalog /OpenAction", judged)
}

// checkCatalogAA checks the catalog /AA dictionary.
//
// # Corpus evidence
//
//   - verapdf 6-5-2-t01-fail-b.pdf: its catalog /AA/WS holds a JavaScript action,
//     and veraPDF reports both §6.6.1 t1 for the action and §6.6.2 t3 for the
//     dictionary.
func checkCatalogAA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, judged map[int]bool) []Violation {
	// veraPDF's PDDocument containsAA is a knownKey test: an /AA written as null
	// is absent (ISO 32000-1, 7.3.7), one naming any object is present. Measured.
	if !knownKey(catalog, "AA") {
		return nil
	}

	var results []Violation

	// The catalog /AA dictionary is forbidden outright: ISO 19005-1 §6.6.2 t3
	// and ISO 19005-2/3 §6.5.2 t1. Record that, but do not stop here, exactly as
	// checkPageActions does for a page /AA: the action-type restrictions apply
	// to every action in the file, so a forbidden dictionary's actions are still
	// judged for a disallowed /S. veraPDF reports both on a catalog /AA
	// holding a JavaScript action: §6.6.1 t1 for the action and §6.6.2 t3
	// for the dictionary.
	switch part {
	case 1:
		results = append(results, fail(Rule_19005_1_6_6_2_t03, "catalog")...)
	case 2:
		results = append(results, fail(Rule_19005_2_6_5_2_t01, "catalog")...)
	case 3:
		results = append(results, fail(Rule_19005_3_6_5_2_t01, "catalog")...)
	}

	aaDict, err := document.ResolveDict(ctx, doc, catalog["AA"])
	if err != nil || aaDict == nil {
		doc.Logger.Debug("compliance.actions: catalog /AA is not a dict", "error", err)
		return results
	}

	// Part 4: check AA key restrictions (§6.6.3).
	if part == 4 {
		results = append(results, checkAAKeyRestrictions(ctx, doc, aaDict, "catalog")...)
	}

	// Walk the actions under the catalog's trigger events for forbidden types.
	results = append(results, walkAATriggers(ctx, doc, aaDict, catalogAATriggers, profile, part, "catalog", judged)...)

	return results
}

// checkPageActions checks /AA dicts on all pages.
func checkPageActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, judged map[int]bool) []Violation {
	pages := collectPages(ctx, doc, catalog)

	var results []Violation
	for i, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		// veraPDF's PDPage containsAA is a knownKey test: an /AA written as null is
		// absent (ISO 32000-1, 7.3.7), one naming any object is present. Measured.
		if !knownKey(page, "AA") {
			continue
		}
		aaObj := page["AA"]

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

		// Walk the actions under the page's trigger events for forbidden types.
		results = append(results, walkAATriggers(ctx, doc, aaDict, pageAATriggers, profile, part, fmt.Sprintf("page %d", i+1), judged)...)
	}

	return results
}

// The trigger events each additional-actions owner defines: an annotation's
// (ISO 32000-1, Table 194), a page's (Table 195), a form field's (Table 196)
// and the document catalog's (Table 197). veraPDF builds actions from these
// entries alone (PDAnnotationAdditionalActions, PDPageAdditionalActions,
// PDFormFieldActions, PDCatalogAdditionalActions), so an action filed under
// another owner's trigger, such as a keystroke /K on an annotation, is never
// judged.
//
//nolint:gochecknoglobals // package-level lookup tables.
var (
	annotAATriggers   = []ast.PdfName{"E", "X", "D", "U", "Fo", "Bl", "PO", "PC", "PV", "PI"}
	pageAATriggers    = []ast.PdfName{"O", "C"}
	fieldAATriggers   = []ast.PdfName{"K", "F", "V", "C"}
	catalogAATriggers = []ast.PdfName{"WC", "WS", "DS", "WP", "DP"}
)

// walkAATriggers walks, for forbidden action types, the actions aaDict holds
// under the trigger events in triggers, in sorted key order. An action object
// judged already under another parent is skipped; see [objectJudgedOnce].
func walkAATriggers(ctx context.Context, doc *document.Document, aaDict ast.PdfDict, triggers []ast.PdfName, profile string, part int, location string, judged map[int]bool) []Violation {
	var results []Violation
	for _, key := range sortedNames(aaDict) {
		if ctx.Err() != nil {
			return results
		}
		if !slices.Contains(triggers, key) {
			continue
		}
		results = append(results, judgeAction(ctx, doc, aaDict[key], profile, part, fmt.Sprintf("%s /AA/%s", location, key), judged)...)
	}
	return results
}

// judgeAction judges the action obj holds, and every action its /Next tree
// reaches, for a forbidden type, unless an action object was judged already
// under another parent. A value that is not a dictionary, such as a
// destination array under /OpenAction, holds no action.
func judgeAction(ctx context.Context, doc *document.Document, obj ast.PdfObject, profile string, part int, location string, judged map[int]bool) []Violation {
	if judged == nil {
		judged = make(map[int]bool)
	}
	if objectJudgedOnce(obj, judged) {
		return nil
	}
	action, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || action == nil {
		return nil
	}
	return walkActionChain(ctx, doc, action, profile, part, location, 0, judged)
}

// checkAnnotationActions checks /A and /AA entries on all annotations across
// all pages.
func checkAnnotationActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, judged map[int]bool) []Violation {
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
				results = append(results, checkWidgetActions(ctx, doc, annotDict, profile, part, location, judged)...)
			}

			if !isWidget {
				if aObj, hasA := annotDict["A"]; hasA {
					results = append(results, judgeAction(ctx, doc, aObj, profile, part, location+" /A", judged)...)
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

					results = append(results, walkAATriggers(ctx, doc, aaDict, annotAATriggers, profile, part, location, judged)...)
				}
			}
		}
	}

	return results
}

// checkWidgetActions validates action restrictions specific to Widget
// annotations.
func checkWidgetActions(ctx context.Context, doc *document.Document, annotDict ast.PdfDict, profile string, part int, location string, judged map[int]bool) []Violation {
	var results []Violation

	// veraPDF's containsA and containsAA are knownKey tests, so an /A or /AA
	// written as null is absent (ISO 32000-1, 7.3.7), measured.
	hasA := knownKey(annotDict, "A")
	hasAA := knownKey(annotDict, "AA")

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
		results = append(results, judgeAction(ctx, doc, annotDict["A"], profile, part, location+" /A", judged)...)
	}

	// Walk widget /AA action chains for forbidden types. In part 4, widget
	// /AA is structurally allowed but individual actions must still be
	// validated against the forbidden action type list.
	// Note: Widget annotations have additional allowed AA keys (K, V, F, C)
	// beyond the standard E, X, D, U, Fo, Bl. Do NOT apply the non-widget
	// AA key restriction check here. Those form-field triggers belong to the
	// field, though: veraPDF walks a widget's /AA under the annotation
	// triggers alone, and the field's under K, F, V and C (walkFormFields).
	if hasAA {
		aaDict, err := document.ResolveDict(ctx, doc, annotDict["AA"])
		if err == nil && aaDict != nil {
			results = append(results, walkAATriggers(ctx, doc, aaDict, annotAATriggers, profile, part, location, judged)...)
		}
	}

	return results
}

// objectJudgedOnce reports whether an indirect object has been judged already,
// marking it when it has not. It keeps one finding per object where a document
// lets several parents reach the same dictionary, which is how veraPDF counts:
// it builds one model object per PDF object and states each rule against it
// once. [checkExtGStateGraphics] uses it for the same reason.
//
// A terminal form field merged with its widget annotation is ONE dictionary
// (ISO 32000-1, 12.5.6.19), so litho reaches its /A and /AA twice: once walking
// the page's /Annots and once walking the AcroForm /Fields tree. veraPDF builds
// one PDAction per action object and reports it once.
//
// Only an indirect action can be shared, so a direct dictionary is always
// judged: it belongs to the one parent that spells it out.
//
// veraPDF keeps one set of the indirect objects it has visited for the whole
// validation run, so the rule holds across every action site: an action that
// an annotation's /A and another action's /Next both reach is judged once,
// and a /Next cycle ends when it comes back to an action already judged.
//
// # Corpus evidence
//
//   - pdfjs 160F-2019.pdf: drew 117 findings from litho against 54 from
//     veraPDF, split exactly 57 and 57 between the two paths over the same 42
//     merged dictionaries.
//   - pdfjs issue18030.pdf: four link annotations share one GoToE action, and
//     at PDF/A-1b litho reported it four times against veraPDF's once while
//     only widgets and form fields shared the set.
func objectJudgedOnce(obj ast.PdfObject, judged map[int]bool) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok || judged == nil {
		return false
	}
	if judged[ref.ObjectNum] {
		return true
	}
	judged[ref.ObjectNum] = true
	return false
}

// walkActionChain recursively checks an action dict and its /Next chain for
// forbidden action types and invalid named actions. ISO 32000-1, 12.6.2, Table
// 193 lets /Next hold one action dictionary or an array of them, performed
// after this one, so the actions form a tree and every action in it is an
// action the document performs: ISO 19005-1 6.6.1 and ISO 19005-2/3 6.5.1
// restrict each of them, and veraPDF links every /Next entry that is a
// dictionary as a PDAction of its own. judged records the object numbers of
// actions already walked in this run (see [objectJudgedOnce]): a crafted /Next
// cycle would otherwise recurse to the depth cap, and a /Next array that lists
// the same action repeatedly would fan out 2^depth (a denial of service).
//
// # Corpus evidence
//
//   - pdfjs issue20914.pdf: 503 outline items hold a GoTo action whose /Next
//     is a JavaScript action. veraPDF reports all 503 under ISO 19005-1 6.6.1
//     test 1 and ISO 19005-2 6.5.1 test 1, and litho reported none while the
//     outline walk judged the first action alone.
//   - All corpora: no action this walk reaches has a /S that is absent or no
//     name, so judging one as a type outside the whitelist moves no finding
//     at PDF/A-1b or PDF/A-2b, veraPDF files at their own profile.
func walkActionChain(ctx context.Context, doc *document.Document, action ast.PdfDict, profile string, part int, location string, depth int, judged map[int]bool) []Violation {
	if ctx.Err() != nil || depth > maxActionChainDepth {
		return nil
	}

	var results []Violation

	// Check the action type (/S). ISO 32000-1, Table 193 requires /S, a name,
	// and the test of ISO 19005-1 6.6.1 t1, ISO 19005-2/3 6.5.1 t1 and ISO
	// 19005-4 6.6.1 t1 permits the types its whitelist names, so an action whose
	// /S is absent, null, a string or the empty name is of no permitted type.
	// resolveActionType answers "" for all of them, which no whitelist holds.
	actionType := resolveActionType(ctx, doc, action)
	if isForbiddenAction(actionType, profile) {
		rule := forbiddenActionTypeRule(part)
		results = append(results, failf(rule, "%s at %s", actionTypeLabel(actionType), location)...)
	}

	// Check named actions (§6.6.1 test 2).
	if actionType == "Named" {
		actionName, _ := document.ResolveName(ctx, doc, action["N"])
		if actionName != "" && !AllowedNamedActions[actionName] {
			rule := namedActionRule(part)
			results = append(results, failf(rule, "%s at %s", actionName, location)...)
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
		if objectJudgedOnce(obj, judged) {
			return
		}
		d, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || d == nil {
			return
		}
		results = append(results, walkActionChain(ctx, doc, d, profile, part, loc, depth+1, judged)...)
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

// resolveActionType returns the action type the /S of action names, or "" when
// /S is absent or names no type: null, a string, any other object that is not
// a name, or the empty name. [walkActionChain] judges "" as a type outside every
// whitelist, as veraPDF does, so a /S that does not resolve is a verdict rather
// than a check left undone.
func resolveActionType(ctx context.Context, doc *document.Document, action ast.PdfDict) ast.PdfName {
	s, ok := action["S"]
	if !ok {
		return ""
	}
	actionType, err := document.ResolveName(ctx, doc, s)
	if err != nil {
		doc.Logger.Debug("compliance.actions: action /S is not a name", "error", err)
		return ""
	}
	return actionType
}

// actionTypeLabel names actionType in a finding. veraPDF names an action with
// no type "Unknown or not permitted Action type null".
func actionTypeLabel(actionType ast.PdfName) string {
	if actionType == "" {
		return "action with no /S type"
	}
	return actionType.String()
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

// checkJavaScriptNameTree judges the actions of the catalog's /Names
// /JavaScript tree against the profile's action-type whitelist. ISO 32000-1,
// 7.7.4, Table 31 makes that tree a map of document-level JavaScript actions
// (12.6.4.16), which a reader runs when it opens the document, so ISO 19005-1,
// 6.6.1 and ISO 19005-2/3, 6.5.1 forbid each of them as they forbid a
// JavaScript action anywhere else, while ISO 19005-4, 6.6.1 permits them. Each
// value is judged as an action, /Next tree included, so the finding names the
// action rather than the key: a tree that is empty, lists no value or is
// written as null (ISO 32000-1, 7.3.7) holds no action and draws none.
//
// veraPDF never judges these actions. Its validation model builds a PDAction
// only from the catalog's /OpenAction and /AA, pages, annotations, outline
// items and form fields (GFPDDocument), and only its feature reporter reads
// the tree (PDNamesDictionary.getJavaScript), so it passes a file whose one
// JavaScript action sits in this tree. That is a gap in veraPDF's reach, not
// a reading of the clause: the same action written as the /OpenAction fails
// the same rules in both tools. litho keeps the rule.
//
// # Corpus evidence
//
//   - All corpora: 83 files carry the tree. 80 hold JavaScript actions and no
//     other type, one each in 21, three in 55, four in 3 and 65 in pdfjs
//     issue20914.pdf; pdfjs bug1844576.pdf, doc_actions.pdf and js-authors.pdf
//     write it as an empty dictionary. Judging the actions rather than the key
//     moved no rule set at any PDF/A level, only the counts of those 62 files.
//   - verapdf PDF_UA-1__7.11-t01-*, 7.15-t01-fail-a, 7.18.7-t01-*,
//     PDF_UA-2__8.14.1-t01-fail-a and 8.9.2.4.10-t01-fail-a (11 files): their
//     only JavaScript is in the tree, and litho alone fails 6.5.1 t1 at
//     PDF/A-2a/2b/2u/3a/3b/3u and, on 7 of them, 6.6.1 t1 at PDF/A-1a/1b.
func checkJavaScriptNameTree(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, judged map[int]bool) []Violation {
	namesDict, err := document.ResolveDict(ctx, doc, catalog["Names"])
	if err != nil || namesDict == nil {
		return nil
	}
	tree, ok := namesDict["JavaScript"]
	if !ok {
		return nil
	}

	var results []Violation
	walkErr := document.WalkNameTree(ctx, doc, tree, func(key string, value ast.PdfObject) {
		location := fmt.Sprintf("catalog /Names/JavaScript %q", key)
		results = append(results, judgeAction(ctx, doc, value, profile, part, location, judged)...)
	})
	if walkErr != nil {
		doc.Logger.Debug("compliance.actions: /Names/JavaScript tree walk stopped", "error", walkErr)
	}
	return results
}

// checkOutlineActions walks the document outline (bookmarks) and checks
// /A entries on outline items for forbidden action types.
func checkOutlineActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, judged map[int]bool) []Violation {
	outlinesObj, ok := catalog["Outlines"]
	if !ok {
		return nil
	}

	outlines, err := document.ResolveDict(ctx, doc, outlinesObj)
	if err != nil || outlines == nil {
		return nil
	}

	var results []Violation
	walkOutlineItems(ctx, doc, outlines["First"], profile, part, &results, 0, make(map[int]bool), judged)
	return results
}

// walkOutlineItems checks one outline level for forbidden actions, walking
// siblings iteratively via /Next and recursing into children via /First.
// visited tracks resolved item object numbers across the whole walk: outline
// items form a linked list, and a crafted /Next or /First cycle would
// otherwise recurse forever. Stack exhaustion is not recoverable in Go, so
// the cycle must be broken here, not at the pkg/pdf recover boundary. judged
// is the run's set of action objects already judged ([objectJudgedOnce]).
func walkOutlineItems(ctx context.Context, doc *document.Document, itemObj ast.PdfObject, profile string, part int, results *[]Violation, depth int, visited, judged map[int]bool) {
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

		// The action-type whitelist of ISO 19005-1 6.6.1 and ISO 19005-2/3
		// 6.5.1 applies to every action, outline /A included: the spec grants
		// outlines no exemption, and the /Next tree is walked as for any other
		// action site. veraPDF links any dictionary under an outline item's /A
		// as a PDAction (PDOutlineItem.getAction), so the walk does not ask
		// for an /S first; walkActionChain judges only a present /S.
		if aObj, hasA := item["A"]; hasA {
			*results = append(*results, judgeAction(ctx, doc, aObj, profile, part, "outline item", judged)...)
		}

		// Recurse into children; siblings continue the loop.
		walkOutlineItems(ctx, doc, item["First"], profile, part, results, depth+1, visited, judged)

		itemObj = item["Next"]
	}
}

// checkFormFieldActions walks the AcroForm /Fields array and checks form field
// dicts for forbidden actions (/A and /AA keys).
func checkFormFieldActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string, part int, judged map[int]bool) []Violation {
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
	walkFormFields(ctx, doc, fields, profile, part, &results, 0, make(map[int]bool), judged, true)
	return results
}

// walkFormFields recursively checks form field dicts for forbidden actions. seen
// processes each field object once, so a shared or cyclic /Kids tree (a node
// whose children list the same object repeatedly) cannot force exponential work.
// listedByField says whether fields is the /Fields array or a form field's
// /Kids, the only lists veraPDF draws form fields from.
func walkFormFields(ctx context.Context, doc *document.Document, fields ast.PdfArray, profile string, part int, results *[]Violation, depth int, seen, judged map[int]bool, listedByField bool) {
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

		// §6.4.1 t2 judges veraPDF's form fields: every dictionary the /AcroForm
		// /Fields array lists, and below a field only the /Kids that carry a
		// partial field name /T (PDFormField.getChildFormFields keeps a kid when
		// isField, a knownKey on /T, holds). Any other kid, such as a widget that
		// carries /FT but no /T (ISO 32000-1, 12.7.3.1 makes /FT inheritable) or
		// one whose /T is null (7.3.7), is judged by t1 as an annotation only.
		// Measured on each, and on a top-level widget with neither /T nor /FT,
		// which is a field.
		isField := listedByField && (depth == 0 || knownKey(field, "T"))

		// §6.6.2 t2 (PDF/A-1) / §6.4.1 t2 (PDF/A-2/3): a form field dictionary
		// shall not contain an /AA additional-actions entry. The prohibition
		// is on the key itself, so a non-null /AA is the violation, independent
		// of the actions it holds. veraPDF's containsAA is a knownKey test, and
		// ISO 32000-1, 7.3.7 makes a null entry absent, measured.
		if isField && knownKey(field, "AA") {
			if rule := fieldAARule(part); rule != (Rule{}) {
				*results = append(*results, failf(rule, "%s contains forbidden /AA entry", loc)...)
			}
		}

		// Walk the actions under the field's trigger events (K, F, V, C) for
		// forbidden types. veraPDF links a form field to its /AA alone, never to
		// an /A, which it judges on the widget annotation instead, so a field's
		// /A is not walked here: walking it counted a merged field widget's
		// action twice, and judged a non-annotation field's action veraPDF never
		// reaches. Measured.
		if isField {
			if aaDict, err := document.ResolveDict(ctx, doc, field["AA"]); err == nil && aaDict != nil {
				*results = append(*results, walkAATriggers(ctx, doc, aaDict, fieldAATriggers, profile, part, loc, judged)...)
			}
		}

		// Recurse into /Kids.
		if kidsObj, hasKids := field["Kids"]; hasKids {
			kids, err := document.ResolveArray(ctx, doc, kidsObj)
			if err == nil {
				walkFormFields(ctx, doc, kids, profile, part, results, depth+1, seen, judged, isField)
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

// FieldAAForbidden reports whether profile forbids the /AA key on a form field
// whatever actions it holds, which is the verdict [fieldAARule] carries: ISO
// 19005-1 §6.6.2 t2 and ISO 19005-2/3 §6.4.1 t2. Remediation asks it so its
// removal follows the validator rather than a copy of the part list.
func FieldAAForbidden(profile string) bool {
	return fieldAARule(PDFAPart(profile)) != (Rule{})
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
