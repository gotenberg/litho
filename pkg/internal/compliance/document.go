package compliance

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkDocumentGroup validates document-level properties for PDF/A and PDF/UA:
//   - PDF/A-1: PDF version must be 1.4 or lower; /OCProperties forbidden.
//   - PDF/A-4: PDF version must be 2.0.
//   - PDF/A parts 2+/4: /AlternatePresentations forbidden in /Names,
//     /Requirements key forbidden in catalog.
//   - PDF/A parts 2+/4: /OCProperties must have /AS array if /OCGs present;
//     OC config dicts require unique /Name and complete /Order.
//   - PDF/A-4: /Info dict restrictions, /Perms dict restrictions.
//   - PDF/UA-2: PDF version must be 2.0.
func checkDocumentGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part

	doc.Logger.Debug("compliance.document: starting document checks",
		"profile", profile, "part", part, "isPDFUA", vctx.ua)

	var results []Violation

	if vctx.ua {
		if profile == "PDF/UA-2" {
			results = append(results, checkVersionIs2(doc, catalog, Rule_14289_2_6_2_t01)...)
		}
		doc.Logger.Debug("compliance.document: PDF/UA document checks complete", "violations", len(results))
		return results
	}

	if part == 0 {
		return nil
	}

	if part == 1 {
		results = append(results, checkVersionPart1(doc, catalog)...)
		results = append(results, checkForbiddenOCProperties(catalog)...)
	}

	if part == 4 {
		results = append(results, checkVersionPart4(doc, catalog)...)
	}

	if part >= 2 {
		results = append(results, checkAlternatePresentations(ctx, doc, catalog, part)...)
		results = append(results, checkRequirementsKey(catalog, part)...)
		results = append(results, checkNeedsRenderingKey(catalog, part)...)
	}

	if part >= 2 {
		results = append(results, checkOCGUsageApplication(ctx, doc, catalog, part)...)
	}

	if part >= 2 {
		results = append(results, checkOCConfigNamesPDFA(ctx, doc, catalog, part)...)
		results = append(results, checkOCGOrderCompleteness(ctx, doc, catalog, part)...)
	}

	if part == 4 {
		results = append(results, checkInfoDictPart4(ctx, doc, catalog)...)
	}

	if part >= 2 {
		results = append(results, checkPermissionsDict(ctx, doc, catalog, part)...)
	}

	// §6.10 t2 (parts 2/3) / §6.11 t2 (part 4): PresSteps forbidden.
	if part >= 2 {
		results = append(results, checkPresSteps(ctx, doc, catalog, part)...)
	}

	// §6.9 t1: embedded file stream /Subtype MIME type.
	if part == 4 {
		results = append(results, checkEmbeddedFileMIME(ctx, doc, catalog)...)
	}

	doc.Logger.Debug("compliance.document: document checks complete", "violations", len(results))
	return results
}

// checkVersionPart1 validates that the PDF header format is correct for PDF/A-1.
// Per §6.1.2, the rule checks that the header is `%PDF-x.y` format, NOT the
// version number value. VeraPDF does not enforce a maximum version here.
func checkVersionPart1(_ *document.Document, _ ast.PdfDict) []Violation {
	// The header format is validated at parse time by the lexer.
	// §6.1.2 does not restrict the version number for PDF/A-1.
	return nil
}

// checkVersionPart4 validates that PDF/A-4 documents have PDF version 2.0.
// Checks catalog /Version first, then falls back to the file header.
func checkVersionPart4(doc *document.Document, catalog ast.PdfDict) []Violation {
	return checkVersionIs2(doc, catalog, Rule_19005_4_6_1_12_t01)
}

// checkVersionIs2 validates that the document uses PDF 2.0 version.
func checkVersionIs2(doc *document.Document, catalog ast.PdfDict, rule Rule) []Violation {
	version := catalogVersion(catalog)
	if version == "" {
		version = doc.Version
	}

	if version == "" {
		return nil
	}

	// Must be exactly "2.n" (3 characters) where n is a digit.
	if len(version) == 3 && version[0] == '2' && version[1] == '.' && version[2] >= '0' && version[2] <= '9' {
		return nil
	}

	return failf(rule, "PDF version %s is not 2.0", version)
}

// catalogVersion extracts the /Version name from the catalog dictionary.
// PDF names may contain #XX hex escape sequences (e.g., /#32#2E#30 = "2.0"),
// so the name is decoded before returning.
func catalogVersion(catalog ast.PdfDict) string {
	vObj, ok := catalog["Version"]
	if !ok {
		return ""
	}
	if name, ok := vObj.(ast.PdfName); ok {
		return decodePdfName(string(name))
	}
	return ""
}

