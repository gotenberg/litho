package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchCatalogCleanup(v compliance.Violation) bool {
	if (v.Rule.Clause == "6.1.3" && v.Rule.TestNumber == 2) || // Encrypt in trailer
		clausePrefix(v, "6.4.2") || // NeedsRendering catalog key
		clausePrefix(v, "6.10") || // OCG config
		clausePrefix(v, "6.11") || // AlternatePresentations
		clausePrefix(v, "6.1.11") { // Perms dict (PDF/A-1)
		return true
	}
	// One requirement, four spellings. "Each optional content configuration
	// dictionary ... shall contain the Name key" is ISO 19005-4 6.10 (claimed
	// by the prefix above), ISO 19005-2 and 19005-3 6.9, ISO 14289-1 7.10 and
	// ISO 14289-2 8.7. Only the part-4 spelling reached [fixOCGConfigs], so the
	// same file was repaired as PDF/A-4 and left flagged as PDF/A-2b.
	//
	// Test 1 only: [fixOCGConfigs] adds a missing /Name and implements neither
	// the uniqueness of test 2, the /Order array of test 3, nor the /AS ban, so
	// claiming those would report them handled while they persist.
	//
	// Exact specification and clause, never clausePrefix: ISO 19005-1 spells an
	// AcroForm NeedAppearances rule 6.9 and ISO 19005-4 an embedded-file rule
	// 6.9, and the prefix "7.1" already swallows 7.10 in [matchXMP].
	//
	// Each arm MATCHES and falls through; none of them returns a negative.
	// Written as `return v.Rule.Clause == "6.9"` this claimed the OCG rule and
	// silently UN-claimed every other test-1 violation in the same
	// specification, because the false short-circuited the 6.1.12 Perms and
	// Signature-Reference block below. That cost 12 corpus cells, and the
	// count-based gate could not see it: 46 newly matched cells hid 12 newly
	// broken ones behind a net improvement.
	if v.Rule.TestNumber == 1 {
		switch v.Rule.Specification {
		case "ISO_19005_2", "ISO_19005_3":
			if v.Rule.Clause == "6.9" {
				return true
			}
		case "ISO_14289_1":
			if v.Rule.Clause == "7.10" {
				return true
			}
		case "ISO_14289_2":
			if v.Rule.Clause == "8.7" {
				return true
			}
		}
	}
	// PDF/A-2 §6.1.12 covers Perms and Signature Reference restrictions.
	if clausePrefix(v, "6.1.12") &&
		(contextContains(v, "Perms dictionary") ||
			contextContains(v, "Signature Reference") ||
			descriptionContains(v, "permissions dictionary") ||
			descriptionContains(v, "Signature References")) {
		return true
	}
	return false
}

func fixCatalogCleanup(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	var fixes []string

	// 6.1.3: Remove /Encrypt from trailer to strip encryption on rewrite.
	if _, ok := doc.Index.Trailer["Encrypt"]; ok {
		newTrailer := make(ast.PdfDict, len(doc.Index.Trailer))
		maps.Copy(newTrailer, doc.Index.Trailer)
		delete(newTrailer, "Encrypt")
		doc.Index.Trailer = newTrailer
		fixes = append(fixes, "removed /Encrypt from trailer")
	}

	// 6.10: Ensure OCG config dicts have /Name.
	if ocpObj, ok := catalogDict["OCProperties"]; ok {
		ocFixes := fixOCGConfigs(ctx, alloc, doc, catalogDict, catalogRef, ocpObj)
		fixes = append(fixes, ocFixes...)
		// Re-read catalog in case it was updated.
		catalogDict = resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
		if catalogDict == nil {
			return fixes, nil
		}
	}

	// 6.11: Remove /AlternatePresentations from catalog /Names.
	if namesObj, ok := catalogDict["Names"]; ok {
		apFixes := fixAlternatePresentations(ctx, alloc, doc, catalogDict, catalogRef, namesObj)
		fixes = append(fixes, apFixes...)
		// Re-read catalog in case it was updated.
		catalogDict = resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
		if catalogDict == nil {
			return fixes, nil
		}
	}

	// 6.1.11: Remove non-permitted keys from /Perms dict (keep only /UR3 and /DocMDP).
	if permsObj, ok := catalogDict["Perms"]; ok {
		pFixes := fixPermsDict(ctx, alloc, doc, catalogDict, catalogRef, permsObj)
		fixes = append(fixes, pFixes...)
		if sFixes := fixSignatureReferences(ctx, alloc, doc, opts, permsObj); len(sFixes) > 0 {
			fixes = append(fixes, sFixes...)
		}
		// Re-read the catalog in case fixPermsDict rewrote it (an inline /Perms
		// dict is rewritten through the catalog object). A later catalog edit
		// must build on that copy, not the stale one.
		catalogDict = resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
		if catalogDict == nil {
			return fixes, nil
		}
	}

	// 6.4.2: Remove the forbidden /NeedsRendering key from the catalog.
	if _, ok := catalogDict["NeedsRendering"]; ok {
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		delete(newCatalog, "NeedsRendering")
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
		// Keep the local copy current: the repair below may rewrite the catalog
		// too, and copying from a stale dict would restore the key just removed.
		catalogDict = newCatalog
		fixes = append(fixes, "removed /NeedsRendering from catalog")
	}

	// 6.1.11 (PDF/A-1 only): the embedded-file constructs that profile forbids
	// outright. Runs last so it builds on whatever the branches above wrote.
	fixes = append(fixes, removePDFA1EmbeddedFiles(ctx, alloc, doc, opts, profile, catalogDict, catalogRef)...)

	return fixes, nil
}

