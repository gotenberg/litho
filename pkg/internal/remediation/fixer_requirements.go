package remediation

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchRequirements(v compliance.Violation) bool {
	return descriptionContains(v, "Requirements key")
}

func fixRequirements(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}

	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	if _, ok := catalogDict["Requirements"]; !ok {
		return nil, nil
	}

	newCatalog := make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	// ISO 19005-2/3 §6.11 (part 4 §6.12): the catalog shall not contain a /Requirements entry.
	delete(newCatalog, "Requirements")

	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return []string{"removed /Requirements from catalog"}, nil
}
