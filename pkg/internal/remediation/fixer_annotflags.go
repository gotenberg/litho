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

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil || len(annots) == 0 {
			continue
		}

		for _, annotObj := range annots {
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
			}
			count++
		}
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added /F flag to %d annotations", count)}, nil
}