// signatureIsSigned reports whether a signature dictionary carries an actual
// signature: a /Contents value together with the four-element /ByteRange that
// fixes which bytes the digest covers (ISO 32000-1, 12.8.1, Table 252). It
// mirrors how [compliance] decides a signature exists before judging its range.
// An entry without both signs nothing, so its reference dictionaries carry no
// integrity to lose.
func signatureIsSigned(ctx context.Context, doc *document.Document, sigDict ast.PdfDict) bool {
	if _, ok := sigDict["Contents"]; !ok {
		return false
	}
	byteRange, err := document.ResolveArray(ctx, doc, sigDict["ByteRange"])
	return err == nil && len(byteRange) == 4
}

// fixSignatureReferences strips DigestLocation, DigestMethod and
// DigestValue from every Reference entry under /Perms /DocMDP and
// /UR3 Signature dicts. ISO 19005-2/4 §6.1.12 test 2 forbids those
// keys on signature-reference dicts.
//
// In standard mode a SIGNED signature is left alone. Its reference
// dictionaries are part of the revision the digest covers, so rewriting one
// moves the /ByteRange and trades §6.1.12 test 2 for §6.4.3 test 1, which
// reports that the declared range no longer matches the signed revision.
// Measured on all three PDF_A-2b__veraPDF_test_suite_6-1-12-t02-fail-*
// fixtures, where the swap kept the violation count at one and so stayed
// invisible to a count-based gate.
//
// Aggressive mode still strips. There the invalidation is intended and
// completes a chain: the strip breaks the coverage, §6.4.3 test 1 fires, and
// [fixSignatureStrip] then removes the now-invalidated signature, resolving
// both rules. Guarding unconditionally severed that chain and cost 18 fixtures
// their fully-fixed verdict in the remediation-aggressive corpus cell, a drop
// the gate could not flag because it compares against a much older baseline.
func fixSignatureReferences(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, permsObj ast.PdfObject) []string {
	permsDict, err := document.ResolveDict(ctx, doc, permsObj)
	if err != nil || permsDict == nil {
		return nil
	}
	var fixes []string
	for _, key := range []ast.PdfName{"DocMDP", "UR3"} {
		sigObj, ok := permsDict[key]
		if !ok {
			continue
		}
		sigRef, _ := sigObj.(ast.PdfIndirectRef)
		sigDict, err := document.ResolveDict(ctx, doc, sigObj)
		if err != nil || sigDict == nil {
			continue
		}
		if opts.PreserveContent && signatureIsSigned(ctx, doc, sigDict) {
			continue
		}
		refsObj, ok := sigDict["Reference"]
		if !ok {
			continue
		}
		refsArr, err := document.ResolveArray(ctx, doc, refsObj)
		if err != nil || len(refsArr) == 0 {
			continue
		}
		newRefs := make(ast.PdfArray, len(refsArr))
		changed := false
		for i, entry := range refsArr {
			entryRef, _ := entry.(ast.PdfIndirectRef)
			entryDict, err := document.ResolveDict(ctx, doc, entry)
			if err != nil || entryDict == nil {
				newRefs[i] = entry
				continue
			}
			patched := make(ast.PdfDict, len(entryDict))
			maps.Copy(patched, entryDict)
			stripped := false
			for _, forbidden := range []ast.PdfName{"DigestLocation", "DigestMethod", "DigestValue"} {
				if _, ok := patched[forbidden]; ok {
					delete(patched, forbidden)
					stripped = true
				}
			}
			if !stripped {
				newRefs[i] = entry
				continue
			}
			if entryRef.ObjectNum > 0 {
				alloc.AllocAt(entryRef.ObjectNum, patched)
				newRefs[i] = entry
			} else {
				newRefs[i] = patched
				changed = true
			}
			fixes = append(fixes, fmt.Sprintf("stripped forbidden digest keys from /%s Reference[%d]", string(key), i))
		}
		if !changed {
			continue
		}
		newSig := make(ast.PdfDict, len(sigDict))
		maps.Copy(newSig, sigDict)
		newSig["Reference"] = newRefs
		if sigRef.ObjectNum > 0 {
			alloc.AllocAt(sigRef.ObjectNum, newSig)
		}
	}
	return fixes
}

