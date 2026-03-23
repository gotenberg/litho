package compliance

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkDocumentGroup validates document-level properties for PDF/A and PDF/UA:
//   - PDF/A-1: PDF version must be 1.4 or lower.
//   - PDF/A-4: PDF version must be 2.0.
//   - PDF/A parts 2+/4: /AlternatePresentations forbidden in /Names,
//     /Requirements key forbidden in catalog.
//   - PDF/A parts 2+/4: /OCProperties must have /AS array if /OCGs present.
//   - PDF/A-4: /Info dict restrictions, /Perms dict restrictions.
//   - PDF/UA-2: PDF version must be 2.0.
func checkDocumentGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	part := pdfaPart(profile)

	slog.Debug("compliance_2/document: starting document checks",
		"profile", profile, "part", part, "isPDFUA", isPDFUA(profile))

	var results []Result

	// --- PDF/UA-2: version must be 2.0 ---
	if isPDFUA(profile) {
		if profile == "PDF/UA-2" {
			results = append(results, checkVersionIs2(doc, catalog, Rule_14289_2_6_2_t01)...)
		}
		slog.Debug("compliance_2/document: PDF/UA document checks complete", "violations", len(results))
		return results
	}

	if part == 0 {
		slog.Debug("compliance_2/document: skipping document checks for unknown profile", "profile", profile)
		return nil
	}

	// --- PDF/A-1: version must be 1.4 or lower ---
	if part == 1 {
		results = append(results, checkVersionPart1(doc, catalog)...)
	}

	// --- PDF/A-4: version must be 2.0 ---
	if part == 4 {
		results = append(results, checkVersionPart4(doc, catalog)...)
	}

	// --- Catalog restrictions (parts 2+/4) ---
	if part >= 2 {
		results = append(results, checkAlternatePresentations(ctx, doc, catalog, part)...)
		results = append(results, checkRequirementsKey(catalog, part)...)
	}

	// --- OCG Usage Application (parts 2+/4) ---
	if part >= 2 {
		results = append(results, checkOCGUsageApplication(ctx, doc, catalog, part)...)
	}

	// --- OCG Config /Name checks (part 4) ---
	if part == 4 {
		results = append(results, checkOCConfigNamesPDFA(ctx, doc, catalog)...)
	}

	// --- Info dict restrictions (part 4) ---
	if part == 4 {
		results = append(results, checkInfoDictPart4(doc, catalog)...)
	}

	// --- Permissions dict ---
	if part >= 2 {
		results = append(results, checkPermissionsDict(ctx, doc, catalog, part)...)
	}

	slog.Debug("compliance_2/document: document checks complete", "violations", len(results))
	return results
}

// checkVersionPart1 validates that the PDF header format is correct for PDF/A-1.
// Per §6.1.2, the rule checks that the header is `%PDF-x.y` format, NOT the
// version number value. VeraPDF does not enforce a maximum version here.
func checkVersionPart1(_ *document.Document, _ ast.PdfDict) []Result {
	// The header format is validated at parse time by the lexer.
	// §6.1.2 does not restrict the version number for PDF/A-1.
	return nil
}

// checkVersionPart4 validates that PDF/A-4 documents have PDF version 2.0.
// Checks catalog /Version first, then falls back to the file header.
func checkVersionPart4(doc *document.Document, catalog ast.PdfDict) []Result {
	return checkVersionIs2(doc, catalog, Rule_19005_4_6_1_12_t01)
}

// checkVersionIs2 validates that the document uses PDF 2.0 version.
func checkVersionIs2(doc *document.Document, catalog ast.PdfDict, rule Rule) []Result {
	version := catalogVersion(catalog)
	if version == "" {
		version = doc.Version
	}

	slog.Debug("compliance_2/document: checking PDF 2.0 version requirement", "version", version)

	if version == "" {
		return nil
	}

	// Must start with "2.".
	if len(version) >= 3 && version[0] == '2' && version[1] == '.' && version[2] >= '0' && version[2] <= '9' {
		slog.Debug("compliance_2/document: PDF 2.0 version OK", "version", version)
		return nil
	}

	slog.Debug("compliance_2/document: version is not 2.x", "version", version)
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
				buf.WriteByte(byte(hi<<4 | lo)) //nolint:mnd,gosec // hex decode; values bounded 0-15
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
		return int(c-'a') + 10 //nolint:mnd // hex offset
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10 //nolint:mnd // hex offset
	default:
		return -1
	}
}

