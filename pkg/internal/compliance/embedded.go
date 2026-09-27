package compliance

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
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

// IsValidMIMEType reports whether s is the media type ISO 19005-3, 6.8 t1 and
// ISO 19005-4, 6.9 t1 require of an embedded file's /Subtype, as the
// validator judges it: a "type/subtype" pair of RFC 2045 tokens, with no
// parameter.
func IsValidMIMEType(s string) bool {
	return mimeTypePattern.MatchString(s)
}

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
//
// # Corpus evidence
//
//   - All corpora: no file gives evidence that PDF/A-4 §6.9 t4 reaches a file
//     specification without /EF, so that check stays scoped to embedded ones.
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

		// Check /F and /UF keys (ISO 19005-2/3, 6.8 t2 and ISO 19005-4, 6.9
		// t2; the keys are those of ISO 32000-1, 7.11.3, Table 44). The rule
		// scopes to "the file specification dictionary for an embedded
		// file", so a plain filespec without /EF is out of scope and not
		// flagged.
		if fs.hasEF {
			results = append(results, checkFileSpecFUF(ctx, doc, fs, part)...)
		}

		// Check /AFRelationship. PDF/A-3 §6.8 t3 applies to every file
		// specification dictionary; PDF/A-4 §6.9 t4 stays scoped to
		// embedded ones.
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
		if defect := embeddedSubtypeDefect(ctx, doc, stream.Dict); defect != "" {
			results = append(results, failf(Rule_19005_3_6_8_t01, "%s: embedded file %s", fs.label(), defect)...)
		}
	}
	return results
}

