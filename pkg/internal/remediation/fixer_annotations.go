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

// forbiddenAnnotSubtypes lists annotation subtypes that fixAnnotArray removes
// even when [Options.PreserveContent] is set, unlike the profile-exact check
// beside it.
//
// Sound, Movie, and Screen are outside the permitted set of every PDF/A part
// (ISO 19005-1, 6.5.2 and ISO 19005-2/3/4, 6.3.1). 3D is outside it for parts
// 1 to 3 and for part 4 at the base and F levels, but ISO 19005-4 6.3.1
// permits it at level E (Annex B, see pdfa4ePermittedAnnotSubtypes), so
// fixAnnotArray consults the profile's own table before this one.
var forbiddenAnnotSubtypes = map[ast.PdfName]bool{
	"Sound": true, "Movie": true, "Screen": true, "3D": true,
}

// permittedAnnotSubtypes lists subtypes defined in ISO 32000-1 that are
// allowed by PDF/A-2/3/4 §6.3.1. Any subtype outside this set plus any
// entry in [forbiddenAnnotSubtypes] must be removed, unless the profile's own
// table permits it: RichMedia is absent here and permitted by PDF/A-4e.
var permittedAnnotSubtypes = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"FileAttachment": true, "Widget": true, "PrinterMark": true,
	"TrapNet": true, "Watermark": true, "Redact": true,
	"Projection": true, // ISO 32000-2 Table 171, re-permitted by PDF/A-4 §6.3.1.
}

// permittedAAKeys lists the additional-actions keys allowed on catalog, page,
// and non-widget annotation dicts in PDF/A-4 (ISO 19005-4, section 6.6.3).
var permittedAAKeys = map[ast.PdfName]bool{
	"E": true, "X": true, "D": true, "U": true, "Fo": true, "Bl": true,
}

func matchAnnotations(v compliance.Violation) bool {
	return clausePrefix(v,
		"6.3.1", // forbidden annotation types
		"6.4.1", // widget /A or /AA, form field /AA
		"6.5.2", // catalog/page /AA (part 1)
		"6.5.3", // annotation flags (part 1)
		"6.6.2", // widget and form field /AA (part 1)
		"6.6.3", // non-permitted AA keys
	)
}

func fixAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	var fixes []string

	// Remove /AA from catalog (section 6.5.2, 6.6.2, 6.6.3).
	if _, ok := catalogDict["AA"]; ok {
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		delete(newCatalog, "AA")
		catalogDict = newCatalog
		alloc.AllocAt(catalogRef.ObjectNum, catalogDict)
		fixes = append(fixes, "removed /AA from catalog")
	}

	// The field walk runs before the page walk, so a merged field and widget
	// the page walk rewrites is read with its /AA already gone.
	if compliance.FieldAAForbidden(profile) {
		fieldFixes, newCatalog := rewriteFieldAA(ctx, alloc, doc, catalogDict, dropFieldAA)
		if newCatalog != nil {
			catalogDict = newCatalog
			alloc.AllocAt(catalogRef.ObjectNum, catalogDict)
		}
		fixes = append(fixes, fieldFixes...)
	}

	// Walk pages to fix annotations.
	pageFixes, err := fixPageAnnotations(ctx, alloc, doc, catalogDict, opts, profile)
	if err != nil {
		return nil, fmt.Errorf("fixing page annotations: %w", err)
	}
	fixes = append(fixes, pageFixes...)

	return fixes, nil
}

