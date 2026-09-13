package compliance

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// mimeTypePattern matches a "type/subtype" MIME string. ISO 19005-3 §6.8
// requires an embedded file's /Subtype to be a valid MIME media type, so the
// pattern enforces the "type/subtype" shape with the RFC 2045 token charset.
//
//nolint:gochecknoglobals // compiled-once pattern shared by the MIME check.
var mimeTypePattern = regexp.MustCompile(`^[-\w+.]+/[-\w+.]+$`)

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
	// "Undefined" is not an ISO 32000-2 §14.13 value (the spec value is
	// "Unspecified"). This relaxes §14.13: real PDF/A-3 producers emit
	// "Undefined" as a synonym for "Unspecified", so accepting it avoids
	// false positives on otherwise conformant files.
	"Undefined": true,
}

// checkEmbeddedGroup validates embedded file compliance rules for PDF/A.
// For PDF/UA profiles, no checks apply and nil is returned.
//
// Validated rules:
//   - PDF/A-1 §6.1.11: embedded files are forbidden.
//   - PDF/A-2/3: embedded files must be PDF/A compliant, must have /AFRelationship (part 3+).
//   - PDF/A-4/4e/4f: embedded files must be PDF/A compliant, must have /AFRelationship,
//     /F and /UF keys, and PDF/A-4f requires /EmbeddedFiles in /Names.
func checkEmbeddedGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part

	if vctx.ua {
		return nil
	}

	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.embedded: starting embedded file checks", "profile", profile, "part", part)

	var results []Violation

	// Resolve /Names dictionary from catalog.
	namesDict, hasNames := resolveNamesDict(ctx, doc, catalog)

	// §6.1.11: PDF/A-1 forbids embedded files. checkEmbeddedFiles states both
	// halves of the clause, t1 against every file specification carrying /EF
	// and t2 against /Names /EmbeddedFiles, so stating them here as well
	// reported each offending object twice. It also sweeps the xref entries in
	// sorted order, which this file's copy did not (CONTRIBUTING
	// "Deterministic output").
	if part == 1 {
		doc.Logger.Debug("compliance.embedded: part 1 handled by checkEmbeddedFiles")
		return results
	}

	// PDF/A-4f: /EmbeddedFiles key required in /Names.
	if vctx.level == 'f' && part == 4 {
		if !hasNames {
			results = append(results, fail(Rule_19005_4_6_9_t05, "Names dictionary missing from catalog")...)
		} else if _, hasEF := namesDict["EmbeddedFiles"]; !hasEF {
			results = append(results, fail(Rule_19005_4_6_9_t05, "EmbeddedFiles key missing from Names dictionary")...)
		}
	}

	fileSpecs := collectFileSpecs(ctx, doc, catalog)
	var embeddedSpecs []fileSpecEntry
	for _, fs := range fileSpecs {
		if fs.hasEF {
			embeddedSpecs = append(embeddedSpecs, fs)
		}
	}

	for _, fs := range fileSpecs {
		if ctx.Err() != nil {
			return results
		}

		// Check /F and /UF keys (§7.11). The rule scopes to "the file
		// specification dictionary for an embedded file", so a plain
		// filespec without /EF is out of scope and not flagged.
		if fs.hasEF {
			results = append(results, checkFileSpecFUF(ctx, doc, fs, part)...)
		}

		// Check /AFRelationship. PDF/A-3 §6.8 t3 applies to every file
		// specification dictionary; PDF/A-4 §6.9 t4 stays scoped to
		// embedded ones (no corpus evidence of the wider scope there).
		if part == 3 || fs.hasEF {
			results = append(results, checkAFRelationship(ctx, doc, fs, part, profile)...)
		}

		// §6.8 t1 (PDF/A-3): embedded file stream /Subtype must be a valid MIME type.
		if part == 3 && fs.hasEF {
			results = append(results, checkEmbeddedSubtypeMIME(ctx, doc, fs)...)
		}
	}

	// §6.9 t3: PDF/A-4 embedded files must themselves be PDF/A-compliant.
	// Applies only to base PDF/A-4 (level '4'), not PDF/A-4e or PDF/A-4f.
	if part == 4 && vctx.level == '4' {
		results = append(results, checkEmbeddedFileCompliance(ctx, vctx.doc, embeddedSpecs,
			Rule_19005_4_6_9_t03,
			[]models.PDFAProfile{models.PDFA4, models.PDFA2b, models.PDFA1b})...)
	}

	// §6.8 t5 (PDF/A-2 only): embedded files shall comply with ISO
	// 19005-1 or 19005-2. Part 3 deliberately has no such rule: PDF/A-3
	// permits attachments of any type.
	if part == 2 {
		results = append(results, checkEmbeddedFileCompliance(ctx, vctx.doc, embeddedSpecs,
			Rule_19005_2_6_8_t05,
			[]models.PDFAProfile{models.PDFA2b, models.PDFA1b})...)
	}

	// §6.8 t4: catalog /AF array must reference file spec dicts (parts 3+).
	if part >= 3 {
		results = append(results, checkCatalogAF(ctx, doc, catalog, fileSpecs, part)...)
	}

	doc.Logger.Debug("compliance.embedded: embedded file checks complete", "violations", len(results))
	return results
}