// decodePdfName decodes #XX hex escape sequences in a PDF name.
func decodePdfName(name string) string {
	if !strings.Contains(name, "#") {
		return name
	}
	var buf strings.Builder
	buf.Grow(len(name))
	for i := 0; i < len(name); i++ {
		if name[i] == '#' && i+2 < len(name) {
			hi := unhex(name[i+1])
			lo := unhex(name[i+2])
			if hi >= 0 && lo >= 0 {
				buf.WriteByte(byte(hi<<4 | lo)) //nolint:gosec // hex decode; values bounded 0-15
				i += 2
				continue
			}
		}
		buf.WriteByte(name[i])
	}
	return buf.String()
}

// unhex returns the numeric value of a hex digit, or -1 if not a hex digit.
func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

// checkAlternatePresentations validates that /AlternatePresentations is not
// present in the /Names dictionary (parts 2+/4).
func checkAlternatePresentations(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Violation {
	namesObj, ok := catalog["Names"]
	if !ok {
		return nil
	}

	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil || namesDict == nil {
		doc.Logger.Debug("compliance.document: cannot resolve /Names dict", "error", err)
		return nil
	}

	if _, hasAP := namesDict["AlternatePresentations"]; hasAP {
		rule := alternatePresentationsRule(part)
		return fail(rule, "AlternatePresentations key present in Names dictionary")
	}

	return nil
}

// checkRequirementsKey validates that the /Requirements key is not present in
// the document catalog (parts 2+/4).
func checkRequirementsKey(catalog ast.PdfDict, part int) []Violation {
	if _, hasReq := catalog["Requirements"]; hasReq {
		rule := requirementsRule(part)
		return fail(rule, "Requirements key present in document catalog")
	}

	return nil
}

// checkNeedsRenderingKey validates that the catalog does not contain the
// forbidden /NeedsRendering key (ISO 19005-2/3/4 §6.4.2).
func checkNeedsRenderingKey(catalog ast.PdfDict, part int) []Violation {
	if _, ok := catalog["NeedsRendering"]; ok {
		return fail(needsRenderingRule(part), "NeedsRendering key present in document catalog")
	}

	return nil
}

// checkOCGUsageApplication validates that /OCProperties contains the /AS array
// when /OCGs are present (parts 2+/4).
func checkOCGUsageApplication(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Violation {
	ocPropsObj, ok := catalog["OCProperties"]
	if !ok {
		return nil
	}

	ocProps, err := document.ResolveDict(ctx, doc, ocPropsObj)
	if err != nil || ocProps == nil {
		doc.Logger.Debug("compliance.document: cannot resolve /OCProperties", "error", err)
		return nil
	}

	// /OCGs array is required when /OCProperties is present.
	ocgsObj, hasOCGs := ocProps["OCGs"]
	if !hasOCGs {
		return nil
	}

	ocgs, err := document.ResolveArray(ctx, doc, ocgsObj)
	if err != nil || len(ocgs) == 0 {
		doc.Logger.Debug("compliance.document: /OCGs array empty or unresolvable", "error", err)
		return nil
	}

	// /AS is only required when at least one OCG has a /Usage dictionary.
	// If no OCG defines usage entries, there is nothing for /AS to control.
	hasUsage := false
	for _, ocgObj := range ocgs {
		if ctx.Err() != nil {
			return nil
		}
		ocgDict, err := document.ResolveDict(ctx, doc, ocgObj)
		if err != nil || ocgDict == nil {
			continue
		}
		if _, ok := ocgDict["Usage"]; ok {
			hasUsage = true
			break
		}
	}

	if !hasUsage {
		return nil
	}

	// /D dict must exist and should have /AS array.
	dObj, hasD := ocProps["D"]
	if !hasD {
		return nil
	}

	dDict, err := document.ResolveDict(ctx, doc, dObj)
	if err != nil || dDict == nil {
		doc.Logger.Debug("compliance.document: cannot resolve /D dict", "error", err)
		return nil
	}

	if _, hasAS := dDict["AS"]; !hasAS {
		rule := ocgASRule(part)
		return fail(rule, "OCProperties /D dictionary missing AS array while OCGs with /Usage are present")
	}

	return nil
}

// checkInfoDictPart4 validates Info dict restrictions for PDF/A-4:
//   - /Info key forbidden in trailer unless /PieceInfo exists in catalog.
//   - If /Info exists, only /ModDate key allowed.
func checkInfoDictPart4(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []Violation {
	trailer := doc.Index.Trailer
	if trailer == nil {
		return nil
	}

	infoObj, hasInfo := trailer["Info"]
	if !hasInfo {
		return nil
	}

	// /Info is only allowed if /PieceInfo exists in catalog.
	_, hasPieceInfo := catalog["PieceInfo"]
	if !hasPieceInfo {
		return fail(Rule_19005_4_6_1_3_t04, "Info key present in trailer but PieceInfo not in catalog")
	}

	// If Info dict is present, it may only contain /ModDate.
	// Use ResolveDict to handle indirect references.
	infoDict, err := document.ResolveDict(ctx, doc, infoObj)
	if err != nil {
		doc.Logger.Debug("compliance.document: cannot resolve Info dict", "error", err)
		return nil
	}
	if infoDict != nil {
		var badKeys []string
		for key := range infoDict {
			if key != "ModDate" {
				badKeys = append(badKeys, string(key))
			}
		}
		if len(badKeys) > 0 {
			return failf(Rule_19005_4_6_1_3_t05,
				"Info dictionary contains keys [%s] (only ModDate allowed)", strings.Join(badKeys, ", "))
		}
	}

	return nil
}

// checkPermissionsDict validates that the /Perms dict only contains /UR3 and
// /DocMDP keys (parts 2+/4).
func checkPermissionsDict(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Violation {
	permsObj, ok := catalog["Perms"]
	if !ok {
		return nil
	}

	permsDict, err := document.ResolveDict(ctx, doc, permsObj)
	if err != nil || permsDict == nil {
		doc.Logger.Debug("compliance.document: cannot resolve /Perms dict", "error", err)
		return nil
	}

	var badKeys []string
	for key := range permsDict {
		if key != "UR3" && key != "DocMDP" {
			badKeys = append(badKeys, string(key))
		}
	}

	if len(badKeys) > 0 {
		rule := permsDictRule(part)
		return failf(rule,
			"Perms dictionary contains keys [%s] other than UR3 and DocMDP",
			strings.Join(badKeys, ", "))
	}

	return nil
}

func alternatePresentationsRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_10_t01, Rule_19005_3_6_10_t01, Rule_19005_4_6_11_t01)
}

func requirementsRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_11_t01, Rule_19005_3_6_11_t01, Rule_19005_4_6_12_t01)
}

func needsRenderingRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_4_2_t02, Rule_19005_3_6_4_2_t02, Rule_19005_4_6_4_2_t02)
}

func ocgASRule(part int) Rule {
	// The AS key rule in OC config dicts, used here for the /AS array check.
	return ruleByPart(part, Rule{}, Rule_19005_2_6_9_t04, Rule_19005_3_6_9_t04, Rule_19005_4_6_10_t01)
}

func permsDictRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_1_12_t01, Rule_19005_3_6_1_12_t01, Rule_19005_4_6_1_11_t01)
}

// checkForbiddenOCProperties validates ISO 19005-1 §6.1.13 t1: a PDF/A-1
// document catalog shall not contain the OCProperties key. Optional content is
// not permitted in PDF/A-1, and veraPDF derives isOptionalContentPresent from
// the catalog OCProperties entry, so its presence is the conformance signal.
func checkForbiddenOCProperties(catalog ast.PdfDict) []Violation {
	if _, ok := catalog["OCProperties"]; ok {
		return fail(Rule_19005_1_6_1_13_t01, "catalog contains /OCProperties (optional content forbidden in PDF/A-1)")
	}
	return nil
}

// ocConfigNamePDFARule selects the OC-configuration /Name presence rule by part:
// ISO 19005-2/3 §6.9 t1 and ISO 19005-4 §6.10 t1.
func ocConfigNamePDFARule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_9_t01, Rule_19005_3_6_9_t01, Rule_19005_4_6_10_t01)
}

// ocConfigNameUniquePDFARule selects the OC-configuration /Name uniqueness rule
// by part: ISO 19005-2/3 §6.9 t2 and ISO 19005-4 §6.10 t2.
func ocConfigNameUniquePDFARule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_9_t02, Rule_19005_3_6_9_t02, Rule_19005_4_6_10_t02)
}