// dropFieldAA is the [fieldAACleaner] that removes a form field's /AA whatever
// it holds. ISO 19005-1 §6.6.2 t2 and ISO 19005-2/3 §6.4.1 t2 forbid the key
// itself on a field, so reducing it to the permitted actions, which the
// javascript fixer does, still leaves the violation: veraPDF reports a field
// /AA holding one GoTo action, measured. A direct null is an absent entry (ISO
// 32000-1, 7.3.7) and is left alone, as compliance's walkFormFields leaves it.
//
// # Corpus evidence
//
//   - All corpora: 50 files carry a form field /AA, and remediated at PDF/A-1b
//     and PDF/A-2b, or at their own profile for the verapdf ones, none keeps
//     it: each is merged with its widget or holds forbidden actions alone, so
//     the page walk or the javascript fixer already cleared it. The verdict
//     moves only for a field that is no widget and holds a permitted action.
func dropFieldAA(_ context.Context, aaObj ast.PdfObject, location string) ([]string, ast.PdfDict) {
	if _, isNull := aaObj.(ast.PdfNull); isNull {
		return nil, nil
	}
	return []string{fmt.Sprintf("removed /AA from %s", location)}, ast.PdfDict{}
}

// annotPageEntry pairs a page dict with its indirect reference.
type annotPageEntry struct {
	ref  ast.PdfIndirectRef
	dict ast.PdfDict
}

// fixPageAnnotations walks the page tree and fixes annotation issues on each page.
func fixPageAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, opts Options, profile string) ([]string, error) {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return nil, nil
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)

	var fixes []string
	for i, pe := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		pageDict := pe.dict

		// Remove /AA from page dict (section 6.5.2).
		if _, hasAA := pageDict["AA"]; hasAA {
			newPage := make(ast.PdfDict, len(pageDict))
			maps.Copy(newPage, pageDict)
			delete(newPage, "AA")
			pageDict = newPage
			if pe.ref.ObjectNum > 0 {
				alloc.AllocAt(pe.ref.ObjectNum, pageDict)
			}
			fixes = append(fixes, fmt.Sprintf("removed /AA from page %d", i+1))
		}

		annotsObj, ok := pageDict["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil || len(annots) == 0 {
			continue
		}

		pageFixes, modified := fixAnnotArray(ctx, alloc, doc, annots, i+1, opts, profile)
		fixes = append(fixes, pageFixes...)

		if modified && pe.ref.ObjectNum > 0 {
			// Re-resolve in case the page dict was already replaced above.
			currentPage := resolveFromAllocOrDoc(ctx, alloc, doc, pe.ref)
			if currentPage != nil {
				pageDict = currentPage
			}

			rebuilt := rebuildAnnotsArray(annots)
			if annotsRef, isRef := annotsObj.(ast.PdfIndirectRef); isRef {
				alloc.AllocAt(annotsRef.ObjectNum, rebuilt)
			} else {
				newPage := make(ast.PdfDict, len(pageDict))
				maps.Copy(newPage, pageDict)
				newPage["Annots"] = rebuilt
				alloc.AllocAt(pe.ref.ObjectNum, newPage)
			}
		}
	}

	return fixes, nil
}

// maxPageTreeDepth bounds the page-tree walks in this package. Anti-DoS guard,
// not spec-mandated: ISO 32000-1, 7.7.3.2 makes the page tree a strict tree, so
// a crafted /Kids chain is the only way to reach any depth, and 100 sits far
// above the depth real page trees reach.
//
// core/document/catalog.go bounds the same descent at 256 and carries the home
// note for this cap; compliance/check.go uses 100 as here.
//
// One consumer borrows it for a different shape: findInheritedResources in
// fixer_pageresources.go walks /Parent UPWARD rather than descending /Kids. The
// limit is right either way, since both directions are shallow, but the name
// describes the descent. See the note at that use site.
//
// # Corpus evidence
//
//   - All corpora: the deepest page tree is 4.
const maxPageTreeDepth = 100

