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

	// Collect all /AF references from catalog, pages, and annotations.
	// The /AF array can appear on any of these per the spec.
	afRefs := make(map[int]bool)
	collectAFRefs(ctx, doc, catalog["AF"], afRefs)

	// Also collect from pages, their annotations, and XObject resources.
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if ctx.Err() != nil {
			break
		}
		collectAFRefs(ctx, doc, page["AF"], afRefs)

		// Annotations.
		annotsObj, ok := page["Annots"]
		if ok {
			annots, err := document.ResolveArray(ctx, doc, annotsObj)
			if err == nil {
				for _, annotObj := range annots {
					annotDict, err := document.ResolveDict(ctx, doc, annotObj)
					if err != nil || annotDict == nil {
						continue
					}
					collectAFRefs(ctx, doc, annotDict["AF"], afRefs)

					// Annotations with /FS directly reference a file spec
					// (e.g., FileAttachment). This counts as a valid
					// association — no separate /AF entry required.
					if fsObj, hasFS := annotDict["FS"]; hasFS {
						if ref, isRef := fsObj.(ast.PdfIndirectRef); isRef {
							afRefs[ref.ObjectNum] = true
						}
					}

					// RichMedia annotations reference file specs through
					// /RichMediaContent/Assets (a name tree).
					collectRichMediaFileSpecRefs(ctx, doc, annotDict, afRefs)
				}
			}
		}

		// XObject resources (Image and Form XObjects can have /AF).
		res := getResources(ctx, doc, page)
		if res != nil {
			xobjDict, err := document.ResolveDict(ctx, doc, res["XObject"])
			if err == nil && xobjDict != nil {
				for _, xobjObj := range xobjDict {
					stream, err := document.ResolveStream(ctx, doc, xobjObj)
					if err != nil {
						// Try as dict (some XObjects may be plain dicts).
						xd, err := document.ResolveDict(ctx, doc, xobjObj)
						if err == nil && xd != nil {
							collectAFRefs(ctx, doc, xd["AF"], afRefs)
						}
						continue
					}
					collectAFRefs(ctx, doc, stream.Dict["AF"], afRefs)
				}
			}
		}
	}

	slog.Debug("compliance_2/embedded: total /AF references collected", "count", len(afRefs))

	if len(afRefs) == 0 {
		slog.Debug("compliance_2/embedded: no /AF arrays found anywhere but associated file specs exist")
		return failf(rule, "no /AF array found but %d embedded file(s) with /AFRelationship exist", len(associatedSpecs))
	}

	var results []Result
	for _, fs := range associatedSpecs {
		if !afRefs[fs.objNum] {
			slog.Debug("compliance_2/embedded: file spec not referenced in any /AF",
				"object", fs.objNum)
			results = append(results, Result{
				Rule:    rule,
				Context: fmt.Sprintf("object %d: file specification with /AFRelationship not referenced in any /AF array", fs.objNum),
			})
		}
	}

	return results
}

// collectAFRefs adds object numbers from an /AF array to the refs set.
func collectAFRefs(ctx context.Context, doc *document.Document, afObj ast.PdfObject, refs map[int]bool) {
	if afObj == nil {
		return
	}
	arr, err := document.ResolveArray(ctx, doc, afObj)
	if err != nil || arr == nil {
		return
	}
	for _, elem := range arr {
		if ref, ok := elem.(ast.PdfIndirectRef); ok {
			refs[ref.ObjectNum] = true
		}
	}
}

// collectRichMediaFileSpecRefs extracts file spec references from a RichMedia
// annotation. File specs can appear in two places:
//   - /RichMediaContent/Assets name tree (key/value pairs)
//   - /RichMediaContent/Configurations/*/Instances/*/Asset
func collectRichMediaFileSpecRefs(ctx context.Context, doc *document.Document, annotDict ast.PdfDict, refs map[int]bool) {
	rmContent, ok := annotDict["RichMediaContent"]
	if !ok {
		return
	}
	rmDict, err := document.ResolveDict(ctx, doc, rmContent)
	if err != nil || rmDict == nil {
		return
	}

	// 1. /Assets name tree.
	assetsDict, err := document.ResolveDict(ctx, doc, rmDict["Assets"])
	if err == nil && assetsDict != nil {
		namesArr, err := document.ResolveArray(ctx, doc, assetsDict["Names"])
		if err == nil {
			for i := 1; i < len(namesArr); i += 2 {
				if ref, ok := namesArr[i].(ast.PdfIndirectRef); ok {
					refs[ref.ObjectNum] = true
				}
			}
		}
	}

	// 2. /Configurations → instances → /Asset.
	configs, err := document.ResolveArray(ctx, doc, rmDict["Configurations"])
	if err != nil {
		return
	}
	for _, cfgObj := range configs {
		cfgDict, err := document.ResolveDict(ctx, doc, cfgObj)
		if err != nil || cfgDict == nil {
			continue
		}
		instances, err := document.ResolveArray(ctx, doc, cfgDict["Instances"])
		if err != nil {
			continue
		}
		for _, instObj := range instances {
			instDict, err := document.ResolveDict(ctx, doc, instObj)
			if err != nil || instDict == nil {
				continue
			}
			if assetRef, ok := instDict["Asset"].(ast.PdfIndirectRef); ok {
				refs[assetRef.ObjectNum] = true
			}
		}
	}
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
