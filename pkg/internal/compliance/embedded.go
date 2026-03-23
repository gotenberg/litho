package compliance

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// validAFRelationships lists the valid values for /AFRelationship (ISO 32000-2,
// 7.11.3).
//
//nolint:gochecknoglobals // package-level lookup table.
var validAFRelationships = map[ast.PdfName]bool{
	"Source":           true,
	"Data":             true,
	"Alternative":      true,
	"Supplement":       true,
	"EncryptedPayload": true,
	"FormData":         true,
	"Schema":           true,
	"Unspecified":      true,
	"Undefined":        true, // Used in some VeraPDF test corpus files.
}

// checkEmbeddedGroup validates embedded file compliance rules for PDF/A.
// For PDF/UA profiles, no checks apply and nil is returned.
//
// Validated rules:
//   - PDF/A-1 §6.1.11: embedded files are forbidden.
//   - PDF/A-2/3: embedded files must be PDF/A compliant, must have /AFRelationship (part 3+).
//   - PDF/A-4/4e/4f: embedded files must be PDF/A compliant, must have /AFRelationship,
//     /F and /UF keys, and PDF/A-4f requires /EmbeddedFiles in /Names.
func checkEmbeddedGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if isPDFUA(profile) {
		return nil
	}

	part := pdfaPart(profile)
	if part == 0 {
		slog.Debug("compliance_2/embedded: skipping embedded checks for unknown profile", "profile", profile)
		return nil
	}

	slog.Debug("compliance_2/embedded: starting embedded file checks", "profile", profile, "part", part)

	var results []Result

	// Resolve /Names dictionary from catalog.
	namesDict, hasNames := resolveNamesDict(ctx, doc, catalog)

	// --- PDF/A-1: embedded files are completely forbidden (§6.1.11) ---
	if part == 1 {
		results = append(results, checkEmbeddedForbiddenPart1(ctx, doc, catalog, namesDict, hasNames)...)
		slog.Debug("compliance_2/embedded: part 1 checks complete", "violations", len(results))
		return results
	}

	// --- PDF/A-4f: /EmbeddedFiles key required in /Names ---
	if pdfaLevel(profile) == 'f' && part == 4 {
		if !hasNames {
			slog.Debug("compliance_2/embedded: PDF/A-4f missing /Names dict")
			results = append(results, fail(Rule_19005_4_6_9_t05, "Names dictionary missing from catalog")...)
		} else if _, hasEF := namesDict["EmbeddedFiles"]; !hasEF {
			slog.Debug("compliance_2/embedded: PDF/A-4f missing /EmbeddedFiles in /Names")
			results = append(results, fail(Rule_19005_4_6_9_t05, "EmbeddedFiles key missing from Names dictionary")...)
		}
	}

	// --- Collect file specification dicts with /EF key (parts 2+) ---
	fileSpecs := collectFileSpecs(ctx, doc)
	slog.Debug("compliance_2/embedded: found file specification dicts with /EF", "count", len(fileSpecs))

	for _, fs := range fileSpecs {
		if ctx.Err() != nil {
			return results
		}

		// Check /F and /UF keys (§7.11, required for parts 2+ generally, strictly for part 4).
		results = append(results, checkFileSpecFUF(ctx, doc, fs, part)...)

		// Check /AFRelationship (required for part 3+ and part 4).
		results = append(results, checkAFRelationship(ctx, doc, fs, part, profile)...)
	}

	// --- §6.8 t4: Catalog /AF array must reference file spec dicts (parts 3+) ---
	if part >= 3 {
		results = append(results, checkCatalogAF(ctx, doc, catalog, fileSpecs, part)...)
	}

	slog.Debug("compliance_2/embedded: embedded file checks complete", "violations", len(results))
	return results
}

// resolveNamesDict resolves the /Names dict from the catalog.
func resolveNamesDict(ctx context.Context, doc *document.Document, catalog ast.PdfDict) (ast.PdfDict, bool) {
	namesObj, ok := catalog["Names"]
	if !ok {
		return nil, false
	}
	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil || namesDict == nil {
		slog.Debug("compliance_2/embedded: cannot resolve /Names dict", "error", err)
		return nil, false
	}
	return namesDict, true
}