// checkEmbeddedSubtypeMIME validates ISO 19005-3 §6.8 t1: each embedded file
// stream shall declare a valid MIME type in its /Subtype. The embedded streams
// are the /F and /UF entries of the file specification's /EF dictionary.
func checkEmbeddedSubtypeMIME(ctx context.Context, doc *document.Document, fs fileSpecEntry) []Violation {
	efDict, err := document.ResolveDict(ctx, doc, fs.dict["EF"])
	if err != nil || efDict == nil {
		return nil
	}

	var results []Violation
	for _, key := range []ast.PdfName{"F", "UF"} {
		streamObj, ok := efDict[key]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, streamObj)
		if err != nil {
			doc.Logger.Debug("compliance.embedded: cannot resolve embedded file stream", "error", err)
			continue
		}
		// Per ISO 32000-1, 7.3.5, the lexer already resolves every #XX escape,
		// so a name written /text#2Fxml arrives here as "text/xml". Decoding
		// #2F a second time would turn /text#232Fxml, whose one correct
		// decoding is the literal "text#2Fxml", back into a valid MIME type and
		// accept a file this clause rejects.
		subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
		if !mimeTypePattern.MatchString(string(subtype)) {
			results = append(results, failf(Rule_19005_3_6_8_t01,
				"%s: embedded file /Subtype %q is not a valid MIME type", fs.label(), subtype)...)
		}
	}
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
		doc.Logger.Debug("compliance.embedded: cannot resolve /Names dict", "error", err)
		return nil, false
	}
	return namesDict, true
}

// fileSpecEntry holds a resolved file specification dict and its object number.
//
// ISO 32000-1, 7.11.3 does not require a file specification to be an indirect
// object. One written inline carries no object number of its own, so objNum is
// zero and container names the indirect object holding it.
type fileSpecEntry struct {
	dict      ast.PdfDict
	objNum    int
	hasEF     bool
	container int
	path      string
}

// label identifies a filespec in a violation message. A direct dictionary is
// named by its container, the only object number a reader can cite.
func (fs fileSpecEntry) label() string {
	if fs.objNum != 0 {
		return fmt.Sprintf("object %d", fs.objNum)
	}
	return fmt.Sprintf("object %d %s", fs.container, fs.path)
}

// collectFileSpecs scans all objects to find file specification dictionaries
// that contain an /EF key (indicating embedded files).
//
// The scan walks the cross-reference map, whose iteration order Go randomises,
// so the result is sorted by object number before returning. Callers emit one
// violation per entry, and an unsorted slice let two embedded files swap places
// between runs of the same binary on the same file: identical findings in a
// different order, which reads as a diff to anything comparing reports.
//
// The cross-reference map holds only indirect objects, so the scan alone cannot
// see a filespec written inline. Annotations carrying a direct /FS are gathered
// separately: pdfjs issue14203.pdf attaches example.tex through one, and every
// clause 6.8 check skipped it. A direct filespec inside an /AF array or an
// EmbeddedFiles name tree stays out of scope, unobserved across 5144 corpus
// files.
func collectFileSpecs(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []fileSpecEntry {
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

		// ISO 19005-3 §6.8 t3/t4 apply to every /Type /Filespec dict,
		// embedded or not, so a plain filespec without /EF can still fail
		// those rules. Callers gate the embedded-only checks on hasEF, which,
		// as veraPDF's containsEF (a knownKey on /EF) reads it, is false for an
		// /EF written as null (ISO 32000-1, 7.3.7): measured on PDF/A-1b, 2b,
		// 3b, 4 and 4f, where a null /EF fired 6.1.11 t1, 6.8 t2 or 6.9 t2.
		hasEF := nonNullKeyPresent(ctx, doc, dict, "EF")

		specs = append(specs, fileSpecEntry{dict: dict, objNum: objNum, hasEF: hasEF})
	}

	specs = append(specs, collectDirectAnnotFileSpecs(ctx, doc, catalog)...)

	slices.SortFunc(specs, func(a, b fileSpecEntry) int {
		if a.objNum != b.objNum {
			return a.objNum - b.objNum
		}
		if a.container != b.container {
			return a.container - b.container
		}
		return strings.Compare(a.path, b.path)
	})

	return specs
}

