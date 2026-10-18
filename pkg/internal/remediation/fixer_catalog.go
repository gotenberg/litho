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

// matchCatalogCleanup matches the catalog-level violations [fixCatalogCleanup]
// repairs: the trailer /Encrypt, /NeedsRendering, the optional-content
// configuration rules and the part-1 /OCProperties ban,
// /AlternatePresentations, and the /Perms and signature-reference
// restrictions.
//
// # Corpus evidence
//
//   - litho complex_ua2: carries a /Configs configuration with its own
//     incomplete /Order that an earlier detector never looked at. Claiming
//     test 3 and completing /D alone silenced litho while veraPDF kept
//     reporting: litho corpus disagreement moved from exactly 0 to 10 across
//     two fixtures.
//   - verapdf: the same test 3 false fix gained 8 cells and hid the litho
//     corpus regression.
//   - All corpora: written as `return v.Rule.Clause == "6.9"`, the OCG arm
//     cost 12 corpus cells, and the count-based gate could not see it: 46
//     newly matched cells hid 12 newly broken ones behind a net improvement.
func matchCatalogCleanup(v compliance.Violation) bool {
	if (v.Rule.Clause == "6.1.3" && v.Rule.TestNumber == 2) || // Encrypt in trailer
		clausePrefix(v, "6.4.2") || // NeedsRendering catalog key
		clausePrefix(v, "6.10") || // OCG config (part 4), PresSteps and AlternatePresentations (parts 2, 3)
		clausePrefix(v, "6.11") || // AlternatePresentations
		clausePrefix(v, "6.1.11") { // Perms dict (PDF/A-1)
		return true
	}
	// ISO 19005-1 6.1.13 test 1: the part-1 ban on catalog /OCProperties.
	// Exact specification, clause and test, never a prefix: parts 2 and 3
	// spell their implementation limits at the same clause number.
	if forbiddenOCPropertiesViolation(v.Rule) {
		return true
	}
	// One requirement, several spellings. The optional-content configuration
	// clause is ISO 19005-4 6.10 (claimed by the prefix above), ISO 19005-2 and
	// 19005-3 6.9, ISO 14289-1 7.10 and ISO 14289-2 8.7, and the last of those
	// also reaches a WTPDF run, which reports PDF/UA-2 clauses as its own.
	// Only the part-4 spelling reached fixOCGConfigs, so the same file was
	// repaired as PDF/A-4 and left flagged as PDF/A-2b.
	//
	// fixOCGConfigs repairs the /Name of test 1, the uniqueness of test 2, the
	// /Order completeness of test 3 and the /AS ban of test 4, which the UA
	// specifications spell as their own test 2. The UA specifications state no
	// /Order test.
	//
	// Test 3 was once implemented for /D alone and MEASURED to be a false fix:
	// a /Configs configuration carried its own incomplete /Order, so veraPDF
	// kept reporting while litho, whose detector then read /D alone, went
	// quiet. The repair now completes every configuration the rule names, as
	// the widened detector judges them.
	//
	// The test numbers are written out rather than taken as a range, so a test
	// this fixer does not implement cannot be claimed by arriving later.
	//
	// Exact specification and clause, never clausePrefix: ISO 19005-1 spells an
	// AcroForm NeedAppearances rule 6.9, and EVERY part-4 6.9 test is an
	// embedded-file rule (t2 the F and UF keys, t3 embedded-file conformance,
	// t4 AFRelationship). The prefix "7.1" already swallows 7.10 in matchXMP.
	//
	// Each arm MATCHES and falls through; none of them returns a negative.
	// Written as `return v.Rule.Clause == "6.9"` this claimed the OCG rule and
	// silently UN-claimed every other test-1 violation in the same
	// specification, because the false short-circuited the 6.1.12 Perms and
	// Signature-Reference block below. A count-based gate cannot see that
	// while the newly matched cells outnumber the newly broken ones.
	switch v.Rule.Specification {
	case "ISO_19005_2", "ISO_19005_3":
		if v.Rule.Clause == "6.9" {
			switch v.Rule.TestNumber {
			case 1, 2, 3, 4:
				return true
			}
		}
	case "ISO_14289_1":
		if v.Rule.Clause == "7.10" {
			switch v.Rule.TestNumber {
			case 1, 2:
				return true
			}
		}
	case "ISO_14289_2":
		if v.Rule.Clause == "8.7" {
			switch v.Rule.TestNumber {
			case 1, 2:
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

// fixCatalogCleanup repairs the catalog and trailer constructs
// [matchCatalogCleanup] claims, then removes the embedded-file constructs
// PDF/A-1 forbids.
//
// # Corpus evidence
//
//   - All corpora: 8 of the 19 corpus carriers of the ISO 19005-1 6.1.13 test
//     1 /OCProperties ban carry /OC entries, whose content deleting the key
//     makes unconditionally visible.
func fixCatalogCleanup(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, profile string) ([]string, error) {
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

	// ISO 19005-1 6.1.13 test 1: PDF/A-1 forbids optional content, and the
	// catalog /OCProperties entry is the whole of what the rule tests
	// (`isOptionalContentPresent == false`; litho's detector reads the key's
	// presence alone). Deleting it is therefore the only way to produce a
	// conforming part-1 file, exactly as fixTransparency flattens
	// transparency regardless of PreserveContent because part 1 forbids that
	// outright too.
	//
	// On a file that genuinely uses optional content this makes the affected
	// content unconditionally visible. That IS what "no optional content"
	// means for part 1, and refusing would mean declining to produce a
	// conforming file at all. The /OC entries themselves are left in place:
	// the rule does not test them, and removing them is a larger change than
	// the rule requires.
	//
	// Gated on the VIOLATION, never on the profile string: parts 2, 3 and 4
	// permit optional content and never state this rule, so the same file
	// keeps its /OCProperties when it is validated under them. See
	// fixOCGConfigs for why the part table stays in compliance.
	if anyViolation(violations, forbiddenOCPropertiesViolation) {
		if _, ok := catalogDict["OCProperties"]; ok {
			newCatalog := make(ast.PdfDict, len(catalogDict))
			maps.Copy(newCatalog, catalogDict)
			delete(newCatalog, "OCProperties")
			if alloc.AllocAt(catalogRef.ObjectNum, newCatalog) {
				// Keep the local copy current: the repairs below copy from
				// this dict, and copying from a stale one would restore the
				// key just removed.
				catalogDict = newCatalog
				fixes = append(fixes, "removed /OCProperties from catalog (optional content forbidden in PDF/A-1)")
			}
		}
	} else if ocpObj, ok := catalogDict["OCProperties"]; ok {
		// 6.10: Repair the optional-content configuration dictionaries.
		// Skipped when the key is being removed: repairing a configuration
		// that is about to be deleted reports work the output never carries.
		ocFixes := fixOCGConfigs(ctx, alloc, doc, catalogDict, catalogRef, ocpObj, violations)
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
// reports that the declared range no longer matches the signed revision. The
// swap keeps the violation count unchanged, so a count-based gate cannot see
// it.
//
// Aggressive mode still strips. There the invalidation is intended and
// completes a chain: the strip breaks the coverage, §6.4.3 test 1 fires, and
// [fixSignatureStrip] then removes the now-invalidated signature, resolving
// both rules. Guarding unconditionally severed that chain.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-1-12-t02-fail-*: on all three
//     fixtures the §6.1.12-for-§6.4.3 swap kept the violation count at one
//     and so stayed invisible to a count-based gate.
//   - All corpora: guarding unconditionally cost 18 fixtures their fully-fixed
//     verdict in the remediation-aggressive corpus cell, a drop the gate could
//     not flag because it compares against a much older baseline.
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

// fixOCGConfigs walks /OCProperties /D and /Configs and repairs the four
// requirements the optional-content configuration clause states: a /Name on
// each configuration, a name unique among them, an /Order referencing every
// OCG, and no /AS. Per ISO 32000-2, Table 100.
//
// The /Name repair runs unconditionally. The other three run only when the
// validation actually reported them, because the profiles do not all state
// them: ISO 19005-4 6.10 has no AS test, so a PDF/A-4 file may legitimately
// carry an /AS that a part-2 file may not, and no PDF/UA part states the
// /Order test. Reading that from the violations rather than from the profile
// string keeps the part table in [compliance], which is the only place that
// decides which rule a profile states.
func fixOCGConfigs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, catalogRef ast.PdfIndirectRef, ocpObj ast.PdfObject, violations []compliance.Violation) []string {
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

	// The other three requirements of the same clause. Each is gated on its
	// own violation: see the function comment for why the profile string is
	// not consulted here.
	if anyViolation(violations, ocgOrderCompleteViolation) {
		if descs := completeOCGOrders(ctx, alloc, doc, newOCP); len(descs) > 0 {
			fixes = append(fixes, descs...)
			modified = true
		}
	}
	if anyViolation(violations, ocgASViolation) {
		if descs := stripOCGUsageApplication(ctx, alloc, doc, newOCP); len(descs) > 0 {
			fixes = append(fixes, descs...)
			modified = true
		}
	}
	if anyViolation(violations, ocgNameUniqueViolation) {
		if descs := uniqueOCGConfigNames(ctx, alloc, doc, newOCP); len(descs) > 0 {
			fixes = append(fixes, descs...)
			modified = true
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

// anyViolation reports whether the run flagged a rule pred names.
func anyViolation(violations []compliance.Violation, pred func(compliance.Rule) bool) bool {
	for _, v := range violations {
		if pred(v.Rule) {
			return true
		}
	}
	return false
}

// ocgNameUniqueViolation matches "the /Name shall be unique amongst all
// optional content configuration dictionaries": ISO 19005-2/3 6.9 t2 and
// ISO 19005-4 6.10 t2.
func ocgNameUniqueViolation(r compliance.Rule) bool {
	switch r.Specification {
	case "ISO_19005_2", "ISO_19005_3":
		return r.Clause == "6.9" && r.TestNumber == 2
	case "ISO_19005_4":
		return r.Clause == "6.10" && r.TestNumber == 2
	}
	return false
}

// forbiddenOCPropertiesViolation identifies ISO 19005-1 6.1.13 test 1, the
// part-1 ban on a catalog /OCProperties entry.
//
// Matched on specification, clause AND test number rather than a clause
// prefix, because 6.1.13 is a DIFFERENT rule in every other part: ISO 19005-2
// and 19005-3 spell their implementation limits there (integer, real, string
// and name caps, object count, q/Q nesting, DeviceN colorants, CID values and
// page-box size, tests 1 to 11). A prefix match would claim all of those.
func forbiddenOCPropertiesViolation(r compliance.Rule) bool {
	return r.Specification == "ISO_19005_1" && r.Clause == "6.1.13" && r.TestNumber == 1
}

// ocgOrderCompleteViolation matches "if an optional content configuration
// dictionary contains the Order key, the array which is the value of this
// Order key shall contain references to all OCGs in the conforming file":
// ISO 19005-2/3 6.9 t3 and ISO 19005-4 6.10 t3. ISO 19005-4 6.9 t3 is an
// embedded-file rule, so the part-4 spelling is matched on 6.10 alone.
func ocgOrderCompleteViolation(r compliance.Rule) bool {
	switch r.Specification {
	case "ISO_19005_2", "ISO_19005_3":
		return r.Clause == "6.9" && r.TestNumber == 3
	case "ISO_19005_4":
		return r.Clause == "6.10" && r.TestNumber == 3
	}
	return false
}

// completeOCGOrders appends to the /Order of every optional content
// configuration the OCGs it leaves out, then gives a text-string /Name to
// every OCG that lacks one. ISO 19005-2 6.9 t3 and ISO 19005-4 6.10 t3 name
// every configuration dictionary carrying /Order, /D and each /Configs entry
// alike.
//
// The missing set is what [compliance.OCGsMissingFromOrder] reports, so the
// repair adds exactly what the check reads as absent and leaves an /Order the
// check cannot read whole as it is. The OCGs are appended at the top level of
// /Order, where ISO 32000-1, 8.11.4.3 lists them as ungrouped entries, and
// the existing groupings keep their place.
//
// The /Name step relaxes nothing: ISO 32000-1, Table 98 makes an OCG's /Name a
// required text string. It is part of this repair because veraPDF matches
// /Order to the OCGs by /Name (GFPDOCConfig removes each referenced OCG's
// name from the set of /OCGs names), and a nameless OCG enters that set as an
// empty string a reference can never remove: the completed /Order of a file
// whose OCG had no /Name stayed reported.
//
// # Corpus evidence
//
//   - litho complex_ua2.pdf: /Configs [ << /Order [ ] >> ] beside an
//     incomplete /D; completing /D alone left veraPDF reporting the /Configs
//     entry.
//   - litho complex_2b.pdf: its OCG carries no /Name, and its completed
//     /Order stayed reported by veraPDF until the OCG was named.
func completeOCGOrders(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ocp ast.PdfDict) []string {
	ocgs := compliance.DocumentOCGs(ctx, doc, ocp)
	if len(ocgs) == 0 {
		return nil
	}

	var fixes []string
	eachOCGConfig(ctx, alloc, doc, ocp, func(label string, d ast.PdfDict) (ast.PdfDict, bool) {
		missing := compliance.OCGsMissingFromOrder(ctx, doc, d, ocgs)
		if len(missing) == 0 {
			return nil, false
		}
		order, err := document.ResolveArray(ctx, doc, d["Order"])
		if err != nil {
			return nil, false
		}
		newOrder := make(ast.PdfArray, 0, len(order)+len(missing))
		newOrder = append(newOrder, order...)
		for _, ref := range missing {
			newOrder = append(newOrder, ref)
		}
		newD := make(ast.PdfDict, len(d))
		maps.Copy(newD, d)
		newD["Order"] = newOrder
		fixes = append(fixes, fmt.Sprintf("added %d OCG(s) missing from /Order of OCG %s", len(missing), label))
		return newD, true
	})

	for _, ref := range ocgs {
		if ctx.Err() != nil {
			break
		}
		ocg := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if ocg == nil {
			continue
		}
		label := fmt.Sprintf("Layer %d", ref.ObjectNum)
		if nameObj, ok := ocg["Name"]; ok {
			if _, err := document.ResolveString(ctx, doc, nameObj); err == nil {
				continue
			}
			// A name object written where Table 98 wants a text string keeps
			// its text.
			if name, err := document.ResolveName(ctx, doc, nameObj); err == nil && name != "" {
				label = string(name)
			}
		}
		newOCG := make(ast.PdfDict, len(ocg)+1)
		maps.Copy(newOCG, ocg)
		newOCG["Name"] = ast.PdfString(label)
		if alloc.AllocAt(ref.ObjectNum, newOCG) {
			fixes = append(fixes, fmt.Sprintf("added /Name to OCG object %d", ref.ObjectNum))
		}
	}
	return fixes
}

// ocgASViolation matches "the AS key shall not appear in any optional content
// configuration dictionary": ISO 19005-2/3 6.9 t4, ISO 14289-1 7.10 t2 and
// ISO 14289-2 8.7 t2. ISO 19005-4 states no AS test, which is exactly why this
// is read from the violations: a part-4 file keeps its /AS.
func ocgASViolation(r compliance.Rule) bool {
	switch r.Specification {
	case "ISO_19005_2", "ISO_19005_3":
		return r.Clause == "6.9" && r.TestNumber == 4
	case "ISO_14289_1":
		return r.Clause == "7.10" && r.TestNumber == 2
	case "ISO_14289_2":
		return r.Clause == "8.7" && r.TestNumber == 2
	}
	return false
}

// eachOCGConfig applies fn to the default configuration and then to every
// /Configs entry, in the order [compliance] inspects them, so a repair that
// depends on what came before sees the sequence the checker did.
//
// A changed configuration is written back where it lives: through the allocator
// when it is indirect, or into ocp when it is direct, because a direct
// dictionary has no object number to address. fn returns the replacement and
// whether anything changed; it must not mutate the dictionary it is given.
func eachOCGConfig(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ocp ast.PdfDict, fn func(label string, d ast.PdfDict) (ast.PdfDict, bool)) {
	apply := func(obj ast.PdfObject, label string) (ast.PdfObject, bool) {
		ref, isRef := obj.(ast.PdfIndirectRef)

		var d ast.PdfDict
		if isRef {
			d = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		}
		if d == nil {
			resolved, err := document.ResolveDict(ctx, doc, obj)
			if err != nil || resolved == nil {
				return obj, false
			}
			d = resolved
		}

		replacement, changed := fn(label, d)
		if !changed {
			return obj, false
		}
		if isRef {
			alloc.AllocAt(ref.ObjectNum, replacement)
			return obj, true
		}
		return replacement, true
	}

	if dObj, ok := ocp["D"]; ok {
		if replacement, changed := apply(dObj, "default config (D)"); changed {
			ocp["D"] = replacement
		}
	}

	configsObj, ok := ocp["Configs"]
	if !ok {
		return
	}
	arr, err := document.ResolveArray(ctx, doc, configsObj)
	if err != nil || arr == nil {
		return
	}
	newConfigs := make(ast.PdfArray, len(arr))
	copy(newConfigs, arr)
	configsChanged := false
	for i, entry := range arr {
		if ctx.Err() != nil {
			return
		}
		if replacement, changed := apply(entry, fmt.Sprintf("config %d", i)); changed {
			newConfigs[i] = replacement
			configsChanged = true
		}
	}
	if configsChanged {
		ocp["Configs"] = newConfigs
	}
}

// stripOCGUsageApplication removes the /AS array from every optional-content
// configuration. A usage-application array lets a viewer toggle content
// visibility automatically, which the profiles that state the rule forbid.
//
// Presence is read as the key being there at all, which is how the PDF/A
// checker reads it. The UA checker drops an entry whose value is the null
// object, so a direct null is absent to it; deleting the key satisfies both
// readings.
func stripOCGUsageApplication(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ocp ast.PdfDict) []string {
	var fixes []string
	eachOCGConfig(ctx, alloc, doc, ocp, func(label string, d ast.PdfDict) (ast.PdfDict, bool) {
		if _, has := d["AS"]; !has {
			return nil, false
		}
		newD := make(ast.PdfDict, len(d))
		maps.Copy(newD, d)
		delete(newD, "AS")
		fixes = append(fixes, fmt.Sprintf("removed forbidden /AS from OCG %s", label))
		return newD, true
	})
	return fixes
}

// uniqueOCGConfigNames renames a configuration whose /Name another
// configuration already used. The checker collects /D first and then /Configs
// in order, flagging each repeat, so the first use keeps the name and the later
// one is renamed.
//
// A configuration with no name at all is left to the test-1 repair above.
func uniqueOCGConfigNames(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ocp ast.PdfDict) []string {
	var fixes []string
	seen := make(map[string]bool)
	eachOCGConfig(ctx, alloc, doc, ocp, func(label string, d ast.PdfDict) (ast.PdfDict, bool) {
		name := ocgConfigName(ctx, doc, d)
		if name == "" {
			return nil, false
		}
		if !seen[name] {
			seen[name] = true
			return nil, false
		}
		unique := disambiguateOCGConfigName(name, seen)
		seen[unique] = true

		newD := make(ast.PdfDict, len(d))
		maps.Copy(newD, d)
		newD["Name"] = ast.PdfString(unique)
		fixes = append(fixes, fmt.Sprintf("made OCG %s name unique: %q became %q", label, name, unique))
		return newD, true
	})
	return fixes
}

// disambiguateOCGConfigName derives a name no other configuration carries by
// suffixing the one the file already uses. The value is a label a viewer shows
// in its layer panel (ISO 32000-1, Table 101), so deriving from the original
// keeps what the producer meant. Inventing an unrelated name, or deleting the
// duplicate configuration, would lose more than the defect costs.
//
// The search ends at the first free candidate, and seen never holds more
// entries than the file has configurations, so it cannot run away.
func disambiguateOCGConfigName(base string, seen map[string]bool) string {
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)", base, n)
		if !seen[candidate] {
			return candidate
		}
	}
}

// ocgConfigName reads an optional-content configuration's /Name as the checker
// does: a text string normally, a name object from the occasional producer that
// writes one. Returns "" when absent or unresolvable.
//
// doc may be nil when a caller repairs a dictionary it already holds, so a
// direct value is read without resolving.
func ocgConfigName(ctx context.Context, doc *document.Document, d ast.PdfDict) string {
	nameObj, ok := d["Name"]
	if !ok {
		return ""
	}
	if s, ok := nameObj.(ast.PdfString); ok {
		return string(s)
	}
	if n, ok := nameObj.(ast.PdfName); ok {
		return string(n)
	}
	if doc == nil {
		return ""
	}
	if s, err := document.ResolveString(ctx, doc, nameObj); err == nil {
		return string(s)
	}
	if n, err := document.ResolveName(ctx, doc, nameObj); err == nil {
		return string(n)
	}
	return ""
}

// ensureOCGConfigName gives an optional-content configuration dictionary the
// /Name its profile requires, reporting the replacement the caller must install
// and whether anything changed.
//
// The replacement is returned rather than only written because a configuration
// may be a DIRECT dictionary inside /OCProperties, which no object number
// addresses. Writing only through the allocator dropped that case on the floor
// while still reporting the fix, so the catalog was rewritten with the /Name
// still missing and the next run found the same defect: repeated remediation
// grew the file every pass, forever, and never added the /Name.
//
// A nil replacement with a true result means the change went through the
// allocator and the caller has nothing to install.
//
// # Corpus evidence
//
//   - litho complex_1b.pdf: remediating it repeatedly grew it by 1125 bytes a
//     pass, forever, and never added the /Name.
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
// Testing only for the key left a default configuration carrying `/Name ()`
// flagged: present, and empty. The checker calls hasNonEmptyName, so the
// violation was reported while the repair declined.
//
// doc may be nil when a caller repairs a dictionary it already holds, so a
// direct string is read without resolving.
//
// # Corpus evidence
//
//   - verapdf 7.10-t01-fail-a: stayed flagged after its clause was routed
//     here, because its default configuration carries `/Name ()`.
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