// checkEmbeddedForbiddenPart1 validates that PDF/A-1 documents do not contain
// embedded files (§6.1.11). /EmbeddedFiles key is forbidden in /Names, and
// file specification dicts must not contain /EF.
func checkEmbeddedForbiddenPart1(ctx context.Context, doc *document.Document, _ ast.PdfDict, namesDict ast.PdfDict, hasNames bool) []Result {
	var results []Result

	// Check /EmbeddedFiles in /Names.
	if hasNames {
		if _, hasEF := namesDict["EmbeddedFiles"]; hasEF {
			slog.Debug("compliance_2/embedded: PDF/A-1 Names dict contains /EmbeddedFiles")
			results = append(results, fail(Rule_19005_1_6_1_11_t02,
				"EmbeddedFiles key present in Names dictionary")...)
		}
	}

	// Check file spec dicts for /EF key.
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free {
			continue
		}

		dict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || dict == nil {
			continue
		}

		typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
		if typeName != "Filespec" {
			continue
		}

		if _, hasEF := dict["EF"]; hasEF {
			slog.Debug("compliance_2/embedded: PDF/A-1 file spec has /EF key", "object", objNum)
			results = append(results, Result{
				Rule:    Rule_19005_1_6_1_11_t01,
				Context: fmt.Sprintf("object %d: file specification contains EF key", objNum),
			})
		}
	}

	return results
}

// fileSpecEntry holds a resolved file specification dict and its object number.
type fileSpecEntry struct {
	dict   ast.PdfDict
	objNum int
}

// collectFileSpecs scans all objects to find file specification dictionaries
// that contain an /EF key (indicating embedded files).
func collectFileSpecs(ctx context.Context, doc *document.Document) []fileSpecEntry {
	var specs []fileSpecEntry

	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return specs
		}
		if entry.Free {
			continue
		}

		dict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || dict == nil {
			continue
		}

		typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
		if typeName != "Filespec" {
			continue
		}

		if _, hasEF := dict["EF"]; !hasEF {
			continue
		}

		slog.Debug("compliance_2/embedded: found file spec with /EF", "object", objNum)
		specs = append(specs, fileSpecEntry{dict: dict, objNum: objNum})
	}

	return specs
}

// checkFileSpecFUF validates that a file specification dict contains both /F and /UF keys.
func checkFileSpecFUF(_ context.Context, _ *document.Document, fs fileSpecEntry, part int) []Result {
	_, hasF := fs.dict["F"]
	_, hasUF := fs.dict["UF"]

	if hasF && hasUF {
		slog.Debug("compliance_2/embedded: file spec has /F and /UF", "object", fs.objNum)
		return nil
	}

	slog.Debug("compliance_2/embedded: file spec missing /F or /UF",
		"object", fs.objNum, "hasF", hasF, "hasUF", hasUF)

	rule := fileSpecFUFRule(part)
	return failf(rule, "object %d: file specification missing F=%v UF=%v", fs.objNum, hasF, hasUF)
}

// checkAFRelationship validates that a file specification dict has a valid
// /AFRelationship key. Required for parts 3+ and part 4.
func checkAFRelationship(ctx context.Context, doc *document.Document, fs fileSpecEntry, part int, _ string) []Result {
	// Parts 2 does not require /AFRelationship.
	if part < 3 {
		return nil
	}

	afObj, hasAF := fs.dict["AFRelationship"]
	if !hasAF {
		slog.Debug("compliance_2/embedded: file spec missing /AFRelationship", "object", fs.objNum)
		rule := afRelationshipRule(part)
		return failf(rule, "object %d: file specification missing AFRelationship key", fs.objNum)
	}

	// Validate the value of /AFRelationship.
	afName, err := document.ResolveName(ctx, doc, afObj)
	if err != nil || afName == "" {
		slog.Debug("compliance_2/embedded: /AFRelationship is not a valid name",
			"object", fs.objNum, "error", err)
		rule := afRelationshipRule(part)
		return failf(rule, "object %d: AFRelationship is not a valid name", fs.objNum)
	}

	if !validAFRelationships[afName] {
		slog.Debug("compliance_2/embedded: invalid /AFRelationship value",
			"object", fs.objNum, "value", afName)
		rule := afRelationshipRule(part)
		return failf(rule, "object %d: AFRelationship value %q is not valid", fs.objNum, afName)
	}

	slog.Debug("compliance_2/embedded: /AFRelationship OK", "object", fs.objNum, "value", afName)
	return nil
}