// collectDirectAnnotFileSpecs gathers file specifications written as a direct
// dictionary in an annotation's /FS.
//
// Only an annotation that is itself an indirect object is considered: a direct
// filespec inside a direct annotation has no object number anywhere on its path
// to cite in a violation, and no corpus file carries that pairing. Each
// annotation is visited once, so an annotation shared by two pages does not
// report its filespec twice.
func collectDirectAnnotFileSpecs(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []fileSpecEntry {
	var specs []fileSpecEntry
	seen := make(map[int]bool)

	for _, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return specs
		}

		annots, err := document.ResolveArray(ctx, doc, page["Annots"])
		if err != nil || annots == nil {
			continue
		}

		for _, annotObj := range annots {
			ref, isRef := annotObj.(ast.PdfIndirectRef)
			if !isRef || seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true

			annotDict, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annotDict == nil {
				continue
			}

			fsDict, direct := annotDict["FS"].(ast.PdfDict)
			if !direct {
				continue
			}
			if typeName, _ := document.ResolveName(ctx, doc, fsDict["Type"]); typeName != "Filespec" {
				continue
			}

			hasEF := nonNullKeyPresent(ctx, doc, fsDict, "EF")
			specs = append(specs, fileSpecEntry{
				dict:      fsDict,
				hasEF:     hasEF,
				container: ref.ObjectNum,
				path:      "/FS",
			})
		}
	}

	return specs
}

// checkFileSpecFUF validates that a file specification dict contains both /F and /UF keys.
func checkFileSpecFUF(_ context.Context, _ *document.Document, fs fileSpecEntry, part int) []Violation {
	_, hasF := fs.dict["F"]
	_, hasUF := fs.dict["UF"]

	if hasF && hasUF {
		return nil
	}

	rule := fileSpecFUFRule(part)
	return failf(rule, "%s: file specification missing F=%v UF=%v", fs.label(), hasF, hasUF)
}

// checkAFRelationship validates that a file specification dict has a valid
// /AFRelationship key. Required for parts 3+ and part 4.
func checkAFRelationship(ctx context.Context, doc *document.Document, fs fileSpecEntry, part int, _ string) []Violation {
	// Parts 2 does not require /AFRelationship.
	if part < 3 {
		return nil
	}

	afObj, hasAF := fs.dict["AFRelationship"]
	if !hasAF {
		rule := afRelationshipRule(part)
		return failf(rule, "%s: file specification missing AFRelationship key", fs.label())
	}

	// Validate the value of /AFRelationship.
	afName, err := document.ResolveName(ctx, doc, afObj)
	if err != nil || afName == "" {
		doc.Logger.Debug("compliance.embedded: /AFRelationship is not a valid name",
			"object", fs.objNum, "error", err)
		rule := afRelationshipRule(part)
		return failf(rule, "%s: AFRelationship is not a valid name", fs.label())
	}

	if !validAFRelationships[afName] {
		rule := afRelationshipRule(part)
		return failf(rule, "%s: AFRelationship value %q is not valid", fs.label(), afName)
	}

	return nil
}

func fileSpecFUFRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_8_t02, Rule_19005_3_6_8_t02, Rule_19005_4_6_9_t02)
}

func afRelationshipRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule{}, Rule_19005_3_6_8_t03, Rule_19005_4_6_9_t04)
}

