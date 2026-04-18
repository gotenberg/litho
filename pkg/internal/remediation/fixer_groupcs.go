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

// matchGroupCS matches transparency /Group CS violations: mismatched
// blending CS vs OutputIntent, or a page with transparency that lacks
// any /Group entry at all. Covers ISO 19005-2/3 §6.2.10 and
// ISO 19005-4 §6.2.9 test 2.
func matchGroupCS(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.9", "6.2.10") {
		return false
	}
	if contextContains(v, "blending CS") && contextContains(v, "incompatible") {
		return true
	}
	// Contexts like "[page 1]" (no detail) flag a page that contains
	// transparency but is missing the /Group key altogether.
	return descriptionContains(v, "shall include the Group key") ||
		descriptionContains(v, "include the Group key")
}

// fixGroupCS handles two repair paths on transparency /Group dicts:
//
//  1. When a /Group has a /CS entry that conflicts with the document
//     OutputIntent, drop /CS so the renderer falls back to the intent
//     profile (ISO 32000-1 §11.6.6).
//  2. When a page that requires a /Group has none, synthesize a minimal
//     transparency group so the page is well-formed.
func fixGroupCS(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var count int
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		groupObj, hasGroup := page.Dict["Group"]
		if !hasGroup {
			newPage := make(ast.PdfDict, len(page.Dict)+1)
			maps.Copy(newPage, page.Dict)
			newPage["Group"] = ast.PdfDict{
				"Type": ast.PdfName("Group"),
				"S":    ast.PdfName("Transparency"),
				// /CS is required by ISO 19005-2 §6.2.10 test 2 when
				// the document lacks a matching PDF/A OutputIntent.
				// DeviceRGB is a safe default blending colorspace.
				"CS": ast.PdfName("DeviceRGB"),
			}
			if page.Ref.ObjectNum > 0 {
				alloc.AllocAt(page.Ref.ObjectNum, newPage)
				count++
			}
			continue
		}
		groupDict, err := document.ResolveDict(ctx, doc, groupObj)
		if err != nil || groupDict == nil {
			continue
		}
		if _, hasCS := groupDict["CS"]; !hasCS {
			continue
		}
		newGroup := make(ast.PdfDict, len(groupDict))
		maps.Copy(newGroup, groupDict)
		delete(newGroup, "CS")

		var groupRef ast.PdfIndirectRef
		if r, ok := groupObj.(ast.PdfIndirectRef); ok {
			groupRef = r
		}
		if groupRef.ObjectNum > 0 {
			alloc.AllocAt(groupRef.ObjectNum, newGroup)
		} else {
			newPage := make(ast.PdfDict, len(page.Dict))
			maps.Copy(newPage, page.Dict)
			newPage["Group"] = newGroup
			alloc.AllocAt(page.Ref.ObjectNum, newPage)
		}
		count++
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("normalised /Group on %d page(s)", count)}, nil
}