// --- Rule selectors ---

func fileSpecFUFRule(part int) Rule {
	switch part {
	case 2:
		return Rule_19005_2_6_8_t02
	case 3:
		return Rule_19005_3_6_8_t02
	case 4:
		return Rule_19005_4_6_9_t02
	default:
		return Rule_19005_2_6_8_t02
	}
}

func afRelationshipRule(part int) Rule {
	switch part {
	case 3:
		return Rule_19005_3_6_8_t03
	case 4:
		return Rule_19005_4_6_9_t04
	default:
		return Rule_19005_3_6_8_t03
	}
}

// checkCatalogAF validates §6.8 t4: for PDF/A-3+, each file specification dict
// with an /EF key must be referenced from the catalog's /AF array. This ensures
// the associated files are properly declared at the document level.
func checkCatalogAF(ctx context.Context, doc *document.Document, catalog ast.PdfDict, fileSpecs []fileSpecEntry, part int) []Result {
	if len(fileSpecs) == 0 {
		slog.Debug("compliance_2/embedded: no file specs to check for catalog /AF")
		return nil
	}

	// Only file specs with /AFRelationship are "associated files" that require
	// catalog /AF. Legacy embedded files without /AFRelationship are exempt.
	var associatedSpecs []fileSpecEntry
	for _, fs := range fileSpecs {
		if _, hasAFR := fs.dict["AFRelationship"]; hasAFR {
			associatedSpecs = append(associatedSpecs, fs)
		}
	}
	if len(associatedSpecs) == 0 {
		slog.Debug("compliance_2/embedded: no associated files (no /AFRelationship), skipping catalog /AF check")
		return nil
	}

	rule := catalogAFRule(part)

	afObj, hasAF := catalog["AF"]
	if !hasAF {
		slog.Debug("compliance_2/embedded: catalog missing /AF array but associated file specs exist")
		return failf(rule, "catalog missing /AF array but %d embedded file(s) found", len(associatedSpecs))
	}

	afArr, err := document.ResolveArray(ctx, doc, afObj)
	if err != nil || afArr == nil {
		slog.Debug("compliance_2/embedded: catalog /AF is not a valid array", "error", err)
		return failf(rule, "catalog /AF is not a valid array")
	}

	// Build a set of object numbers referenced in the /AF array.
	afRefs := make(map[int]bool, len(afArr))
	for _, elem := range afArr {
		if ref, ok := elem.(ast.PdfIndirectRef); ok {
			afRefs[ref.ObjectNum] = true
		}
	}

	slog.Debug("compliance_2/embedded: catalog /AF references", "count", len(afRefs))

	var results []Result
	for _, fs := range fileSpecs {
		if !afRefs[fs.objNum] {
			slog.Debug("compliance_2/embedded: file spec not referenced in catalog /AF",
				"object", fs.objNum)
			results = append(results, Result{
				Rule:    rule,
				Context: fmt.Sprintf("object %d: file specification with /EF not referenced in catalog /AF array", fs.objNum),
			})
		}
	}

	return results
}

func catalogAFRule(part int) Rule {
	switch part {
	case 3:
		return Rule_19005_3_6_8_t04
	case 4:
		return Rule_19005_4_6_9_t04
	default:
		return Rule_19005_3_6_8_t04
	}
}
