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

// forbiddenAnnotSubtypes lists annotation subtypes that are never permitted
// in any PDF/A profile (parts 2+/4).
var forbiddenAnnotSubtypes = map[ast.PdfName]bool{
	"Sound": true, "Movie": true, "Screen": true, "3D": true,
}

// permittedAnnotSubtypes lists subtypes defined in ISO 32000-1 that are
// allowed by PDF/A-2/3/4 §6.3.1. Any subtype outside this set plus any
// entry in [forbiddenAnnotSubtypes] must be removed.
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
		"6.4.1", // widget /A or /AA
		"6.5.2", // catalog/page /AA (part 1)
		"6.5.3", // annotation flags (part 1)
		"6.6.2", // widget /AA
		"6.6.3", // non-permitted AA keys
	)
}

func fixAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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

	// Walk pages to fix annotations.
	pageFixes, err := fixPageAnnotations(ctx, alloc, doc, catalogDict)
	if err != nil {
		return nil, fmt.Errorf("fixing page annotations: %w", err)
	}
	fixes = append(fixes, pageFixes...)

	return fixes, nil
}

// annotPageEntry pairs a page dict with its indirect reference.
type annotPageEntry struct {
	ref  ast.PdfIndirectRef
	dict ast.PdfDict
}

// fixPageAnnotations walks the page tree and fixes annotation issues on each page.
func fixPageAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) ([]string, error) {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return nil, nil
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, 0)

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

		pageFixes, modified := fixAnnotArray(ctx, alloc, doc, annots, i+1)
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

const maxPageTreeDepth = 100

// collectPageRefs recursively collects page dicts with their indirect refs.
func collectPageRefs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, pages *[]annotPageEntry, depth int) {
	if ctx.Err() != nil || depth > maxPageTreeDepth {
		return
	}

	var ref ast.PdfIndirectRef
	if r, ok := obj.(ast.PdfIndirectRef); ok {
		ref = r
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
			collectPageRefs(ctx, alloc, doc, kid, pages, depth+1)
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
				collectPageRefs(ctx, alloc, doc, kid, pages, depth+1)
			}
		} else {
			*pages = append(*pages, annotPageEntry{ref: ref, dict: dict})
		}
	}
}

// fixAnnotArray processes each annotation in the array, returning fixes and
// whether any annotations were removed (requiring the array to be rebuilt).
func fixAnnotArray(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, annots ast.PdfArray, pageNum int) ([]string, bool) {
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
		if forbiddenAnnotSubtypes[subtype] || !permittedAnnotSubtypes[subtype] {
			annots[i] = nil
			removed = true
			// Null the annotation object as well. Dropping it from /Annots
			// alone leaves the object live, and veraPDF scans every object
			// for /Type /Filespec: a Rendition /Screen annotation carries its
			// MediaClip embedded-file filespec inline, so an orphaned object
			// still fails ISO 19005-3 §6.8 / 19005-4 §6.9 (verified on
			// PDF_UA-1__7.18.6.2-t01-fail-a remediated to A-2/3/4). Annotation
			// rules are /Annots-array-scoped, so the nulled object is invisible
			// to them.
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

		if changed && annotRef.ObjectNum > 0 {
			alloc.AllocAt(annotRef.ObjectNum, newAnnot)
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
