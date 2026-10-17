package remediation

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchXFA(v compliance.Violation) bool {
	return descriptionContains(v, "XFA")
}

// fixXFA removes the /XFA entry from the interactive form dictionary, whether
// that dictionary is indirect or written inline in the catalog.
//
// # Corpus evidence
//
//   - All corpora: 11 of the 20 files carrying an interactive form dictionary
//     write it inline.
func fixXFA(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	acroFormObj, ok := catalogDict["AcroForm"]
	if !ok {
		return nil, nil
	}

	var acroForm ast.PdfDict
	var acroFormRef ast.PdfIndirectRef
	if ref, ok := acroFormObj.(ast.PdfIndirectRef); ok {
		acroFormRef = ref
		acroForm = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if acroForm == nil {
		resolved, err := document.ResolveDict(ctx, doc, acroFormObj)
		if err != nil || resolved == nil {
			return nil, nil //nolint:nilerr // An unresolvable /AcroForm is null (ISO 32000-1, 7.3.10): no XFA to remove.
		}
		acroForm = resolved
	}

	if _, ok := acroForm["XFA"]; !ok {
		return nil, nil
	}

	newAcroForm := make(ast.PdfDict, len(acroForm))
	maps.Copy(newAcroForm, acroForm)
	// ISO 19005-2/3 §6.4.2: an interactive form shall not contain /XFA.
	delete(newAcroForm, "XFA")

	if acroFormRef.ObjectNum > 0 {
		alloc.AllocAt(acroFormRef.ObjectNum, newAcroForm)
	} else {
		// ISO 32000-1, 12.7.2 does not require the interactive form dictionary
		// to be an indirect object, and producers commonly write it inline.
		// Dropping the rewrite here reported a repair that was never made: /XFA
		// survived, the violation persisted, and the fixpoint loop re-reported
		// the same repair on every one of its rounds.
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["AcroForm"] = newAcroForm
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return []string{"removed /XFA from AcroForm"}, nil
}