// collectPageRefs recursively collects page dicts with their indirect refs.
func collectPageRefs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, pages *[]annotPageEntry, seen map[int]bool, depth int) {
	if ctx.Err() != nil || depth > maxPageTreeDepth {
		return
	}

	var ref ast.PdfIndirectRef
	if r, ok := obj.(ast.PdfIndirectRef); ok {
		ref = r
		// A page tree is a strict tree (ISO 32000-1 §7.7.3.2); visit each node
		// once so a shared or cyclic /Kids tree cannot fan out 2^depth.
		if seen[ref.ObjectNum] {
			return
		}
		seen[ref.ObjectNum] = true
	}

	var dict ast.PdfDict
	if ref.ObjectNum > 0 {
		dict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if dict == nil {
		resolved, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || resolved == nil {
			return
		}
		dict = resolved
	}

	// Best-effort: an unreadable /Type yields an empty name; the switch
	// default handles it.
	typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
	switch typeName {
	case "Pages":
		kids, err := document.ResolveArray(ctx, doc, dict["Kids"])
		if err != nil {
			return
		}
		for _, kid := range kids {
			collectPageRefs(ctx, alloc, doc, kid, pages, seen, depth+1)
		}
	case "Page":
		*pages = append(*pages, annotPageEntry{ref: ref, dict: dict})
	default:
		if _, hasKids := dict["Kids"]; hasKids {
			kids, err := document.ResolveArray(ctx, doc, dict["Kids"])
			if err != nil {
				return
			}
			for _, kid := range kids {
				collectPageRefs(ctx, alloc, doc, kid, pages, seen, depth+1)
			}
		} else {
			*pages = append(*pages, annotPageEntry{ref: ref, dict: dict})
		}
	}
}

// pdfa1PermittedAnnotSubtypes lists the PDF 1.4 annotation types ISO 19005-1
// §6.5.2 permits. It mirrors compliance's pdfa1AnnotWhitelist, which is what
// raises the violation, so anything outside this set is forbidden in Part 1.
//
// Modelled as a PERMITTED set rather than a forbidden one deliberately. §6.5.2
// t1 has two clauses, "annotation types not defined in PDF Reference shall not
// be permitted" and "additionally, the FileAttachment, Sound and Movie types",
// and an earlier attempt enumerated forbidden subtypes and named only
// FileAttachment. That left Polygon, PolyLine, Caret, Watermark and Redact
// unrepaired. A whitelist cannot fail open that way.
//
// It is a copy because the compliance table is unexported, and the copy is the
// hazard: drift toward OVER-removal destroys content the author placed. A test
// pins the two sets together, and neither should change without the other.
//
// # Corpus evidence
//
//   - All corpora: the forbidden-subtype enumeration left Polygon, PolyLine,
//     Caret, Watermark and Redact unrepaired on 40 corpus cells which had
//     nothing else wrong with them.
var pdfa1PermittedAnnotSubtypes = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Highlight": true, "Underline": true,
	"Squiggly": true, "StrikeOut": true, "Stamp": true, "Ink": true,
	"Popup": true, "Widget": true, "PrinterMark": true, "TrapNet": true,
}

// pdfa23PermittedAnnotSubtypes mirrors compliance's annotWhitelistPart23
// (ISO 19005-2/3 §6.3.1). It is NOT a superset of the part-4 set and not a
// subset of it: part 2/3 permits FileAttachment and forbids Projection, and
// part 4 does the reverse.
var pdfa23PermittedAnnotSubtypes = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"FileAttachment": true, "Widget": true, "PrinterMark": true, "TrapNet": true,
	"Watermark": true, "Redact": true,
}

// pdfa4PermittedAnnotSubtypes mirrors compliance's annotWhitelistPart4
// (ISO 19005-4 §6.3.1): the part-2/3 set with FileAttachment dropped and
// Projection added.
var pdfa4PermittedAnnotSubtypes = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"Widget": true, "PrinterMark": true, "TrapNet": true, "Watermark": true,
	"Redact": true, "Projection": true,
}

// The two conformance levels of part 4 differ from it by exactly one addition
// each, which is how compliance states them and how the set difference measures
// them. Writing the deltas rather than two more near-identical literals keeps a
// future divergence visible instead of buried in twenty repeated names.
var (
	pdfa4ePermittedAnnotSubtypes = annotSubtypesWith(pdfa4PermittedAnnotSubtypes, "3D", "RichMedia")
	pdfa4fPermittedAnnotSubtypes = annotSubtypesWith(pdfa4PermittedAnnotSubtypes, "FileAttachment")
)