// fixOCGConfigs walks /OCProperties /D and /Configs, adding /Name to any
// config dict that lacks it. Per ISO 32000-2, Table 100.
func fixOCGConfigs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, catalogRef ast.PdfIndirectRef, ocpObj ast.PdfObject) []string {
	var ocpDict ast.PdfDict
	var ocpRef ast.PdfIndirectRef
	var ocpIsRef bool

	if ref, ok := ocpObj.(ast.PdfIndirectRef); ok {
		ocpDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		ocpRef = ref
		ocpIsRef = true
	}
	if ocpDict == nil {
		resolved, err := document.ResolveDict(ctx, doc, ocpObj)
		if err != nil || resolved == nil {
			return nil
		}
		ocpDict = resolved
	}

	var fixes []string
	modified := false
	newOCP := make(ast.PdfDict, len(ocpDict))
	maps.Copy(newOCP, ocpDict)

	// Fix /D (default config).
	if dObj, ok := newOCP["D"]; ok {
		replacement, fixed := ensureOCGConfigName(ctx, alloc, doc, dObj, "D")
		if fixed {
			if replacement != nil {
				newOCP["D"] = replacement
			}
			fixes = append(fixes, "added /Name to OCG default config /D")
			modified = true
		}
	}

	// Fix /Configs array entries.
	if configsObj, ok := newOCP["Configs"]; ok {
		arr, err := document.ResolveArray(ctx, doc, configsObj)
		if err != nil || arr == nil {
			return fixes
		}
		newConfigs := make(ast.PdfArray, len(arr))
		copy(newConfigs, arr)
		configsChanged := false
		for i, entry := range arr {
			replacement, fixed := ensureOCGConfigName(ctx, alloc, doc, entry, fmt.Sprintf("Configs[%d]", i))
			if fixed {
				if replacement != nil {
					newConfigs[i] = replacement
					configsChanged = true
				}
				fixes = append(fixes, fmt.Sprintf("added /Name to OCG config Configs[%d]", i))
				modified = true
			}
		}
		if configsChanged {
			newOCP["Configs"] = newConfigs
		}
	}

	if modified {
		if ocpIsRef {
			alloc.AllocAt(ocpRef.ObjectNum, newOCP)
		} else {
			// OCProperties is inline in catalog. Update catalog to point at the new dict.
			newCatalog := make(ast.PdfDict, len(catalogDict))
			maps.Copy(newCatalog, catalogDict)
			newCatalog["OCProperties"] = newOCP
			alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
		}
	}

	return fixes
}

