package remediation

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchNeedAppearances(v compliance.Violation) bool {
	return descriptionContains(v, "NeedAppearances")
}

func fixNeedAppearances(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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
	if ref, ok := acroFormObj.(ast.PdfIndirectRef); ok {
		acroForm = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if acroForm == nil {
		resolved, err := document.ResolveDict(ctx, doc, acroFormObj)
		if err != nil || resolved == nil {
			return nil, nil
		}
		acroForm = resolved
	}

	newAcroForm := make(ast.PdfDict, len(acroForm))
	maps.Copy(newAcroForm, acroForm)
	delete(newAcroForm, "NeedAppearances")

	if ref, ok := acroFormObj.(ast.PdfIndirectRef); ok {
		alloc.AllocAt(ref.ObjectNum, newAcroForm)
	} else {
		// AcroForm is inline in catalog — update catalog.
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["AcroForm"] = newAcroForm
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return []string{"removed /NeedAppearances from AcroForm"}, nil
}

// resolveFromAllocOrDoc checks the allocator for a pending version, falling
// back to the document. Shared by all fixers.
func resolveFromAllocOrDoc(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) ast.PdfDict {
	if obj, ok := alloc.Pending()[ref.ObjectNum]; ok {
		if d, ok := obj.(ast.PdfDict); ok {
			return d
		}
	}
	d, err := document.ResolveDict(ctx, doc, ref)
	if err != nil {
		return nil
	}
	return d
}