// ocgOrderCompletePDFARule selects the OC-configuration /Order completeness rule
// by part: ISO 19005-2/3 §6.9 t3 and ISO 19005-4 §6.10 t3.
func ocgOrderCompletePDFARule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_9_t03, Rule_19005_3_6_9_t03, Rule_19005_4_6_10_t03)
}

// checkOCConfigNamesPDFA validates that OC configuration dictionaries in
// /OCProperties carry a /Name entry whose value is unique (ISO 19005-2/3 §6.9
// t1/t2, ISO 19005-4 §6.10 t1/t2). PDF/A-2 and PDF/A-3 additionally require the
// name to be non-empty (§6.9 t1: Name != null && length > 0); PDF/A-4 requires
// only presence (§6.10 t1: Name != null).
func checkOCConfigNamesPDFA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Violation {
	ocPropsObj, ok := catalog["OCProperties"]
	if !ok {
		return nil
	}

	ocProps, err := document.ResolveDict(ctx, doc, ocPropsObj)
	if err != nil || ocProps == nil {
		return nil
	}

	var results []Violation
	var names []string

	inspect := func(cfg ast.PdfDict, format string, args ...any) {
		name := configName(ctx, doc, cfg)
		if !configNameValid(cfg, name, part) {
			results = append(results, failf(ocConfigNamePDFARule(part), format, args...)...)
			return
		}
		if name != "" {
			names = append(names, name)
		}
	}

	// Check default config (/D).
	if dObj, hasD := ocProps["D"]; hasD {
		if dConfig, err := document.ResolveDict(ctx, doc, dObj); err == nil && dConfig != nil {
			inspect(dConfig, "default config (D)")
		}
	}

	// Check /Configs array.
	if configsObj, hasConfigs := ocProps["Configs"]; hasConfigs {
		if configs, err := document.ResolveArray(ctx, doc, configsObj); err == nil {
			for i, cfgObj := range configs {
				if ctx.Err() != nil {
					return results
				}
				cfg, err := document.ResolveDict(ctx, doc, cfgObj)
				if err != nil || cfg == nil {
					continue
				}
				inspect(cfg, "config %d", i)
			}
		}
	}

	// Check uniqueness of names.
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			results = append(results, failf(ocConfigNameUniquePDFARule(part), "duplicate name %q", n)...)
		}
		seen[n] = true
	}

	return results
}

// configNameValid reports whether an OC configuration dictionary's /Name
// satisfies the part requirement: presence only for PDF/A-4 (§6.10 t1), or
// presence and non-empty for PDF/A-2/3 (§6.9 t1).
func configNameValid(cfg ast.PdfDict, name string, part int) bool {
	if part == 4 {
		return configHasName(cfg)
	}
	return name != ""
}

// configHasName returns true if the OC configuration dict has a /Name key.
func configHasName(cfg ast.PdfDict) bool {
	_, ok := cfg["Name"]
	return ok
}

// configName extracts the /Name value from an OC configuration dictionary.
// The value may be a PdfString (text string) or occasionally a PdfName.
// Returns "" for absent, empty, or unresolvable values.
func configName(ctx context.Context, doc *document.Document, cfg ast.PdfDict) string {
	nameObj, ok := cfg["Name"]
	if !ok {
		return ""
	}
	// Try as string first (most common). Accept empty strings.
	nameStr, err := document.ResolveString(ctx, doc, nameObj)
	if err == nil {
		return string(nameStr)
	}
	// Try as name (some PDF generators).
	nameVal, err := document.ResolveName(ctx, doc, nameObj)
	if err == nil && nameVal != "" {
		return string(nameVal)
	}
	return ""
}

// checkPresSteps validates that no page contains a /PresSteps entry:
// ISO 19005-2/3 §6.10 t2 and ISO 19005-4 §6.11 t2.
func checkPresSteps(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Violation {
	pages := collectPages(ctx, doc, catalog)
	for i, page := range pages {
		if _, has := page["PresSteps"]; has {
			return failf(presStepsRule(part), "page %d contains /PresSteps entry", i)
		}
	}
	return nil
}

// presStepsRule selects the forbidden-/PresSteps rule by part: ISO 19005-2/3
// §6.10 t2 and ISO 19005-4 §6.11 t2.
func presStepsRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_10_t02, Rule_19005_3_6_10_t02, Rule_19005_4_6_11_t02)
}