// annotSubtypesWith returns a copy of base with extra added, leaving base
// untouched so the tables cannot alias one another.
func annotSubtypesWith(base map[ast.PdfName]bool, extra ...ast.PdfName) map[ast.PdfName]bool {
	out := make(map[ast.PdfName]bool, len(base)+len(extra))
	maps.Copy(out, base)
	for _, name := range extra {
		out[name] = true
	}
	return out
}

// permittedAnnotSubtypesFor returns the annotation subtypes the profile permits,
// or nil when the profile is not PDF/A and the stricter check does not apply.
//
// The selection mirrors compliance's checkForbiddenAnnotType, which is what
// raises the violation. [compliance.PDFALevel] returns the identifier's raw last
// byte, so bare "PDF/A-4" yields '4' rather than an absent level: the default
// arm has to catch it, exactly as compliance's own level switches do.
func permittedAnnotSubtypesFor(profile string) map[ast.PdfName]bool {
	switch compliance.PDFAPart(profile) {
	case 1:
		return pdfa1PermittedAnnotSubtypes
	case 2, 3:
		return pdfa23PermittedAnnotSubtypes
	case 4:
		switch compliance.PDFALevel(profile) {
		case 'e':
			return pdfa4ePermittedAnnotSubtypes
		case 'f':
			return pdfa4fPermittedAnnotSubtypes
		default:
			return pdfa4PermittedAnnotSubtypes
		}
	}
	return nil
}

// annotSubtypeRemoved reports whether [fixAnnotArray] removes an annotation of
// subtype from a run at profile with opts.
//
// Each PDF/A part permits a different set of annotation types: part 2/3 allows
// FileAttachment and forbids Projection, part 4 does the reverse, and part 1 is
// narrower than both. Removing an annotation destroys content the author
// placed, so the profile-exact check is gated like every other repair that
// cannot preserve what it touches. A nil set means the profile is not PDF/A,
// leaving only the generic check.
//
// A subtype the profile's own table permits is kept in both modes. The two
// generic terms serve every part and level at once, so they forbid 3D and
// RichMedia, which ISO 19005-4 6.3.1 permits at conformance level E (Annex B):
// without this exemption a PDF/A-4e run deleted them whenever any other
// annotation violation brought the fixer in.
func annotSubtypeRemoved(subtype ast.PdfName, opts Options, profile string) bool {
	strictSubtypes := permittedAnnotSubtypesFor(profile)
	if strictSubtypes[subtype] {
		return false
	}
	strict := !opts.PreserveContent && strictSubtypes != nil
	return forbiddenAnnotSubtypes[subtype] || !permittedAnnotSubtypes[subtype] || strict
}

