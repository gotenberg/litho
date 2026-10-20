package remediation

import (
	"context"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// pdfA1StringLimit is the 65535-byte cap imposed by ISO 19005-1 §6.1.12
// test 3.
const pdfA1StringLimit = 65535

// pdfA2StringLimit is the 32767-byte cap imposed by ISO 19005-2/3/4
// §6.1.13 test 3.
const pdfA2StringLimit = 32767

// matchStringLimit matches PDF/A string-length violations for both the
// 65535-byte (part 1) and 32767-byte (parts 2-4) caps.
func matchStringLimit(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.12", "6.1.13") {
		return false
	}
	return contextContains(v, "string length") && contextContains(v, "exceeds")
}

// fixStringLimit truncates every PdfString in every indirect object
// and every page content stream so that its length fits the profile
// cap. Truncation is destructive but unavoidable: the spec permits no
// alternative for over-long strings. The cap is picked from the active
// profile.
func fixStringLimit(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	// Truncating a string discards its tail. Run only when the caller
	// has explicitly accepted content loss.
	if opts.PreserveContent {
		return nil, nil
	}
	limit := pdfA2StringLimit
	if profile == "PDF/A-1a" || profile == "PDF/A-1b" {
		limit = pdfA1StringLimit
	}

	truncDict := func(d ast.PdfDict) (ast.PdfDict, bool) {
		return truncStringsInDict(d, limit)
	}
	truncArr := func(a ast.PdfArray) (ast.PdfArray, bool) {
		return truncStringsInArray(a, limit)
	}
	count, err := walkIndirectObjects(ctx, alloc, doc, truncDict, truncArr)
	if err != nil {
		return nil, err
	}

	// Content-stream operand strings are rewritten through the operations
	// of the page ([repairContentOps]).
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err == nil {
		// Best-effort: a page-tree resolve failure yields an empty slice;
		// the loop then no-ops.
		pages, _ := document.ResolvePages(ctx, doc, catalog)
		sharing := pageContentSharing(ctx, alloc, doc, pages)
		for pageNum, page := range pages {
			if ctx.Err() != nil {
				break
			}
			data, contentsObj, err := livePageContentData(ctx, alloc, doc, page)
			if err != nil || contentsObj == nil {
				continue
			}
			newData, n, err := repairContentOps(ctx, data, func() opRepair { return truncatedOperandStrings(limit) })
			if err != nil {
				doc.Logger.Debug("remediation: cannot rewrite a content stream for its long strings",
					"page", pageNum+1, "error", err)
				continue
			}
			if n == 0 {
				continue
			}
			if err := replaceContentStream(ctx, alloc, doc, sharing, contentsObj, newData, pageNum+1); err != nil {
				return nil, fmt.Errorf("replacing content stream on page %d: %w", pageNum+1, err)
			}
			count++
		}
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("truncated over-long strings in %d object(s)/stream(s)", count)}, nil
}

// truncatedOperandStrings returns the repair of [fixStringLimit] for the
// operations of a content stream: every string operand longer than limit is
// cut to its first limit bytes, and every truncation counts as an edit.
func truncatedOperandStrings(limit int) opRepair {
	return func(op content.Operation) ([]content.Operation, int) {
		var operands []ast.PdfObject
		edits := 0
		for j, operand := range op.Operands {
			s, ok := operand.(ast.PdfString)
			if !ok || len(s) <= limit {
				continue
			}
			if operands == nil {
				operands = slices.Clone(op.Operands)
			}
			operands[j] = s[:limit]
			edits++
		}
		if edits == 0 {
			return []content.Operation{op}, 0
		}
		return []content.Operation{{Operator: op.Operator, Operands: operands}}, edits
	}
}

// truncStringsInDict walks d and truncates any PdfString whose length
// exceeds limit. Recurses into nested arrays and dicts. Returns a fresh
// dict plus a flag when anything changed.
//
//nolint:dupl // Dict and array walkers operate on distinct types.
func truncStringsInDict(d ast.PdfDict, limit int) (ast.PdfDict, bool) {
	newDict := make(ast.PdfDict, len(d))
	changed := false
	for k, v := range d {
		switch val := v.(type) {
		case ast.PdfString:
			if len(val) > limit {
				newDict[k] = val[:limit]
				changed = true
				continue
			}
		case ast.PdfDict:
			if sub, c := truncStringsInDict(val, limit); c {
				newDict[k] = sub
				changed = true
				continue
			}
		case ast.PdfArray:
			if sub, c := truncStringsInArray(val, limit); c {
				newDict[k] = sub
				changed = true
				continue
			}
		}
		newDict[k] = v
	}
	if !changed {
		return nil, false
	}
	return newDict, true
}

// truncStringsInArray mirrors truncStringsInDict for arrays.
//
//nolint:dupl // See truncStringsInDict.
func truncStringsInArray(a ast.PdfArray, limit int) (ast.PdfArray, bool) {
	newArr := make(ast.PdfArray, len(a))
	changed := false
	for i, v := range a {
		switch val := v.(type) {
		case ast.PdfString:
			if len(val) > limit {
				newArr[i] = val[:limit]
				changed = true
				continue
			}
		case ast.PdfDict:
			if sub, c := truncStringsInDict(val, limit); c {
				newArr[i] = sub
				changed = true
				continue
			}
		case ast.PdfArray:
			if sub, c := truncStringsInArray(val, limit); c {
				newArr[i] = sub
				changed = true
				continue
			}
		}
		newArr[i] = v
	}
	if !changed {
		return nil, false
	}
	return newArr, true
}
