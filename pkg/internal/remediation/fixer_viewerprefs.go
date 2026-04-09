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

func matchViewerPrefs(v compliance.Violation) bool {
	return descriptionContains(v, "DisplayDocTitle") || descriptionContains(v, "ViewerPreferences")
}

func fixViewerPrefs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	// Get or create ViewerPreferences.
	var vpDict ast.PdfDict
	if vpObj, ok := catalogDict["ViewerPreferences"]; ok {
		if existing, err := document.ResolveDict(ctx, doc, vpObj); err == nil && existing != nil {
			vpDict = make(ast.PdfDict, len(existing)+1)
			maps.Copy(vpDict, existing)
		}
	}
	if vpDict == nil {
		vpDict = ast.PdfDict{}
	}
	vpDict["DisplayDocTitle"] = ast.PdfBool(true)

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["ViewerPreferences"] = vpDict
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return []string{"set /ViewerPreferences/DisplayDocTitle to true"}, nil
}
