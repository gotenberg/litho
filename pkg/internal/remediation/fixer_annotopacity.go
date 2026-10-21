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

// matchAnnotOpacity matches the PDF/A-1 annotation constant-opacity violation
// (ISO 19005-1 §6.5.3 t01: "An annotation dictionary shall not contain the CA
// key with a value other than 1.0"). It keys on the rule description rather than
// the clause, which §6.5.3 shares with the /F-flag and appearance checks. The
// rule exists only for PDF/A-1, so the fixer never runs for later parts that
// permit transparency.
func matchAnnotOpacity(v compliance.Violation) bool {
	return descriptionContains(v, "annotation") && descriptionContains(v, "CA key")
}

// fixAnnotOpacity removes a non-conformant /CA (constant opacity) from every
// annotation, so the value reverts to its 1.0 default (ISO 32000-1 Table 164),
// which ISO 19005-1 §6.5.3 requires. The key is dropped only when present and
// not already 1.0, matching the validator's [compliance] check exactly; an
// absent or 1.0 /CA is left untouched. Removing the key rather than writing 1.0
// is the conventional repair: the default already conforms, so the resulting
// document carries no redundant entry. Only indirect annotations are rewritten;
// an inline annotation has no object number to patch in place.
func fixAnnotOpacity(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
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
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool))

	var count int
	for _, pe := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// The annotations fixer runs first and writes the /Annots it filtered
		// at the array's own object number: read as the file wrote it, the
		// array still lists what it removed.
		for _, annotObj := range liveArray(ctx, alloc, doc, pe.dict["Annots"]) {
			annotRef, ok := annotObj.(ast.PdfIndirectRef)
			if !ok || annotRef.ObjectNum == 0 {
				// An inline annotation cannot be rewritten in place.
				continue
			}
			annotDict := liveAnnotation(ctx, alloc, doc, annotRef)
			if annotDict == nil {
				continue
			}

			caObj, hasCA := annotDict["CA"]
			if !hasCA {
				continue
			}
			resolved, err := document.ResolveObject(ctx, doc, caObj)
			if err != nil {
				// An unreadable /CA is left alone rather than guessed at.
				doc.Logger.Debug("remediation: annot-opacity: cannot resolve /CA",
					"obj", annotRef.ObjectNum, "error", err)
				continue
			}
			if document.Float(resolved) == 1.0 {
				continue
			}

			newAnnot := make(ast.PdfDict, len(annotDict))
			maps.Copy(newAnnot, annotDict)
			delete(newAnnot, "CA")
			alloc.AllocAt(annotRef.ObjectNum, newAnnot)
			count++
		}
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("removed non-conformant /CA from %d annotation(s)", count)}, nil
}
