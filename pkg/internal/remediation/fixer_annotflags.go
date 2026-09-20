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

func matchAnnotFlags(v compliance.Violation) bool {
	return clausePrefix(v, "6.3.2") ||
		(descriptionContains(v, "annotation") && descriptionContains(v, "F key"))
}

// fixAnnotFlags ensures every annotation (except Popup) has an /F key with the
// Print flag set, per ISO 19005-2, clause 6.3.2.
func fixAnnotFlags(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)

	var count int
	for _, pe := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		annotsObj, ok := pe.dict["Annots"]
		if !ok {
			continue
		}

		// Alloc-aware, and the helper matches the object KIND: liveArray for an
		// array, since the resolveFromAllocOrDoc calls elsewhere here return
		// dicts. This value is rebuilt wholesale by [installInlineAnnots] and
		// written back at the array's own object number, so reading it from
		// the document discards a peer's pending edit. annotations (registry
		// 121) nils forbidden annotation slots and compacts the array before
		// writing it at that same object, and annot-flags is registry 124, so
		// a stale copy here restores annotations that were deliberately
		// removed.
		annots := liveArray(ctx, alloc, doc, annotsObj)
		if len(annots) == 0 {
			continue
		}

		var inlineFixed map[int]ast.PdfDict

		for i, annotObj := range annots {
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

			// Best-effort: an unreadable /Subtype yields an empty name,
			// which is not "Popup".
			subtype, _ := document.ResolveName(ctx, doc, annotDict["Subtype"])
			if subtype == "Popup" {
				continue
			}

			// Required flags per PDF/A: Print=1, Hidden=0, Invisible=0,
			// NoView=0, ToggleNoView=0.
			const (
				printFlag     = 1 << 2 // bit 3
				hiddenFlag    = 1 << 1 // bit 2
				invisibleFlag = 1 << 0 // bit 1
				noViewFlag    = 1 << 5 // bit 6
				toggleNoView  = 1 << 8 // bit 9
				forbiddenMask = hiddenFlag | invisibleFlag | noViewFlag | toggleNoView
			)

			currentF := 0
			if fObj, hasF := annotDict["F"]; hasF {
				if fInt, ok := fObj.(ast.PdfInt); ok {
					currentF = int(fInt)
				}
				// Already correct: Print set, no forbidden flags.
				if currentF&printFlag != 0 && currentF&forbiddenMask == 0 {
					continue
				}
			}

			newF := (currentF | printFlag) &^ forbiddenMask
			newAnnot := make(ast.PdfDict, len(annotDict)+1)
			maps.Copy(newAnnot, annotDict)
			newAnnot["F"] = ast.PdfInt(newF)

			if annotRef.ObjectNum > 0 {
				alloc.AllocAt(annotRef.ObjectNum, newAnnot)
				count++
				continue
			}

			// ISO 32000-1, 12.5.2 does not require an annotation to be an
			// indirect object. One written inline in /Annots has no object
			// number to address, so writing only through the allocator dropped
			// the repair while still counting it: pdfjs bug1905623.pdf carries
			// an inline /Link whose /F flag was reported as added on every run
			// and never appeared in the output. Collect it and install it
			// through the array below.
			if inlineFixed == nil {
				inlineFixed = make(map[int]ast.PdfDict, 1)
			}
			inlineFixed[i] = newAnnot
		}

		count += installInlineAnnots(ctx, alloc, doc, pe, annotsObj, annots, inlineFixed)
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added /F flag to %d annotations", count)}, nil
}

// installInlineAnnots writes annotations that were repaired in place, returning
// how many landed.
//
// An inline annotation is addressed only through its page's /Annots array, so
// the array is rebuilt with the replacements and written to whichever object
// owns it: the array itself when indirect, the page otherwise. A page with no
// object number of its own cannot be written at all, and nothing is reported.
func installInlineAnnots(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pe annotPageEntry,
	annotsObj ast.PdfObject,
	annots ast.PdfArray,
	fixed map[int]ast.PdfDict,
) int {
	if len(fixed) == 0 {
		return 0
	}

	newArr := make(ast.PdfArray, len(annots))
	copy(newArr, annots)
	for i, dict := range fixed {
		if i < len(newArr) {
			newArr[i] = dict
		}
	}

	if annotsRef, isRef := annotsObj.(ast.PdfIndirectRef); isRef && annotsRef.ObjectNum > 0 {
		alloc.AllocAt(annotsRef.ObjectNum, newArr)
		return len(fixed)
	}

	if pe.ref.ObjectNum <= 0 {
		return 0
	}

	pageDict := resolveFromAllocOrDoc(ctx, alloc, doc, pe.ref)
	if pageDict == nil {
		pageDict = pe.dict
	}
	newPage := make(ast.PdfDict, len(pageDict))
	maps.Copy(newPage, pageDict)
	newPage["Annots"] = newArr
	alloc.AllocAt(pe.ref.ObjectNum, newPage)
	return len(fixed)
}