// checkCatalogAF validates §6.8 t4: for PDF/A-3+, each file specification dict
// with an /EF key must be referenced from the catalog's /AF array. This ensures
// the associated files are properly declared at the document level.
func checkCatalogAF(ctx context.Context, doc *document.Document, catalog ast.PdfDict, fileSpecs []fileSpecEntry, part int) []Violation {
	if len(fileSpecs) == 0 {
		return nil
	}

	// PDF/A-3 §6.8 t4 covers every file specification dictionary, with or
	// without /EF or /AFRelationship, so a plain filespec carrying neither
	// must still be an associated file. PDF/A-4 keeps the narrower scope:
	// embedded specs that declare /AFRelationship.
	var associatedSpecs []fileSpecEntry
	for _, fs := range fileSpecs {
		if part == 3 {
			associatedSpecs = append(associatedSpecs, fs)
			continue
		}
		_, hasAFR := fs.dict["AFRelationship"]
		if fs.hasEF && hasAFR {
			associatedSpecs = append(associatedSpecs, fs)
		}
	}
	if len(associatedSpecs) == 0 {
		return nil
	}

	rule := catalogAFRule(part)

	// Collect all /AF references from catalog, pages, and annotations.
	// The /AF array can appear on any of these per the spec.
	afRefs := make(map[int]bool)
	// fsAnnotRefs holds filespecs reached only through an annotation /FS (e.g. a
	// Link's remote go-to). For §6.8 t4 association, an annotation /FS reference
	// counts for a filespec WITHOUT /EF (an external file) but NOT for an
	// embedded one: a filespec carrying /EF is data the document owns, so it
	// must appear in a real /AF array, not merely be reachable via an
	// annotation.
	fsAnnotRefs := make(map[int]bool)
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
					// (e.g., a Link's remote go-to or a FileAttachment). This
					// satisfies association only for an external (non-/EF)
					// filespec; an embedded one still needs a real /AF entry.
					if fsObj, hasFS := annotDict["FS"]; hasFS {
						if ref, isRef := fsObj.(ast.PdfIndirectRef); isRef {
							fsAnnotRefs[ref.ObjectNum] = true
						}
					}

					// RichMedia annotations reference file specs through
					// /RichMediaContent/Assets (a name tree).
					collectRichMediaFileSpecRefs(ctx, doc, annotDict, afRefs)
				}
			}
		}

		// XObject resources (Image and Form XObjects can have /AF).
		res := resolveInheritedResources(ctx, doc, page)
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

	// Per ISO 32000-2 §14.13, /AF can also appear on structure elements (e.g. a
	// Figure or formula associating a source data file). A spec associated only
	// via a struct element's /AF therefore counts as associated, otherwise an
	// accessible embedded file (e.g. a heading's /AF) would be a false positive.
	if structRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"]); err == nil && structRoot != nil {
		walkStructureElements(ctx, doc, structRoot, func(elem ast.PdfDict, _ string) {
			collectAFRefs(ctx, doc, elem["AF"], afRefs)
		})
	}

	var results []Violation
	for _, fs := range associatedSpecs {
		associated := fs.objNum != 0 && afRefs[fs.objNum]
		// An external (non-embedded) filespec may be associated through an
		// annotation /FS reference; an embedded one (/EF) must be in /AF. A
		// direct /FS is that same annotation reference, written inline.
		if !fs.hasEF && (fs.objNum == 0 || fsAnnotRefs[fs.objNum]) {
			associated = true
		}
		if !associated {
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("%s: file specification not referenced in any /AF array", fs.label()),
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

	// /Assets name tree.
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

	// /Configurations → instances → /Asset.
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
			doc.Logger.Debug("compliance.embedded: cannot resolve config instances array", "error", err)
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
	// Only PDF/A-3 §6.8 t4 requires associated-file linkage. PDF/A-4 §6.9
	// t4 is the AFRelationship-presence rule and carries no association
	// requirement, so a filespec with /AFRelationship and no /AF array
	// passes PDF/A-4f.
	return ruleByPart(part, Rule{}, Rule{}, Rule_19005_3_6_8_t04, Rule{})
}

// embeddedDepthKey is a context key for tracking recursive embedded PDF validation depth.
type embeddedDepthKey struct{}

// maxEmbeddedDepth is the maximum recursion depth for embedded PDF validation.
// An embedded PDF containing another embedded PDF would be depth 2.
const maxEmbeddedDepth = 2

// maxEmbeddedFileSize is the maximum size of a decoded embedded file stream
// that will be validated (50 MB). Larger files are skipped to prevent OOM.
const maxEmbeddedFileSize = 50 * 1024 * 1024

// checkEmbeddedFileCompliance validates that all embedded files are
// themselves PDF/A-compliant. Two rules share the machinery: ISO
// 19005-4 §6.9 t3 (embedded files shall comply with 19005-1, -2 or -4)
// and ISO 19005-2 §6.8 t5 (embedded files in a PDF/A-2 document shall
// comply with 19005-1 or -2). rule selects the violation and accepted
// lists the profiles whose pass clears a file.
//
// Safety limits prevent abuse: maximum 50 MB per file, maximum recursion depth
// of 2, and context cancellation is checked per file.
func checkEmbeddedFileCompliance(ctx context.Context, doc *document.Document, fileSpecs []fileSpecEntry, rule Rule, accepted []models.PDFAProfile) []Violation {
	if len(fileSpecs) == 0 {
		return nil
	}

	// Check recursion depth.
	depth, _ := ctx.Value(embeddedDepthKey{}).(int)
	if depth >= maxEmbeddedDepth {
		doc.Logger.Debug("compliance.embedded: max recursion depth reached, skipping embedded file compliance check",
			"depth", depth)
		return nil
	}

	childCtx := context.WithValue(ctx, embeddedDepthKey{}, depth+1)

	var results []Violation

	for _, fs := range fileSpecs {
		if ctx.Err() != nil {
			return results
		}

		violations := validateEmbeddedFileSpec(childCtx, doc, fs, rule, accepted)
		results = append(results, violations...)
	}

	return results
}

// validateEmbeddedFileSpec extracts and validates a single embedded file stream
// from a file specification dictionary.
func validateEmbeddedFileSpec(ctx context.Context, doc *document.Document, fs fileSpecEntry, rule Rule, accepted []models.PDFAProfile) []Violation {
	efObj, ok := fs.dict["EF"]
	if !ok {
		return nil
	}

	efDict, err := document.ResolveDict(ctx, doc, efObj)
	if err != nil || efDict == nil {
		return nil
	}

	// The /EF dict maps /F, /UF, etc. to embedded file streams.
	// Check /F first (most common), then /UF.
	var streamObj ast.PdfObject
	if f, ok := efDict["F"]; ok {
		streamObj = f
	} else if uf, ok := efDict["UF"]; ok {
		streamObj = uf
	}

	if streamObj == nil {
		return nil
	}

	stream, err := document.ResolveStream(ctx, doc, streamObj)
	if err != nil || stream.Dict == nil {
		return nil
	}

	// Decode the stream.
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.embedded: cannot decode embedded stream",
			"object", fs.objNum, "error", err)
		return nil
	}

	if len(data) > maxEmbeddedFileSize {
		doc.Logger.Debug("compliance.embedded: embedded file too large, skipping",
			"object", fs.objNum, "size", len(data))
		return nil
	}

	// Non-PDF embedded files cannot be PDF/A-compliant.
	if !bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")) {
		return failf(rule,
			"object %d: embedded file is not a PDF document", fs.objNum)
	}

	doc.Logger.Debug("compliance.embedded: validating embedded PDF",
		"object", fs.objNum, "size", len(data))

	// Parse the embedded PDF.
	embDoc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		doc.Logger.Debug("compliance.embedded: cannot parse embedded PDF",
			"object", fs.objNum, "error", err)
		// An unparseable PDF cannot be PDF/A-compliant.
		return failf(rule,
			"object %d: embedded file is not a valid PDF", fs.objNum)
	}

	for _, profile := range accepted {
		if ctx.Err() != nil {
			return nil
		}

		violations, err := ValidatePDFA(ctx, embDoc, profile)
		if err != nil {
			doc.Logger.Debug("compliance.embedded: validation error for embedded PDF",
				"object", fs.objNum, "profile", profile, "error", err)
			continue
		}

		if len(violations) == 0 {
			doc.Logger.Debug("compliance.embedded: embedded PDF passes",
				"object", fs.objNum, "profile", profile)
			return nil // Passes at least one profile, compliant.
		}

		doc.Logger.Debug("compliance.embedded: embedded PDF fails profile",
			"object", fs.objNum, "profile", profile, "violations", len(violations))
	}

	// Failed all accepted profiles.
	return failf(rule,
		"object %d: embedded file is not compliant with an accepted PDF/A part", fs.objNum)
}