// checkAlternatePresentations validates that /AlternatePresentations is not
// present in the /Names dictionary (parts 2+/4).
func checkAlternatePresentations(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Result {
	namesObj, ok := catalog["Names"]
	if !ok {
		slog.Debug("compliance_2/document: no /Names dict in catalog")
		return nil
	}

	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil || namesDict == nil {
		slog.Debug("compliance_2/document: cannot resolve /Names dict", "error", err)
		return nil
	}

	if _, hasAP := namesDict["AlternatePresentations"]; hasAP {
		slog.Debug("compliance_2/document: /AlternatePresentations found in /Names")
		rule := alternatePresentationsRule(part)
		return fail(rule, "AlternatePresentations key present in Names dictionary")
	}

	slog.Debug("compliance_2/document: no /AlternatePresentations — pass")
	return nil
}

// checkRequirementsKey validates that the /Requirements key is not present in
// the document catalog (parts 2+/4).
func checkRequirementsKey(catalog ast.PdfDict, part int) []Result {
	if _, hasReq := catalog["Requirements"]; hasReq {
		slog.Debug("compliance_2/document: /Requirements key found in catalog")
		rule := requirementsRule(part)
		return fail(rule, "Requirements key present in document catalog")
	}

	slog.Debug("compliance_2/document: no /Requirements key — pass")
	return nil
}

// checkOCGUsageApplication validates that /OCProperties contains the /AS array
// when /OCGs are present (parts 2+/4).
func checkOCGUsageApplication(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Result {
	ocPropsObj, ok := catalog["OCProperties"]
	if !ok {
		slog.Debug("compliance_2/document: no /OCProperties in catalog")
		return nil
	}

	ocProps, err := document.ResolveDict(ctx, doc, ocPropsObj)
	if err != nil || ocProps == nil {
		slog.Debug("compliance_2/document: cannot resolve /OCProperties", "error", err)
		return nil
	}

	// Check if /OCGs array exists.
	ocgsObj, hasOCGs := ocProps["OCGs"]
	if !hasOCGs {
		slog.Debug("compliance_2/document: no /OCGs in /OCProperties")
		return nil
	}

	ocgs, err := document.ResolveArray(ctx, doc, ocgsObj)
	if err != nil || len(ocgs) == 0 {
		slog.Debug("compliance_2/document: /OCGs array empty or unresolvable", "error", err)
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
		slog.Debug("compliance_2/document: no OCG has /Usage dictionary, /AS not required")
		return nil
	}

	// /D dict must exist and should have /AS array.
	dObj, hasD := ocProps["D"]
	if !hasD {
		slog.Debug("compliance_2/document: /OCProperties missing /D entry")
		return nil
	}

	dDict, err := document.ResolveDict(ctx, doc, dObj)
	if err != nil || dDict == nil {
		slog.Debug("compliance_2/document: cannot resolve /D dict", "error", err)
		return nil
	}

	if _, hasAS := dDict["AS"]; !hasAS {
		slog.Debug("compliance_2/document: /D dict missing /AS array with OCGs having /Usage")
		rule := ocgASRule(part)
		return fail(rule, "OCProperties /D dictionary missing AS array while OCGs with /Usage are present")
	}

	slog.Debug("compliance_2/document: OCG /AS array present — pass")
	return nil
}

// checkInfoDictPart4 validates Info dict restrictions for PDF/A-4:
//   - /Info key forbidden in trailer unless /PieceInfo exists in catalog.
//   - If /Info exists, only /ModDate key allowed.
func checkInfoDictPart4(doc *document.Document, catalog ast.PdfDict) []Result {
	trailer := doc.Index.Trailer
	if trailer == nil {
		return nil
	}

	infoObj, hasInfo := trailer["Info"]
	if !hasInfo {
		slog.Debug("compliance_2/document: no /Info in trailer — pass (PDF/A-4)")
		return nil
	}

	// /Info is only allowed if /PieceInfo exists in catalog.
	_, hasPieceInfo := catalog["PieceInfo"]
	if !hasPieceInfo {
		slog.Debug("compliance_2/document: /Info present without /PieceInfo in catalog")
		return fail(Rule_19005_4_6_1_3_t04, "Info key present in trailer but PieceInfo not in catalog")
	}

	// If Info dict is present, it may only contain /ModDate.
	if infoDict, ok := infoObj.(ast.PdfDict); ok {
		var badKeys []string
		for key := range infoDict {
			if key != "ModDate" {
				badKeys = append(badKeys, string(key))
			}
		}
		if len(badKeys) > 0 {
			slog.Debug("compliance_2/document: Info dict has non-ModDate keys", "keys", badKeys)
			return failf(Rule_19005_4_6_1_3_t05,
				"Info dictionary contains keys [%s] (only ModDate allowed)", strings.Join(badKeys, ", "))
		}
	}

	slog.Debug("compliance_2/document: PDF/A-4 Info dict OK")
	return nil
}

// checkPermissionsDict validates that the /Perms dict only contains /UR3 and
// /DocMDP keys (parts 2+/4).
func checkPermissionsDict(ctx context.Context, doc *document.Document, catalog ast.PdfDict, part int) []Result {
	permsObj, ok := catalog["Perms"]
	if !ok {
		slog.Debug("compliance_2/document: no /Perms in catalog")
		return nil
	}

	permsDict, err := document.ResolveDict(ctx, doc, permsObj)
	if err != nil || permsDict == nil {
		slog.Debug("compliance_2/document: cannot resolve /Perms dict", "error", err)
		return nil
	}

	var badKeys []string
	for key := range permsDict {
		if key != "UR3" && key != "DocMDP" {
			badKeys = append(badKeys, string(key))
		}
	}

	if len(badKeys) > 0 {
		slog.Debug("compliance_2/document: /Perms dict has invalid keys", "keys", badKeys)
		rule := permsDictRule(part)
		return failf(rule,
			"Perms dictionary contains keys [%s] other than UR3 and DocMDP",
			strings.Join(badKeys, ", "))
	}

	slog.Debug("compliance_2/document: /Perms dict OK")
	return nil
}

// --- Rule selectors ---

func alternatePresentationsRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_10_t01
	case 3:
		return Rule_19005_3_6_10_t01
	case 4:
		return Rule_19005_4_6_11_t01
	default:
		return Rule_19005_2_6_10_t01
	}
}

func requirementsRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_11_t01
	case 3:
		return Rule_19005_3_6_11_t01
	case 4:
		return Rule_19005_4_6_12_t01
	default:
		return Rule_19005_2_6_11_t01
	}
}

func ocgASRule(part int) Rule {
	// The AS key rule in OC config dicts — used here for the /AS array check.
	switch part {
	case 2:
		return Rule_19005_2_6_9_t04
	case 3:
		return Rule_19005_3_6_9_t04
	case 4:
		return Rule_19005_4_6_10_t01
	default:
		return Rule_19005_2_6_9_t04
	}
}

func permsDictRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_1_12_t01
	case 3:
		return Rule_19005_3_6_1_12_t01
	case 4:
		return Rule_19005_4_6_1_11_t01
	default:
		return Rule_19005_2_6_1_12_t01
	}
}

// checkOCConfigNamesPDFA validates that OC configuration dictionaries in
// /OCProperties have /Name entries and that names are unique (PDF/A-4, §6.10).
func checkOCConfigNamesPDFA(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []Result {
	ocPropsObj, ok := catalog["OCProperties"]
	if !ok {
		return nil
	}

	ocProps, err := document.ResolveDict(ctx, doc, ocPropsObj)
	if err != nil || ocProps == nil {
		return nil
	}

	var results []Result
	var names []string

	// Check default config (/D).
	dObj, hasD := ocProps["D"]
	if hasD {
		dConfig, err := document.ResolveDict(ctx, doc, dObj)
		if err == nil && dConfig != nil {
			name := configName(ctx, doc, dConfig)
			if name == "" {
				slog.Debug("compliance_2/document: PDF/A-4 default OC config missing /Name")
				results = append(results, fail(Rule_19005_4_6_10_t01, "default config (D)")...)
			} else {
				names = append(names, name)
			}
		}
	}

	// Check /Configs array.
	configsObj, hasConfigs := ocProps["Configs"]
	if hasConfigs {
		configs, err := document.ResolveArray(ctx, doc, configsObj)
		if err == nil {
			for i, cfgObj := range configs {
				if ctx.Err() != nil {
					return results
				}
				cfg, err := document.ResolveDict(ctx, doc, cfgObj)
				if err != nil || cfg == nil {
					continue
				}
				name := configName(ctx, doc, cfg)
				if name == "" {
					slog.Debug("compliance_2/document: PDF/A-4 OC config missing /Name", "index", i)
					results = append(results, failf(Rule_19005_4_6_10_t01, "config %d", i)...)
				} else {
					names = append(names, name)
				}
			}
		}
	}

	// Check uniqueness of names.
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			slog.Debug("compliance_2/document: PDF/A-4 OC config duplicate /Name", "name", n)
			results = append(results, failf(Rule_19005_4_6_10_t02, "duplicate name %q", n)...)
		}
		seen[n] = true
	}

	return results
}

// configName extracts the /Name value from an OC configuration dictionary.
// The value may be a PdfString (text string) or occasionally a PdfName.
func configName(ctx context.Context, doc *document.Document, cfg ast.PdfDict) string {
	nameObj, ok := cfg["Name"]
	if !ok {
		return ""
	}
	// Try as string first (most common).
	nameStr, err := document.ResolveString(ctx, doc, nameObj)
	if err == nil && len(nameStr) > 0 {
		return string(nameStr)
	}
	// Try as name (some PDF generators).
	nameVal, err := document.ResolveName(ctx, doc, nameObj)
	if err == nil && nameVal != "" {
		return string(nameVal)
	}
	return ""
}
