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

	// Content-stream operand strings are rewritten in-place via the
	// parser → serializer round trip.
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err == nil {
		pages, _ := document.ResolvePages(ctx, doc, catalog)
		for pageNum, page := range pages {
			if ctx.Err() != nil {
				break
			}
			contentsObj, ok := page.Dict["Contents"]
			if !ok {
				continue
			}
			ops, err := content.ParsePageContent(ctx, doc, page.Dict)
			if err != nil {
				continue
			}
			modified := false
			for i := range ops {
				for j, operand := range ops[i].Operands {
					if s, ok := operand.(ast.PdfString); ok && len(s) > limit {
						ops[i].Operands[j] = s[:limit]
						modified = true
					}
				}
			}
			if !modified {
				continue
			}
			newData := serializeOps(ops)
			if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum+1); err != nil {
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
