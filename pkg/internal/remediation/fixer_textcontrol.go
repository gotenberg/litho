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

// matchTextControl matches ISO 19005-2 §6.2.11.8 and ISO 19005-4
// §6.2.10.9 violations: text-showing operators render the .notdef
// glyph because the character code resolves to a missing or undefined
// entry in the font's charset.
func matchTextControl(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.10.9", "6.2.11.8") {
		return false
	}
	return descriptionContains(v, ".notdef") || contextContains(v, ".notdef")
}

// fixTextControl strips ASCII control bytes (0x00-0x1F excluding TAB,
// LF, CR) from every text-showing operator in every page's content
// stream. These bytes typically map to .notdef in standard font
// encodings and are almost never intended glyph references; removing
// them eliminates the violation without affecting legitimate text.
func fixTextControl(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	// Dropping bytes from Tj/TJ operands removes whatever glyph those
	// codes mapped to. That's exactly how we fix .notdef references,
	// but it is still content loss; gate behind PreserveContent.
	if opts.PreserveContent {
		return nil, nil
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	total := 0
	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contentsObj, ok := page.Dict["Contents"]
		if !ok {
			continue
		}
		ops, err := content.ParsePageContent(ctx, doc, page.Dict)
		if err != nil {
			continue
		}
		removed, modified := stripControlBytesFromOps(ops)
		if !modified {
			continue
		}
		total += removed
		filtered := filterOps(ops)
		newData := serializeOps(filtered)
		if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum+1); err != nil {
			return nil, fmt.Errorf("replacing content stream on page %d: %w", pageNum+1, err)
		}
	}
	if total == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("stripped %d control-byte(s) from text operators", total)}, nil
}

// stripControlBytesFromOps mutates the operand slices of every
// text-showing operator (Tj, TJ, ', ") in place, removing bytes that
// fall in the ASCII control range 0x00-0x1F except TAB/LF/CR.
// Operators whose entire operand becomes empty are cleared so the
// serialiser drops them. Returns the count of removed bytes plus a
// flag indicating whether any op changed.
func stripControlBytesFromOps(ops []content.Operation) (int, bool) {
	removed := 0
	modified := false
	for i := range ops {
		op := ops[i].Operator
		if op != "Tj" && op != "TJ" && op != "'" && op != "\"" {
			continue
		}
		for j, operand := range ops[i].Operands {
			switch v := operand.(type) {
			case ast.PdfString:
				cleaned, n := stripControlString(v)
				if n == 0 {
					continue
				}
				removed += n
				modified = true
				ops[i].Operands[j] = cleaned
				if len(cleaned) == 0 {
					ops[i].Operator = ""
				}
			case ast.PdfArray:
				cleanedArr, n := stripControlTJArray(v)
				if n == 0 {
					continue
				}
				removed += n
				modified = true
				ops[i].Operands[j] = cleanedArr
				if isEmptyTJArray(cleanedArr) {
					ops[i].Operator = ""
				}
			}
		}
	}
	return removed, modified
}

// stripControlString returns a copy of s with ASCII control bytes
// removed. The returned count reports how many bytes were stripped.
func stripControlString(s ast.PdfString) (ast.PdfString, int) {
	src := []byte(s)
	dst := make([]byte, 0, len(src))
	removed := 0
	for _, b := range src {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			removed++
			continue
		}
		dst = append(dst, b)
	}
	if removed == 0 {
		return s, 0
	}
	return ast.PdfString(dst), removed
}

// stripControlTJArray cleans every PdfString inside a TJ operand array.
func stripControlTJArray(arr ast.PdfArray) (ast.PdfArray, int) {
	out := make(ast.PdfArray, 0, len(arr))
	removed := 0
	for _, elem := range arr {
		if s, ok := elem.(ast.PdfString); ok {
			cleaned, n := stripControlString(s)
			removed += n
			if len(cleaned) == 0 {
				continue
			}
			out = append(out, cleaned)
			continue
		}
		out = append(out, elem)
	}
	return out, removed
}

// isEmptyTJArray reports whether arr holds no text content after
// cleaning: only spacing adjustments (numbers) remain.
func isEmptyTJArray(arr ast.PdfArray) bool {
	for _, elem := range arr {
		if _, ok := elem.(ast.PdfString); ok {
			return false
		}
	}
	return true
}
