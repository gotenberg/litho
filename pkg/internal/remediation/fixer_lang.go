package remediation

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxStructTreeDepth bounds the structure-tree walk in the /Lang repair. It
// reuses [compliance.MaxStructTreeWalkDepth] so the repair walks exactly as deep
// as the validator that flags the violations: a shallower fixer would flag
// structure elements it could not reach to repair. A visited set independently
// breaks reference cycles.
const maxStructTreeDepth = compliance.MaxStructTreeWalkDepth

// matchLangClauses matches the /Lang violations proper, across the specs that
// state them.
//
// ISO 14289-1 states its language requirements in clause 7.2 exactly, and the
// clause is matched exactly for that reason: [clausePrefix] is a string prefix
// test, so "7.2" also matches 7.20 and every 7.21.x clause, which is the whole
// PDF/UA-1 FONT family (CMaps, embedding, ToUnicode, .notdef). Claiming those
// here made clause 7.21.8 look repaired while no fixer could touch it. The
// other three clauses have no such neighbour, measured against the rule table.
func matchLangClauses(v compliance.Violation) bool {
	return clausePrefix(v, "6.7.4", "6.8.4", "8.4.4") || v.Rule.Clause == "7.2"
}

// matchCatalogLang matches the /Lang violations this fixer repairs and, under
// PDF/UA-1, the violations answered by the fixers whose repair writes text in
// a natural language.
//
// ISO 14289-1, 7.2 requires the natural language of such text to be
// determined, and text a repair writes declares none, so only the catalog
// /Lang determines it. xmp-metadata writes dc:title as a language alternative
// with an x-default entry, which veraPDF's 7.2 test 33 (xDefault == false ||
// gContainsCatalogLang == true) then judges, and auto-tag and figure-alt write
// the /Alt of a Figure (test 22), form-tooltip a /TU (test 25) and a widget
// /Contents (test 24). None of that text is in the document the violations
// are judged on, so claiming the 7.2 findings alone left the catalog /Lang to
// a second remediation round, run on the first round's output. The claim is
// harmless where the text needs no language after all: the repair only adds a
// /Lang the catalog lacks. PDF/UA-2 needs no such claim, since ISO 14289-2,
// 8.4.4 requires the catalog /Lang outright.
//
// # Corpus evidence
//
//   - govdocs thread_100_100811 and thread_300_300189, pdfcpu
//     pkg_testdata__empty.pdf, pkg_testdata__blank-scan.pdf and
//     pkg_testdata__HL1396.pdf, and 19 pdfjs files, among them issue4379.pdf
//     and issue8823.pdf: their source reports no 7.2 finding, and each
//     PDF/UA-1 remediation, default and aggressive, took a second round for
//     the catalog /Lang alone, once the dc:title xmp-metadata wrote failed
//     test 33 and, on the auto-tagged scans, each Figure /Alt test 22.
//   - All corpora: over the PDF/UA-1 remediations, default and aggressive,
//     of 343 sampled files, those 24 now take one round and no residual
//     moves. No other file took a second round for catalog-lang. veraPDF
//     1.30 passes 23 of the 24 one-round outputs, as it passed the two-round
//     ones, and cannot read the page tree of poppler-85140-0.pdf in either.
func matchCatalogLang(v compliance.Violation) bool {
	return matchLangClauses(v) || (v.Rule.Specification == "ISO_14289_1" && writesLanguageText(v))
}

// writesLanguageText reports whether v is answered by a fixer whose repair
// writes text in a natural language: the dc:title of xmp-metadata, the Figure
// /Alt of auto-tag and figure-alt, and the /TU and /Contents of form-tooltip.
func writesLanguageText(v compliance.Violation) bool {
	return matchXMP(v) || matchAutoTag(v) || matchFigureAlt(v) || matchFormTooltip(v)
}

