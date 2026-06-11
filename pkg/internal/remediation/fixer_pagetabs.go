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
	// Match by clause, not Rule.Description: the PDF/UA-1 rule (§7.18.3) and
	// PDF/UA-2 rule (§8.9.3.3) word the requirement differently, so a
	// description-substring match silently skipped every PDF/UA-1 document.
	return clausePrefix(v, "7.18.3", "8.9.3.3")
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
		pageDict := livePageDict(ctx, alloc, doc, page)
		if _, hasAnnots := pageDict["Annots"]; !hasAnnots {
			continue
		}
		if _, hasTabs := pageDict["Tabs"]; hasTabs {
			continue
		}

		newDict := make(ast.PdfDict, len(pageDict)+1)
		maps.Copy(newDict, pageDict)
		newDict["Tabs"] = ast.PdfName("S")
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
		count++
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added /Tabs /S to %d page(s)", count)}, nil
}