// fixAnnotArray processes each annotation in the array, returning fixes and
// whether any annotations were removed (requiring the array to be rebuilt).
func fixAnnotArray(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, annots ast.PdfArray, pageNum int, opts Options, profile string) ([]string, bool) {
	var fixes []string
	removed := false

	for i := len(annots) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			break
		}

		annotObj := annots[i]
		var annotRef ast.PdfIndirectRef
		if r, ok := annotObj.(ast.PdfIndirectRef); ok {
			annotRef = r
		}

		var annotDict ast.PdfDict
		if annotRef.ObjectNum > 0 {
			annotDict = resolveFromAllocOrDoc(ctx, alloc, doc, annotRef)
		}
		if annotDict == nil {
			resolved, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || resolved == nil {
				continue
			}
			annotDict = resolved
		}

		// Best-effort: an unreadable /Subtype yields an empty name, handled below.
		subtype, _ := document.ResolveName(ctx, doc, annotDict["Subtype"])

		// Remove forbidden or non-standard annotation subtypes
		// (PDF/A-2/3/4 §6.3.1, PDF/A-1 §6.5.2). Mark the slot nil here;
		// the caller's rebuildAnnotsArray skips nil entries when
		// rewriting the array.
		if annotSubtypeRemoved(subtype, opts, profile) {
			annots[i] = nil
			removed = true
			// Null the annotation object as well. Dropping it from /Annots
			// alone leaves the object live. A Rendition /Screen annotation
			// carries its MediaClip embedded-file filespec inline, and the
			// embedded-file requirements (ISO 19005-3 §6.8, 19005-4 §6.9)
			// apply to every /Type /Filespec in the file, not just those
			// reachable through /Annots, so an orphaned object still fails
			// them. Annotation rules are /Annots-array-scoped, so the nulled
			// object is invisible to them.
			if annotRef.ObjectNum > 0 {
				alloc.AllocAt(annotRef.ObjectNum, ast.PdfNull{})
			}
			fixes = append(fixes, fmt.Sprintf("removed forbidden %s annotation from page %d", subtype, pageNum))
			continue
		}

		changed := false
		newAnnot := make(ast.PdfDict, len(annotDict))
		maps.Copy(newAnnot, annotDict)

		isWidget := subtype == "Widget"

		// Remove /A and /AA from widget annotations (sections 6.4.1, 6.6.1, 6.6.2).
		if isWidget {
			if _, hasA := newAnnot["A"]; hasA {
				delete(newAnnot, "A")
				changed = true
				fixes = append(fixes, fmt.Sprintf("removed /A from widget annotation on page %d", pageNum))
			}
			if _, hasAA := newAnnot["AA"]; hasAA {
				delete(newAnnot, "AA")
				changed = true
				fixes = append(fixes, fmt.Sprintf("removed /AA from widget annotation on page %d", pageNum))
			}
		} else {
			// For non-widget annotations, restrict AA keys to permitted set (section 6.6.3).
			if aaObj, hasAA := newAnnot["AA"]; hasAA {
				aaDict, err := document.ResolveDict(ctx, doc, aaObj)
				if err == nil && aaDict != nil {
					cleaned := cleanAADict(aaDict)
					if cleaned != nil {
						newAnnot["AA"] = cleaned
						changed = true
						fixes = append(fixes, fmt.Sprintf("removed non-permitted AA keys from annotation on page %d", pageNum))
					}
				}
			}
		}

		if changed {
			if annotRef.ObjectNum > 0 {
				alloc.AllocAt(annotRef.ObjectNum, newAnnot)
			} else {
				// ISO 32000-1, 12.5.2 does not require an annotation to be an
				// indirect object. One written inline has no object number to
				// address, so the repair was reported and thrown away: a widget
				// kept its forbidden /A and /AA while litho printed "removed
				// /AA from widget annotation" on every fixpoint round. Replace
				// it in the array and let the caller write whichever object
				// owns that array.
				annots[i] = newAnnot
				removed = true
			}
		}
	}

	return fixes, removed
}

// cleanAADict returns a new AA dict with only permitted keys, or nil if no
// changes were needed.
func cleanAADict(aa ast.PdfDict) ast.PdfDict {
	needsCleaning := false
	for k := range aa {
		if !permittedAAKeys[k] {
			needsCleaning = true
			break
		}
	}
	if !needsCleaning {
		return nil
	}

	cleaned := make(ast.PdfDict, len(aa))
	for k, v := range aa {
		if permittedAAKeys[k] {
			cleaned[k] = v
		}
	}
	return cleaned
}

// rebuildAnnotsArray filters out nil entries from the array after annotation removal.
func rebuildAnnotsArray(annots ast.PdfArray) ast.PdfArray {
	result := make(ast.PdfArray, 0, len(annots))
	for _, a := range annots {
		if a != nil {
			result = append(result, a)
		}
	}
	return result
}