// fixCatalogLang repairs the document language. It adds a missing catalog /Lang,
// replaces an invalid catalog /Lang with a valid tag, and strips invalid /Lang
// entries from the StructTreeRoot and from structure elements, indirect and
// inline. An element that loses its /Lang inherits its nearest valid ancestor's
// language, ultimately the now-valid catalog /Lang, per ISO 32000-1:2008
// §14.9.2. Validity is the BCP 47 / RFC 3066 form §14.9.2 requires, checked via
// [compliance.IsValidLangTag] (reused, not re-derived).
func fixCatalogLang(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	// A document with no catalog reference cannot take a catalog /Lang. Skip,
	// as fixXMP and fixXMPSchema do: an error here discards the repairs of
	// every other fixer, and this fixer also runs for violations that are not
	// its own (see matchCatalogLang).
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	lang := opts.Lang
	if lang == "" {
		lang = "en"
	}

	var fixes []string

	// Catalog /Lang: add when missing, replace when present but invalid.
	if langObj, present := catalogDict["Lang"]; !present {
		newCatalog := make(ast.PdfDict, len(catalogDict)+1)
		maps.Copy(newCatalog, catalogDict)
		newCatalog["Lang"] = ast.PdfString(lang)
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
		fixes = append(fixes, fmt.Sprintf("set catalog /Lang to %q", lang))
	} else if !langTagValid(ctx, doc, langObj, requiresDeclaredLang(profile)) || (langUnknown(ctx, doc, langObj) && requiresDeclaredLang(profile)) {
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["Lang"] = ast.PdfString(lang)
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
		fixes = append(fixes, fmt.Sprintf("replaced invalid catalog /Lang with %q", lang))
	}

	// Structure tree: strip an invalid /Lang from the StructTreeRoot and any
	// structure element so it inherits a valid ancestor's language, ultimately
	// the catalog /Lang set above, rather than carrying a malformed tag.
	requireDeclared := requiresDeclaredLang(profile)
	if n := repairStructElemLangs(ctx, alloc, doc, catalogDict, requireDeclared); n > 0 {
		fixes = append(fixes, fmt.Sprintf("removed invalid /Lang from %d structure element(s)", n))
	}

	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

// requiresDeclaredLang reports whether the profile insists the document name a
// natural language rather than declare it unknown.
//
// ISO 32000-1, 14.9.2 lets the empty text string stand for "the language is
// unknown", and [compliance.IsValidLangTag] accepts it on that basis. PDF/UA
// does not: ISO 14289-1, 7.2 and ISO 14289-2, 8.4.4 require the default
// language to be identified, which is why litho and veraPDF both report an
// empty catalog /Lang that the fixer, asking the lenient question, would
// never replace.
//
// The WTPDF levels adopt ISO 14289-2 and state 8.4.4 with it, so they
// require a declared language too. Asking [compliance.IsPDFUA] rather than
// testing the string keeps that true for every profile built on ISO 14289:
// a "PDF/UA" prefix test left the marked-content language fix dark under a
// WTPDF run.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.2-t29-fail-n: litho and veraPDF both report its
//     empty catalog /Lang, while the fixer asking the lenient question
//     replaced nothing.
//   - All corpora: a corpus differential caught the "PDF/UA" prefix test
//     under a WTPDF run as one fewer file repaired.
func requiresDeclaredLang(profile string) bool {
	return compliance.IsPDFUA(profile)
}

// langUnknown reports whether obj resolves to the empty language tag.
func langUnknown(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	s, err := document.ResolveString(ctx, doc, obj)
	if err != nil {
		return false
	}
	return strings.TrimSpace(document.DecodePdfString(s)) == ""
}

// langTagValid reports whether obj resolves to a valid (or absent) language tag.
// An unresolvable value is treated as valid so the fixer never alters what it
// cannot read. DecodePdfString handles UTF-16/hex string forms. Under PDF/UA
// (ua) a non-empty value is judged as written, exactly as the validator judges
// it ([compliance.IsValidUALangTag]); an empty one is left to langUnknown.
func langTagValid(ctx context.Context, doc *document.Document, obj ast.PdfObject, ua bool) bool {
	s, err := document.ResolveString(ctx, doc, obj)
	if err != nil {
		return true
	}
	if ua && strings.TrimSpace(document.DecodePdfString(s)) != "" {
		return compliance.IsValidUALangTag(s)
	}
	return compliance.IsValidLangTag(document.DecodePdfString(s))
}

// repairStructElemLangs walks the logical structure tree from the catalog's
// StructTreeRoot and strips every invalid /Lang it finds: on the StructTreeRoot
// itself, on indirect structure elements, and on inline (direct-dict) structure
// elements nested in a /K array. It returns the number of /Lang entries removed.
func repairStructElemLangs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, requireDeclared bool) int {
	stObj, ok := catalogDict["StructTreeRoot"]
	if !ok {
		return 0
	}
	stRef, ok := stObj.(ast.PdfIndirectRef)
	if !ok {
		return 0
	}
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		return 0
	}
	visited := map[int]bool{stRef.ObjectNum: true}
	count := 0
	repairOwner(ctx, alloc, doc, stRef, stDict, visited, &count, 0, requireDeclared)
	return count
}

