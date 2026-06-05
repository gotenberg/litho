package remediation

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// matchUnknownOps matches ISO 19005-2/4 §6.2.2 content-stream violations
// that report operators not defined in ISO 32000. These are dead bytes at
// the viewer level, so stripping them is content-preserving.
func matchUnknownOps(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.2") {
		return false
	}
	return descriptionContains(v, "undefined operator") ||
		descriptionContains(v, "not defined in ISO") ||
		descriptionContains(v, "unknown operator") ||
		descriptionContains(v, "operator \"")
}

// fixUnknownOps strips operators absent from [validOperators] from every
// page content stream and every Form XObject referenced through page
// Resources. Viewers ignore these tokens per ISO 32000 §A.2, so removal
// has no visible effect and only eliminates the PDF/A violation.
func fixUnknownOps(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var total int
	visited := make(map[int]bool)
	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := stripUnknownFromStream(ctx, alloc, doc, page, pageNum+1)
		if err != nil {
			return nil, err
		}
		total += n
		total += stripUnknownInResources(ctx, alloc, doc, page.Resources, visited)
	}

	if total == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("removed %d unknown content-stream operator(s)", total)}, nil
}

// stripUnknownFromStream parses a page's live content (alloc-first, so an
// earlier fixer's rewrite is seen), strips operators outside [validOperators],
// and writes the result back through [replaceContentStream]. Returns the number
// of operators removed.
func stripUnknownFromStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, pageNum int) (int, error) {
	ops, contentsObj, err := livePageContentOps(ctx, alloc, doc, page)
	if err != nil || contentsObj == nil {
		return 0, nil
	}
	removed := 0
	modified := false
	for i, op := range ops {
		if op.Operator == "" || op.Operator == "BX" || op.Operator == "EX" {
			continue
		}
		if !validOperators[op.Operator] {
			ops[i].Operator = ""
			removed++
			modified = true
		}
	}
	if !modified {
		return 0, nil
	}
	filtered := filterOps(ops)
	newData := serializeOps(filtered)
	if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum); err != nil {
		return removed, fmt.Errorf("replacing content stream on page %d: %w", pageNum, err)
	}
	return removed, nil
}

// stripUnknownInResources recurses into a page's /Resources/XObject
// entries to process Form XObjects that may hold their own content
// streams with bad operators.
func stripUnknownInResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, visited map[int]bool) int {
	if resources == nil {
		return 0
	}
	xoObj, ok := resources["XObject"]
	if !ok {
		return 0
	}
	xoDict, err := document.ResolveDict(ctx, doc, xoObj)
	if err != nil || xoDict == nil {
		return 0
	}
	total := 0
	for _, entry := range xoDict {
		ref, ok := entry.(ast.PdfIndirectRef)
		if !ok || ref.ObjectNum == 0 || visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		rawObj, err := doc.Resolve(ctx, ref)
		if err != nil {
			continue
		}
		stream, ok := rawObj.(ast.PdfStream)
		if !ok {
			continue
		}
		subtype, _ := stream.Dict["Subtype"].(ast.PdfName)
		if subtype != "Form" {
			continue
		}

		data, err := stream.Bytes(ctx)
		if err != nil {
			continue
		}
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			continue
		}
		removed := 0
		modified := false
		for i, op := range ops {
			if op.Operator == "" || op.Operator == "BX" || op.Operator == "EX" {
				continue
			}
			if !validOperators[op.Operator] {
				ops[i].Operator = ""
				removed++
				modified = true
			}
		}
		if !modified {
			continue
		}
		filtered := filterOps(ops)
		newData := serializeOps(filtered)
		newDict := make(ast.PdfDict, len(stream.Dict)+1)
		for k, v := range stream.Dict {
			if k == "Length" {
				continue
			}
			newDict[k] = v
		}
		newDict["Length"] = ast.PdfInt(len(newData))
		alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, newData))
		total += removed

		// Recurse into nested Form XObjects.
		if nestedRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && nestedRes != nil {
			total += stripUnknownInResources(ctx, alloc, doc, nestedRes, visited)
		}
	}
	return total
}
