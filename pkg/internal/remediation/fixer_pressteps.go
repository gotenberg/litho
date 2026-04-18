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

func matchPresSteps(v compliance.Violation) bool {
	return descriptionContains(v, "PresSteps")
}

func fixPresSteps(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var notes []string

	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("fixing PresSteps on page %d: %w", i+1, err)
		}

		if _, ok := page.Dict["PresSteps"]; !ok {
			continue
		}

		newDict := make(ast.PdfDict, len(page.Dict))
		maps.Copy(newDict, page.Dict)
		delete(newDict, "PresSteps")

		alloc.AllocAt(page.Ref.ObjectNum, newDict)
		notes = append(notes, fmt.Sprintf("removed /PresSteps from page %d", i+1))
	}

	return notes, nil
}