// repairOwner repairs one indirect structure object: its own /Lang plus any
// inline-dict descendants reachable through its /K that are not themselves
// indirect objects. Indirect children in /K are recursed as separate owners and
// persist via their own allocation. The owner is written back only when its own
// /Lang or one of its inline descendants changed, keeping the output
// deterministic and untouched objects stable.
func repairOwner(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ownerRef ast.PdfIndirectRef, ownerDict ast.PdfDict, visited map[int]bool, count *int, depth int, requireDeclared bool) {
	if depth > maxStructTreeDepth || ctx.Err() != nil {
		return
	}
	newOwner := make(ast.PdfDict, len(ownerDict))
	maps.Copy(newOwner, ownerDict)
	changed := removeInvalidLang(ctx, doc, newOwner, requireDeclared)
	if changed {
		*count++
	}
	if newK, kChanged := repairKids(ctx, alloc, doc, ownerDict["K"], visited, count, depth+1, requireDeclared); kChanged {
		newOwner["K"] = newK
		changed = true
	}
	if changed {
		alloc.AllocAt(ownerRef.ObjectNum, newOwner)
	}
}

// repairKids returns a /K value with invalid /Lang stripped from every inline
// (direct-dict) structure element it contains, and whether anything changed. A
// container is copied only when a descendant changed (copy-on-write), so an
// unchanged subtree is shared and the output stays deterministic. Indirect
// children are recursed as separate owners and left as references in /K. visited
// breaks reference cycles and the depth bound caps a pathological tree.
func repairKids(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject, visited map[int]bool, count *int, depth int, requireDeclared bool) (ast.PdfObject, bool) {
	if depth > maxStructTreeDepth || ctx.Err() != nil {
		return kObj, false
	}
	switch v := kObj.(type) {
	case ast.PdfArray:
		var newArr ast.PdfArray
		for i, item := range v {
			newItem, itemChanged := repairKids(ctx, alloc, doc, item, visited, count, depth, requireDeclared)
			if itemChanged && newArr == nil {
				newArr = make(ast.PdfArray, len(v))
				copy(newArr, v)
			}
			if newArr != nil {
				newArr[i] = newItem
			}
		}
		if newArr == nil {
			return v, false
		}
		return newArr, true
	case ast.PdfIndirectRef:
		if v.ObjectNum == 0 || visited[v.ObjectNum] {
			return v, false
		}
		visited[v.ObjectNum] = true
		if elem := resolveFromAllocOrDoc(ctx, alloc, doc, v); elem != nil {
			repairOwner(ctx, alloc, doc, v, elem, visited, count, depth+1, requireDeclared)
		}
		return v, false
	case ast.PdfDict:
		newDict := make(ast.PdfDict, len(v))
		maps.Copy(newDict, v)
		changed := removeInvalidLang(ctx, doc, newDict, requireDeclared)
		if changed {
			*count++
		}
		if newK, kChanged := repairKids(ctx, alloc, doc, v["K"], visited, count, depth+1, requireDeclared); kChanged {
			newDict["K"] = newK
			changed = true
		}
		if !changed {
			return v, false
		}
		return newDict, true
	default:
		return kObj, false
	}
}

// removeInvalidLang strips an invalid /Lang from a mutable structure dict,
// returning whether it removed one. A valid or absent /Lang is left intact. The
// caller owns dict (a fresh copy), so the document's parsed objects are never
// mutated in place.
func removeInvalidLang(ctx context.Context, doc *document.Document, dict ast.PdfDict, requireDeclared bool) bool {
	langObj, ok := dict["Lang"]
	if !ok {
		return false
	}
	// An empty tag means "unknown" (ISO 32000-1, 14.9.2). PDF/UA does not
	// accept that answer, so under those profiles it is stripped like any
	// other unusable value and the element inherits a declared ancestor.
	if langTagValid(ctx, doc, langObj, requireDeclared) && (!requireDeclared || !langUnknown(ctx, doc, langObj)) {
		return false
	}
	delete(dict, "Lang")
	return true
}