// ensureOCGConfigName adds /Name to an OCG config dict if missing.
// Returns true if a modification was made.
// ensureOCGConfigName gives an optional-content configuration dictionary the
// /Name its profile requires, reporting the replacement the caller must install
// and whether anything changed.
//
// The replacement is returned rather than only written because a configuration
// may be a DIRECT dictionary inside /OCProperties, which no object number
// addresses. Writing only through the allocator dropped that case on the floor
// while still reporting the fix, so the catalog was rewritten with the /Name
// still missing and the next run found the same defect: remediating
// complex_1b.pdf repeatedly grew it by 1125 bytes a pass, forever, and never
// added the /Name.
//
// A nil replacement with a true result means the change went through the
// allocator and the caller has nothing to install.
func ensureOCGConfigName(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, label string) (ast.PdfDict, bool) {
	ref, isRef := obj.(ast.PdfIndirectRef)

	var d ast.PdfDict
	if isRef {
		d = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if d == nil {
		resolved, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || resolved == nil {
			return nil, false
		}
		d = resolved
	}

	if ocgConfigHasName(ctx, doc, d) {
		return nil, false
	}

	newD := make(ast.PdfDict, len(d)+1)
	maps.Copy(newD, d)
	newD["Name"] = ast.PdfString(label)

	if isRef {
		alloc.AllocAt(ref.ObjectNum, newD)
		return nil, true
	}

	return newD, true
}

// ocgConfigHasName reports whether an optional-content configuration already
// carries the /Name its profile requires, mirroring the compliance check that
// raises the violation: the key must be present and resolve to a non-empty
// string.
//
// Testing only for the key left veraPDF's 7.10-t01-fail-a flagged after its
// clause was routed here, because its default configuration carries
// `/Name ()`: present, and empty. The checker calls hasNonEmptyName, so the
// violation was reported while the repair declined.
//
// doc may be nil when a caller repairs a dictionary it already holds, so a
// direct string is read without resolving.
func ocgConfigHasName(ctx context.Context, doc *document.Document, d ast.PdfDict) bool {
	nameObj, ok := d["Name"]
	if !ok {
		return false
	}
	if s, ok := nameObj.(ast.PdfString); ok {
		return string(s) != ""
	}
	if doc == nil {
		return false
	}
	s, err := document.ResolveString(ctx, doc, nameObj)
	return err == nil && string(s) != ""
}

// fixAlternatePresentations removes /AlternatePresentations from catalog /Names.
func fixAlternatePresentations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, catalogRef ast.PdfIndirectRef, namesObj ast.PdfObject) []string {
	var namesDict ast.PdfDict
	var namesRef ast.PdfIndirectRef
	namesIsRef := false

	if ref, ok := namesObj.(ast.PdfIndirectRef); ok {
		namesDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		namesRef = ref
		namesIsRef = true
	}
	if namesDict == nil {
		resolved, err := document.ResolveDict(ctx, doc, namesObj)
		if err != nil || resolved == nil {
			return nil
		}
		namesDict = resolved
	}

	if _, ok := namesDict["AlternatePresentations"]; !ok {
		return nil
	}

	newNames := make(ast.PdfDict, len(namesDict))
	maps.Copy(newNames, namesDict)
	delete(newNames, "AlternatePresentations")

	if namesIsRef {
		alloc.AllocAt(namesRef.ObjectNum, newNames)
	} else {
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["Names"] = newNames
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return []string{"removed /AlternatePresentations from catalog /Names"}
}

// fixPermsDict removes all keys except /UR3 and /DocMDP from the /Perms dict.
// Per ISO 32000-2, 12.8.4, PDF/A restricts the permitted entries.
func fixPermsDict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, catalogRef ast.PdfIndirectRef, permsObj ast.PdfObject) []string {
	var permsDict ast.PdfDict
	var permsRef ast.PdfIndirectRef
	permsIsRef := false

	if ref, ok := permsObj.(ast.PdfIndirectRef); ok {
		permsDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		permsRef = ref
		permsIsRef = true
	}
	if permsDict == nil {
		resolved, err := document.ResolveDict(ctx, doc, permsObj)
		if err != nil || resolved == nil {
			return nil
		}
		permsDict = resolved
	}

	// Collect keys to remove.
	var removed []string
	for k := range permsDict {
		if k == "UR3" || k == "DocMDP" {
			continue
		}
		removed = append(removed, string(k))
	}

	if len(removed) == 0 {
		return nil
	}

	newPerms := make(ast.PdfDict, 2)
	if v, ok := permsDict["UR3"]; ok {
		newPerms["UR3"] = v
	}
	if v, ok := permsDict["DocMDP"]; ok {
		newPerms["DocMDP"] = v
	}

	if permsIsRef {
		alloc.AllocAt(permsRef.ObjectNum, newPerms)
	} else {
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["Perms"] = newPerms
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return []string{"removed non-permitted keys from /Perms dict"}
}