// embeddedSubtypeDefect states what keeps the /Subtype of the embedded file
// stream dict from being the valid MIME type ISO 19005-3, 6.8 t1 and ISO
// 19005-4, 6.9 t1 require, or returns "" when it is one.
//
// The statement names the key's own defect: an absent or null entry (ISO
// 32000-1, 7.3.7) is missing, and a present name is quoted without the solidus
// [ast.PdfName.String] writes. Formatting the name with %q went through String,
// so a missing /Subtype, which resolves to the empty name, printed as "/", and
// a name with a slash inside it read as a malformed type.
func embeddedSubtypeDefect(ctx context.Context, doc *document.Document, dict ast.PdfDict) string {
	resolved, err := document.ResolveObject(ctx, doc, dict["Subtype"])
	if err == nil && isNullObject(resolved) {
		return "/Subtype is missing"
	}
	subtype, isName := resolved.(ast.PdfName)
	if err != nil || !isName {
		return "/Subtype is not a name"
	}
	// Per ISO 32000-1, 7.3.5, the lexer already resolves every #XX escape, so a
	// name written /text#2Fxml arrives here as "text/xml". Decoding #2F a second
	// time would turn /text#232Fxml, whose one correct decoding is the literal
	// "text#2Fxml", back into a valid MIME type and accept a file this clause
	// rejects.
	if mimeTypePattern.MatchString(string(subtype)) {
		return ""
	}
	return fmt.Sprintf("/Subtype %q is not a valid MIME type", string(subtype))
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
// ISO 32000-1, 7.11.3 does not require a file specification to be an indirect
// object, and the cross-reference map holds only indirect ones, so the sweep
// also walks the direct values of every object it resolves, a stream's
// dictionary included, and gathers each /Type /Filespec dictionary written
// inline there ([directFileSpecsIn]): an annotation's /FS or /AF entry, the
// annotation itself written inline in a page's /Annots, a reference XObject's
// /Ref, an /OPI dictionary's /F, a catalog or page /AF entry. veraPDF judges a
// file specification wherever it sits, as it walks every value of every
// object, and names an inline one by the object holding it and its path there,
// as [fileSpecEntry.label] does.
//
// The sweep identifies a candidate by /Type alone, which is all it can do over
// every object in a file, so it misses one whose /Type is wrong. The
// /Names /EmbeddedFiles tree names its values regardless, and
// [collectNameTreeFileSpecs] gathers those by position. veraPDF sees no file
// specification in an inline dictionary without /Type either, measured on a
// FileAttachment /FS with /EF and a /Subtype-less embedded file, which fails
// no clause 6.8 test.
//
// # Corpus evidence
//
//   - pdfjs issue14203.pdf: attaches example.tex through an annotation's
//     direct /FS, and every clause 6.8 check skipped it.
//   - litho complex_1b.pdf and complex_4.pdf: a reference XObject names its
//     target file through a /Ref /F written inline, and veraPDF reports it
//     under ISO 19005-3, 6.8 t3 and t4 where litho did not.
//   - govdocs thread_400_400002.pdf: 42 /OPI dictionaries name their image
//     file through an inline /F, and veraPDF reports each under ISO 19005-3,
//     6.8 t3 and t4 where litho reported none.
//   - All corpora, the 298 files that hold a file specification, at PDF/A-1b,
//     2b, 3b, 4, 4f and PDF/UA-1: those three files are the only counts the
//     wider walk moves, and it costs no measurable time.
func collectFileSpecs(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []fileSpecEntry {
	var specs []fileSpecEntry

	for objNum, entry := range sweptXRefEntries(doc) {
		if ctx.Err() != nil {
			return specs
		}
		if entry.Free {
			continue
		}

		obj, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			continue
		}
		// A stream is never a file specification, but its dictionary can hold
		// one inline, as a reference XObject's /Ref does.
		if stream, ok := obj.(ast.PdfStream); ok {
			specs = append(specs, directFileSpecsIn(ctx, doc, stream.Dict, objNum, "", 0)...)
			continue
		}
		specs = append(specs, directFileSpecsIn(ctx, doc, obj, objNum, "", 0)...)

		dict, ok := obj.(ast.PdfDict)
		if !ok || dict == nil {
			continue
		}
		if typeName, _ := document.ResolveName(ctx, doc, dict["Type"]); typeName != "Filespec" {
			continue
		}

		// ISO 19005-3 §6.8 t3/t4 apply to every /Type /Filespec dict,
		// embedded or not, so a plain filespec without /EF can still fail
		// those rules. Callers gate the embedded-only checks on hasEF, which,
		// as veraPDF's containsEF (a knownKey on /EF) reads it, is false for an
		// /EF written as null (ISO 32000-1, 7.3.7): measured on PDF/A-1b, 2b,
		// 3b, 4 and 4f, where a null /EF fired 6.1.11 t1, 6.8 t2 or 6.9 t2.
		hasEF := knownKey(dict, "EF")

		specs = append(specs, fileSpecEntry{dict: dict, objNum: objNum, hasEF: hasEF})
	}

	gathered := make(map[int]bool, len(specs))
	for _, fs := range specs {
		if fs.objNum != 0 {
			gathered[fs.objNum] = true
		}
	}
	specs = append(specs, collectNameTreeFileSpecs(ctx, doc, catalog, gathered)...)

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

// collectNameTreeFileSpecs gathers the file specifications the catalog's
// /Names /EmbeddedFiles tree names, skipping any the caller already holds.
//
// ISO 32000-1, 7.11.4 defines that tree as mapping name strings to file
// specification dictionaries, so a dictionary it names is one whatever its
// /Type says, and position identifies it where the sweep's /Type test cannot.
// Table 44 requires /Type on a filespec carrying /EF, so a file writing
// anything else is malformed: reading its attachments anyway reports the
// defects they carry instead of hiding them behind the one key the producer got
// wrong.
//
// gathered carries the object numbers the sweep and the annotation walk already
// returned. A spec both reach would otherwise be stated twice, which is the
// double-reporting [TestEmbeddedFilesReportedOncePerObject] pins.
//
// # Corpus evidence
//
//   - pdfjs issue9367.pdf: names three specs as /Type /F, and veraPDF reports
//     every one under 6.1.11 t1, 6.8 t5, 6.8 t1/t3/t4 and 6.9 t1/t3/t4 across
//     parts 1 to 4 while litho saw none of them.
func collectNameTreeFileSpecs(ctx context.Context, doc *document.Document, catalog ast.PdfDict, gathered map[int]bool) []fileSpecEntry {
	namesDict, ok := resolveNamesDict(ctx, doc, catalog)
	if !ok {
		return nil
	}
	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil
	}

	refs := make(map[int]bool)
	collectNameTreeSpecRefs(ctx, doc, efObj, refs, make(map[int]bool), 0)

	// The tree walk fills a map, whose iteration order Go randomises, so the
	// object numbers are ordered before resolving: the caller sorts the whole
	// slice afterwards, but an unstable order here would still let two entries
	// carrying the same number swap places between runs.
	nums := make([]int, 0, len(refs))
	for objNum := range refs {
		if !gathered[objNum] {
			nums = append(nums, objNum)
		}
	}
	slices.Sort(nums)

	specs := make([]fileSpecEntry, 0, len(nums))
	for _, objNum := range nums {
		if ctx.Err() != nil {
			return specs
		}
		dict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || dict == nil {
			continue
		}
		specs = append(specs, fileSpecEntry{dict: dict, objNum: objNum, hasEF: knownKey(dict, "EF")})
	}
	return specs
}

// maxInlineFileSpecDepth bounds the descent into the direct values of one
// object while looking for file specifications written inline. Indirect values
// are visited by the outer sweep, so this recursion only walks the direct
// values of a single object and cannot cycle. This is an anti-DoS guard, not
// spec-mandated: a real object nests a handful of levels, the deepest observed
// being a Rendition action reaching its media clip's filespec at /A/R/C/D of
// an annotation, and an inline annotation adds two levels above it, so 16
// leaves genuine documents untouched while capping a crafted one.
const maxInlineFileSpecDepth = 16

// directFileSpecsIn returns every /Type /Filespec dictionary written directly
// inside obj, the value of the indirect object container, at any depth below
// its top. An object reaches a filespec by more routes than an annotation's
// /FS: a Screen annotation's Rendition action holds its media clip's data at
// /A/R/C/D (ISO 32000-1 §13.2.3), an /AF array lists associated files (ISO
// 32000-2, 14.13), and PDF/A-1 §6.1.11 t1 forbids the /EF key on a file
// specification wherever it sits.
//
// Indirect values are skipped: the caller's sweep over the cross-reference table
// already visits those, and following them here would report the same
// dictionary twice.
//
// # Corpus evidence
//
//   - verapdf: five test-suite files embed a video through a Rendition
//     action's media clip, and veraPDF reports every one; litho saw only /FS
//     and passed them.
func directFileSpecsIn(ctx context.Context, doc *document.Document, obj ast.PdfObject, container int, path string, depth int) []fileSpecEntry {
	if depth > maxInlineFileSpecDepth {
		return nil
	}

	var specs []fileSpecEntry
	switch v := obj.(type) {
	case ast.PdfDict:
		if ctx.Err() != nil {
			return nil
		}
		if typeName, _ := document.ResolveName(ctx, doc, v["Type"]); typeName == "Filespec" && path != "" {
			specs = append(specs, fileSpecEntry{
				dict:      v,
				hasEF:     knownKey(v, "EF"),
				container: container,
				path:      path,
			})
		}
		for _, key := range sortedNames(v) {
			specs = append(specs, directFileSpecsIn(ctx, doc, v[key], container, path+"/"+string(key), depth+1)...)
		}
	case ast.PdfArray:
		if ctx.Err() != nil {
			return nil
		}
		for i, item := range v {
			specs = append(specs, directFileSpecsIn(ctx, doc, item, container, fmt.Sprintf("%s[%d]", path, i), depth+1)...)
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
			"filespec", fs.label(), "error", err)
		rule := afRelationshipRule(part)
		return failf(rule, "%s: AFRelationship is not a valid name", fs.label())
	}

	if !validAFRelationships[afName] {
		rule := afRelationshipRule(part)
		return failf(rule, "%s: AFRelationship value %q is not valid", fs.label(), string(afName))
	}

	return nil
}

func fileSpecFUFRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_8_t02, Rule_19005_3_6_8_t02, Rule_19005_4_6_9_t02)
}

func afRelationshipRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule{}, Rule_19005_3_6_8_t03, Rule_19005_4_6_9_t04)
}

// checkCatalogAF validates ISO 19005-3, 6.8 t4: every file specification
// dictionary of a PDF/A-3 file is an associated file, one an /AF array lists
// on the catalog, a page, an annotation, an XObject or a structure element (ISO
// 32000-2, 14.13).
//
// A reference that is not an /AF entry does not associate, whether the file is
// embedded or external: an annotation's /FS, a remote go-to action's /F and a
// RichMedia /Assets tree name the file without stating its relationship to
// the document, which is what /AFRelationship on an associated file records.
// veraPDF's isAssociatedFile reads the same way: it holds only the object keys
// that /AF arrays list (FileSpecificationKeysHelper), and fails the rule on
// each of those routes, measured on a crafted file per route.
//
// # Corpus evidence
//
//   - verapdf PDF_A-4e__veraPDF_test_suite_6-3-2-t01-fail-b.pdf: its two /EF
//     filespecs are reached only through a /RichMedia annotation's /Assets
//     tree and carry no /AF anywhere in the file, and veraPDF fails
//     ISO 19005-3, 6.8 t4 (isAssociatedFile == true) on both.
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-3-2-t01-fail-p.pdf and
//     6-3-3-t01-fail-p.pdf: a FileAttachment /FS names an external filespec
//     no /AF lists, and veraPDF fails the rule at PDF/A-3a, 3b and 3u, where
//     litho called the annotation reference an association and passed them.
//   - All corpora: dropping that leniency moves no other file at PDF/A-3b.
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
	collectAFRefs(ctx, doc, catalog["AF"], afRefs)

	// Also collect from pages, their annotations, and XObject resources.
	pages := collectPageObjects(ctx, doc, catalog)
	for i := range pages {
		page := pages[i].dict
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
				}
			}
		}

		// XObject resources (Image and Form XObjects can have /AF).
		res := pages[i].effectiveResources(ctx, doc)
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
		// Only an /AF array associates, and it lists indirect objects: a
		// filespec written inline has no object number an /AF entry could name,
		// and veraPDF fails one even where an /AF array lists it inline,
		// measured on an inline /AF entry of a Link annotation.
		if fs.objNum == 0 || !afRefs[fs.objNum] {
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
// An embedded PDF containing another embedded PDF would be depth 2. Anti-DoS
// guard, not spec-mandated: ISO 19005-2, 6.8 and ISO 19005-4, 6.9 require an
// embedded file to conform to PDF/A itself, which carries the requirement down
// every level of nesting, so a file could otherwise chain validation runs
// without end.
//
// The embedded files of a PDF at depth 2 are not validated and raise no
// violation. Two levels leave one level of headroom over the nesting real
// documents use.
//
// # Corpus evidence
//
//   - All corpora: 38 of the 5163 files that open embed a PDF (23 verapdf,
//     11 facturx, 4 pdfjs), and none of the 41 embedded PDFs embeds another,
//     so the deepest nesting is 1.
const maxEmbeddedDepth = 2

// embeddedHeaderScanLen is how many leading bytes of an embedded file are
// searched for the "%PDF-" header. ISO 32000-1, 7.5.2 puts the header on the
// first line; the window is the one document.NewDocument scans
// (headerScanLimit in package document) to tolerate a leading prefix, so a
// file judged a PDF here is one the parser recognises by its header.
const embeddedHeaderScanLen = 1024

// embeddedReadChunk is how much of an embedded file [readEmbeddedFile] reads
// between two context checks, the chunk the filter pipeline decodes between
// its own.
const embeddedReadChunk = 1 << 20 // 1 MB

// pdfHeaderMarker opens the header line of a PDF file (ISO 32000-1, 7.5.2).
const pdfHeaderMarker = "%PDF-"

// readEmbeddedFile reads a decoded embedded file from r and reports whether it
// is a PDF, deciding that from its first [embeddedHeaderScanLen] bytes alone.
//
// A file that is not a PDF is read no further than its header, and data is
// nil. A PDF is read in full up to limit bytes; one longer than that sets
// tooLarge and returns no data, having read limit+1 bytes. The whole file used
// to be decoded before its size was known, so the cap exempted a large
// attachment from the PDF/A check altogether, even one whose first bytes
// showed it was not a PDF.
//
// A read error once some bytes have arrived ends the file there and is logged
// at Debug: the filter pipeline tolerates a damaged stream the same way when
// it decodes one eagerly, so a truncated attachment is judged on what decodes.
// An error before any byte arrives, or a cancelled ctx, is returned.
func readEmbeddedFile(ctx context.Context, r io.Reader, limit int64, logger *slog.Logger) (data []byte, isPDF, tooLarge bool, err error) {
	var buf bytes.Buffer
	ended, err := copyEmbedded(ctx, &buf, r, embeddedHeaderScanLen, logger)
	if err != nil {
		return nil, false, false, err
	}
	if !bytes.Contains(buf.Bytes(), []byte(pdfHeaderMarker)) {
		return nil, false, false, nil
	}
	if !ended {
		if _, err := copyEmbedded(ctx, &buf, r, limit+1-int64(buf.Len()), logger); err != nil {
			return nil, true, false, err
		}
	}
	if int64(buf.Len()) > limit {
		return nil, true, true, nil
	}
	return buf.Bytes(), true, false, nil
}

// copyEmbedded copies up to n bytes of r into buf in [embeddedReadChunk]
// chunks, checking ctx between them. ended reports that r has no more data:
// it reached its end, or failed after buf received data, which
// [readEmbeddedFile] treats as the end of a damaged stream.
func copyEmbedded(ctx context.Context, buf *bytes.Buffer, r io.Reader, n int64, logger *slog.Logger) (ended bool, err error) {
	for n > 0 {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		copied, err := io.CopyN(buf, r, min(n, embeddedReadChunk))
		n -= copied
		switch {
		case err == io.EOF:
			return true, nil
		case err != nil && buf.Len() > 0:
			logger.Debug("compliance.embedded: embedded stream ends in a read error, judging the bytes before it",
				"bytes", buf.Len(), "error", err)
			return true, nil
		case err != nil:
			return true, err
		}
	}
	return false, nil
}

// checkEmbeddedFileCompliance validates that all embedded files are
// themselves PDF/A-compliant. Two rules share the machinery: ISO
// 19005-4 §6.9 t3 (embedded files shall comply with 19005-1, -2 or -4)
// and ISO 19005-2 §6.8 t5 (embedded files in a PDF/A-2 document shall
// comply with 19005-1 or -2). rule selects the violation and accepted
// lists the profiles whose pass clears a file.
//
// Safety limits prevent abuse: the document's embedded-file budget
// (limits.Limits.MaxEmbeddedFileBytes) per file, [maxEmbeddedDepth] levels
// of nesting, and context cancellation is checked per file. A larger embedded
// PDF is not validated and raises no violation, so a crafted attachment cannot
// make validation hold a huge document in memory and parse it; the budget
// does not spare a large file that is not a PDF, since [readEmbeddedFile]
// judges that from its header alone.
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

	rc, err := doc.NewStreamReader(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.embedded: cannot decode embedded stream",
			"filespec", fs.label(), "error", err)
		return nil
	}
	lim := doc.Limits()
	data, isPDF, tooLarge, err := readEmbeddedFile(ctx, rc, lim.MaxEmbeddedFileBytes, doc.Logger)
	if closeErr := rc.Close(); closeErr != nil {
		doc.Logger.Debug("compliance.embedded: closing embedded stream",
			"filespec", fs.label(), "error", closeErr)
	}
	if err != nil {
		doc.Logger.Debug("compliance.embedded: cannot decode embedded stream",
			"filespec", fs.label(), "error", err)
		return nil
	}

	// ISO 19005-2, 6.8 and ISO 19005-4, 6.9 require an embedded file to
	// conform to an accepted PDF/A part, which a file that is not a PDF
	// cannot, whatever its size.
	if !isPDF {
		return failf(rule,
			"%s: embedded file is not a PDF document", fs.label())
	}
	if tooLarge {
		doc.Logger.Debug("compliance.embedded: embedded PDF too large, skipping",
			"filespec", fs.label(), "limit", lim.MaxEmbeddedFileBytes)
		return nil
	}

	doc.Logger.Debug("compliance.embedded: validating embedded PDF",
		"filespec", fs.label(), "size", len(data))

	// Parse the embedded PDF under the budgets of the document that carries
	// it: a caller who lowered them for the outer file meant them for what it
	// embeds too.
	embDoc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, document.WithLimits(lim))
	if err != nil {
		doc.Logger.Debug("compliance.embedded: cannot parse embedded PDF",
			"filespec", fs.label(), "error", err)
		// An unparseable PDF cannot be PDF/A-compliant.
		return failf(rule,
			"%s: embedded file is not a valid PDF", fs.label())
	}

	for _, profile := range accepted {
		if ctx.Err() != nil {
			return nil
		}

		violations, err := ValidatePDFA(ctx, embDoc, profile)
		if err != nil {
			doc.Logger.Debug("compliance.embedded: validation error for embedded PDF",
				"filespec", fs.label(), "profile", profile, "error", err)
			continue
		}

		if len(violations) == 0 {
			doc.Logger.Debug("compliance.embedded: embedded PDF passes",
				"filespec", fs.label(), "profile", profile)
			return nil // Passes at least one profile, compliant.
		}

		doc.Logger.Debug("compliance.embedded: embedded PDF fails profile",
			"filespec", fs.label(), "profile", profile, "violations", len(violations))
	}

	// Failed all accepted profiles.
	return failf(rule,
		"%s: embedded file is not compliant with an accepted PDF/A part", fs.label())
}
