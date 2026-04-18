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

func matchPageTabs(v compliance.Violation) bool {
	return descriptionContains(v, "Tabs entry") ||
		descriptionContains(v, "Tabs key") ||
		descriptionContains(v, "Tabs value")
}

// fixPageTabs adds a /Tabs /S entry to every page that contains annotations
// but lacks /Tabs. PDF/UA-1 §7.18.3 and PDF/UA-2 §8.9.3.3 require this for
// keyboard navigation in structure (tab) order.
func fixPageTabs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var count int
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("fixing Tabs on page %d: %w", i+1, err)
		}
		if _, hasAnnots := page.Dict["Annots"]; !hasAnnots {
			continue
		}
		if _, hasTabs := page.Dict["Tabs"]; hasTabs {
			continue
		}

		newDict := make(ast.PdfDict, len(page.Dict)+1)
		maps.Copy(newDict, page.Dict)
		newDict["Tabs"] = ast.PdfName("S")
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
		count++
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added /Tabs /S to %d page(s)", count)}, nil
}