// checkEmbeddedFileMIME validates PDF/A-4 §6.9 t1: embedded file streams must
// have a valid /Subtype MIME type.
func checkEmbeddedFileMIME(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []Violation {
	// Walk the /Names/EmbeddedFiles name tree.
	names, err := document.ResolveDict(ctx, doc, catalog["Names"])
	if err != nil || names == nil {
		return nil
	}
	efTree, err := document.ResolveDict(ctx, doc, names["EmbeddedFiles"])
	if err != nil || efTree == nil {
		return nil
	}
	namesArr, err := document.ResolveArray(ctx, doc, efTree["Names"])
	if err != nil || len(namesArr) == 0 {
		return nil
	}

	var results []Violation
	// Names array is [name1, filespec1, name2, filespec2, ...]
	for i := 1; i < len(namesArr); i += 2 {
		fileSpec, err := document.ResolveDict(ctx, doc, namesArr[i])
		if err != nil || fileSpec == nil {
			continue
		}
		efDict, err := document.ResolveDict(ctx, doc, fileSpec["EF"])
		if err != nil || efDict == nil {
			continue
		}
		// Check /F and /UF streams for /Subtype.
		for _, key := range []ast.PdfName{"F", "UF"} {
			streamObj, ok := efDict[key]
			if !ok {
				continue
			}
			stream, err := document.ResolveStream(ctx, doc, streamObj)
			if err != nil {
				doc.Logger.Debug("compliance.document: cannot resolve embedded file stream", "error", err)
				continue
			}
			subtypeObj, hasSubtype := stream.Dict["Subtype"]
			if !hasSubtype {
				results = append(results, failf(Rule_19005_4_6_9_t01,
					"embedded file stream missing /Subtype MIME type")...)
				break
			}
			// The MIME type is a PdfName which may have encoded '/' as '#2F'.
			// Resolve it to get the decoded name.
			subtype, _ := document.ResolveName(ctx, doc, subtypeObj)
			decoded := string(subtype)
			// Also try decoding #XX sequences for names not automatically decoded.
			decoded = strings.ReplaceAll(decoded, "#2F", "/")
			decoded = strings.ReplaceAll(decoded, "#2f", "/")
			if decoded == "" || !strings.Contains(decoded, "/") {
				results = append(results, failf(Rule_19005_4_6_9_t01,
					"embedded file stream invalid /Subtype MIME type: %q", subtype)...)
				break
			}
		}
	}
	return results
}

// checkOCGOrderCompleteness validates ISO 19005-2/3 §6.9 t3 and ISO 19005-4
// §6.10 t3: if an OC config dict has an /Order key, the Order array must
// reference all OCGs in the document.
func checkOCGOrderCompleteness(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Violation {
	ocProps, err := document.ResolveDict(ctx, doc, catalog["OCProperties"])
	if err != nil || ocProps == nil {
		return nil
	}

	// Collect all OCG references from /OCGs array.
	ocgsArr, err := document.ResolveArray(ctx, doc, ocProps["OCGs"])
	if err != nil || len(ocgsArr) == 0 {
		return nil
	}
	allOCGs := make(map[int]bool, len(ocgsArr))
	for _, ocg := range ocgsArr {
		if ref, ok := ocg.(ast.PdfIndirectRef); ok {
			allOCGs[ref.ObjectNum] = true
		}
	}

	// Check default config (/D) for /Order key.
	dObj, ok := ocProps["D"]
	if !ok {
		return nil
	}
	dConfig, err := document.ResolveDict(ctx, doc, dObj)
	if err != nil || dConfig == nil {
		return nil
	}
	orderObj, hasOrder := dConfig["Order"]
	if !hasOrder {
		return nil
	}

	// Collect OCG refs in the Order array (can be nested).
	orderOCGs := make(map[int]bool)
	collectOrderOCGRefs(ctx, doc, orderObj, orderOCGs, 0)

	// Check that all OCGs are referenced.
	for objNum := range allOCGs {
		if !orderOCGs[objNum] {
			return failf(ocgOrderCompletePDFARule(part),
				"OCG object %d not referenced in Order array", objNum)
		}
	}
	return nil
}

// collectOrderOCGRefs recursively collects OCG indirect refs from an Order array.
func collectOrderOCGRefs(ctx context.Context, doc *document.Document, obj ast.PdfObject, result map[int]bool, depth int) {
	if depth > 20 {
		return
	}
	// Direct indirect ref = OCG.
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		result[ref.ObjectNum] = true
		return
	}
	// Array: recurse into each element.
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		return
	}
	for _, item := range arr {
		collectOrderOCGRefs(ctx, doc, item, result, depth+1)
	}
}
