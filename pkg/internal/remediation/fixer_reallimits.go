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

// matchRealLimits matches PDF/A numeric-limit violations. ISO 19005-1 §6.1.12
// and ISO 19005-2/4 §6.1.13 impose different caps on integers and reals; both
// overflows are repaired by the same pass, which clamps the numbers of every
// indirect object. The numbers of content streams are [matchContentLimits]'s.
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

// fixRealLimits clamps out-of-range numeric values in every indirect object's
// body. PDF/A §6.1.12 / §6.1.13 apply to bare numeric literals anywhere in the
// file, not just page content. The numbers of content streams are clamped by
// [fixContentLimits], a content-tier repair.
func fixRealLimits(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	// Stream bodies are not walked here: the content streams are
	// fixContentLimits's.
	total, err := walkIndirectObjects(ctx, alloc, doc, clampDictReals, clampArrayReals)
	if err != nil {
		return nil, err
	}

	if total == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("clamped %d out-of-range numeric value(s)", total)}, nil
}

// clampDictReals clamps PdfReal values to ±[pdfARealLimit] and PdfInt
// values to ±[pdfAIntLimit]. Small integers are untouched so /Length,
// /Size, offsets etc. stay intact; only values that exceed the 32-bit
// signed range (which PDF/A forbids outright) get capped. The numbers
// of content streams are clamped by [clampContentNumbers].
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
