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

func fixXFA(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
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
	var acroFormRef ast.PdfIndirectRef
	if ref, ok := acroFormObj.(ast.PdfIndirectRef); ok {
		acroFormRef = ref
		acroForm = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if acroForm == nil {
		resolved, err := document.ResolveDict(ctx, doc, acroFormObj)
		if err != nil || resolved == nil {
			return nil, nil
		}
		acroForm = resolved
	}

	if _, ok := acroForm["XFA"]; !ok {
		return nil, nil
	}

	newAcroForm := make(ast.PdfDict, len(acroForm))
	maps.Copy(newAcroForm, acroForm)
	delete(newAcroForm, "XFA")

	if acroFormRef.ObjectNum > 0 {
		alloc.AllocAt(acroFormRef.ObjectNum, newAcroForm)
	}

	return []string{"removed /XFA from AcroForm"}, nil
}
