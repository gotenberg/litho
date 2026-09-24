package remediation

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// pdfARealLimit is the absolute maximum real value permitted by ISO 19005-1
// §6.1.12 test 2 and ISO 19005-2/4 §6.1.13 test 2.
const pdfARealLimit = 32767.0

// pdfAIntLimit is the integer cap required by ISO 19005-2 §6.1.13 test 1
// and ISO 19005-4 §6.1.9 test 1 (2^31 - 1).
const pdfAIntLimit = 2147483647

// matchRealLimits matches PDF/A numeric-limit violations that originate
// from content streams. ISO 19005-1 §6.1.12 and ISO 19005-2/4 §6.1.13
// impose different caps on integers and reals; both overflows are
// repaired by the same pass, which clamps operands in place.
//
// # Corpus evidence
//
//   - All corpora: gating on contextContains("out of range") silently stopped
//     claiming 14 cells of 6.1.12 test 2.
func matchRealLimits(v compliance.Violation) bool {
	// The clause number alone is not the rule. ISO 19005-1 spells the ban on a
	// catalog /OCProperties entry at 6.1.13 test 1, where parts 2 and 3 spell
	// their numeric implementation limits. Without this exclusion the part-1
	// optional-content violation is claimed here, which marks it handled in the
	// preliminary residual while this fixer cannot touch it.
	//
	// Excluding that one rule rather than requiring a context substring: an
	// earlier attempt gated on contextContains("out of range") and silently
	// stopped claiming 6.1.12 test 2, whose violations carry a different
	// context. A positive context filter has to enumerate every phrasing the
	// checks emit; an exclusion only has to name the one rule that does not
	// belong here.
	if forbiddenOCPropertiesViolation(v.Rule) {
		return false
	}
	return clausePrefix(v, "6.1.12", "6.1.13")
}

// fixRealLimits clamps out-of-range numeric values in two places:
//
//  1. Every page-level content stream, whose operands live in a parse
//     tree the fixer rewrites in place.
//  2. Every indirect object's body. PDF/A §6.1.12 / §6.1.13 apply to
//     bare numeric literals anywhere in the file, not just page content.
//
// Clamping can shift a drawing coordinate, but the alternative is
// failing compliance; the spec caps the value regardless of intent.
func fixRealLimits(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var total int
	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ops, contentsObj, err := livePageContentOps(ctx, alloc, doc, page)
		if err != nil || contentsObj == nil {
			continue
		}

		modified := false
		for i := range ops {
			for j, operand := range ops[i].Operands {
				if clamped, changed := clampReal(operand); changed {
					ops[i].Operands[j] = clamped
					total++
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
	}

	// Sweep every indirect object's dict for scalar/array operands over
	// the limit. Stream bodies are not walked here: numeric literals
	// inside content streams of Form XObjects etc. are caught via the
	// page-content pass above plus the aggressive content-stream fixer.
	n, err := walkIndirectObjects(ctx, alloc, doc, clampDictReals, clampArrayReals)
	if err != nil {
		return nil, err
	}
	total += n

	if total == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("clamped %d out-of-range numeric value(s)", total)}, nil
}

// clampDictReals clamps PdfReal values to ±[pdfARealLimit] and PdfInt
// values to ±[pdfAIntLimit]. Small integers are untouched so /Length,
// /Size, offsets etc. stay intact; only values that exceed the 32-bit
// signed range (which PDF/A forbids outright) get capped. Content
// stream operands are handled by the top-level pass which uses the
// stricter real limit.
//
//nolint:dupl // Dict and array walkers handle distinct types.
func clampDictReals(dict ast.PdfDict) (ast.PdfDict, bool) {
	newDict := make(ast.PdfDict, len(dict))
	changed := false
	for k, v := range dict {
		switch val := v.(type) {
		case ast.PdfArray:
			if newArr, c := clampArrayReals(val); c {
				newDict[k] = newArr
				changed = true
				continue
			}
		case ast.PdfDict:
			if newSub, c := clampDictReals(val); c {
				newDict[k] = newSub
				changed = true
				continue
			}
		case ast.PdfReal:
			f := float64(val)
			if f > pdfARealLimit {
				newDict[k] = ast.PdfReal(pdfARealLimit)
				changed = true
				continue
			}
			if f < -pdfARealLimit {
				newDict[k] = ast.PdfReal(-pdfARealLimit)
				changed = true
				continue
			}
		case ast.PdfInt:
			n := int64(val)
			if n > pdfAIntLimit {
				newDict[k] = ast.PdfInt(pdfAIntLimit)
				changed = true
				continue
			}
			if n < -pdfAIntLimit-1 {
				newDict[k] = ast.PdfInt(-pdfAIntLimit - 1)
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

// clampArrayReals behaves like clampDictReals but for arrays.
//
//nolint:dupl // See clampDictReals.
func clampArrayReals(arr ast.PdfArray) (ast.PdfArray, bool) {
	newArr := make(ast.PdfArray, len(arr))
	changed := false
	for i, v := range arr {
		switch val := v.(type) {
		case ast.PdfArray:
			if sub, c := clampArrayReals(val); c {
				newArr[i] = sub
				changed = true
				continue
			}
		case ast.PdfDict:
			if sub, c := clampDictReals(val); c {
				newArr[i] = sub
				changed = true
				continue
			}
		case ast.PdfReal:
			f := float64(val)
			if f > pdfARealLimit {
				newArr[i] = ast.PdfReal(pdfARealLimit)
				changed = true
				continue
			}
			if f < -pdfARealLimit {
				newArr[i] = ast.PdfReal(-pdfARealLimit)
				changed = true
				continue
			}
		case ast.PdfInt:
			n := int64(val)
			if n > pdfAIntLimit {
				newArr[i] = ast.PdfInt(pdfAIntLimit)
				changed = true
				continue
			}
			if n < -pdfAIntLimit-1 {
				newArr[i] = ast.PdfInt(-pdfAIntLimit - 1)
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

// clampReal returns a clamped operand plus a flag reporting whether the
// value had to be modified. Integer operands that fit within the real
// limit are passed through unchanged.
func clampReal(obj ast.PdfObject) (ast.PdfObject, bool) {
	switch v := obj.(type) {
	case ast.PdfReal:
		f := float64(v)
		if f > pdfARealLimit {
			return ast.PdfReal(pdfARealLimit), true
		}
		if f < -pdfARealLimit {
			return ast.PdfReal(-pdfARealLimit), true
		}
	case ast.PdfInt:
		f := float64(v)
		if f > pdfARealLimit {
			return ast.PdfReal(pdfARealLimit), true
		}
		if f < -pdfARealLimit {
			return ast.PdfReal(-pdfARealLimit), true
		}
	}
	return obj, false
}
