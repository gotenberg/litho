package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchRealLimits matches the violations of an integer or real of an object
// past the implementation limits, the tests [numberRepairsFor] answers: ISO
// 19005-1, 6.1.12 t1 and t2, and ISO 19005-2/3, 6.1.13 t1, t2 and t5. The
// numbers of content streams are [matchContentLimits]'s.
//
// The rule is matched, not the clause. Each part spells other requirements
// at these clause numbers: the string, name, array and dictionary limits
// beside the numeric ones, the permissions dictionary at ISO 19005-2/3,
// 6.1.12, the catalog /OCProperties at ISO 19005-1, 6.1.13 t1, and the
// catalog /Version at ISO 19005-4, 6.1.12 t1. Claiming the clause ran this
// repair on every one of them, and clamped the reals of a PDF/A-4 file whose
// only finding was its /Version, where ISO 19005-4 states no numeric limit.
func matchRealLimits(v compliance.Violation) bool {
	if contextContains(v, contentStreamContext) {
		return false
	}
	switch v.Rule {
	case compliance.Rule_19005_1_6_1_12_t01, compliance.Rule_19005_1_6_1_12_t02,
		compliance.Rule_19005_2_6_1_13_t01, compliance.Rule_19005_2_6_1_13_t02, compliance.Rule_19005_2_6_1_13_t05,
		compliance.Rule_19005_3_6_1_13_t01, compliance.Rule_19005_3_6_1_13_t02, compliance.Rule_19005_3_6_1_13_t05:
		return true
	}
	return false
}

// numberRepairs names the repairs [fixRealLimits] makes, each the answer to
// one test of the implementation limits.
type numberRepairs struct {
	// integers clamps an integer past the range of ISO 19005-1, 6.1.12 t1 and
	// ISO 19005-2/3, 6.1.13 t1 to that range.
	integers bool
	// maxReal is the largest real magnitude of ISO 19005-1, 6.1.12 t2 or ISO
	// 19005-2/3, 6.1.13 t2, which a real past it is clamped to, or 0 when
	// neither test failed.
	maxReal float64
	// denormals writes 0 for a nonzero real closer to 0 than ISO 19005-2/3,
	// 6.1.13 t5 admits, the one value that test accepts there.
	denormals bool
}

// numberRepairsFor returns the repairs the violations call for under the
// PDF/A part. Each repair answers one test of the part, so a real is clamped
// only when the part's real test failed, and to the part's limit: ISO 19005-1,
// 6.1.12 t2 caps a real at 32767 (PDF Reference 1.4, Table C.1), where ISO
// 19005-2/3, 6.1.13 t2 admits one up to 3.403e38 (ISO 32000-1, Table C.1).
// ISO 19005-4 states no numeric limit, and no repair runs under it.
func numberRepairsFor(violations []compliance.Violation, part int) numberRepairs {
	var r numberRepairs
	for _, v := range violations {
		switch {
		case part == 1 && v.Rule == compliance.Rule_19005_1_6_1_12_t01,
			part == 2 && v.Rule == compliance.Rule_19005_2_6_1_13_t01,
			part == 3 && v.Rule == compliance.Rule_19005_3_6_1_13_t01:
			r.integers = true
		case part == 1 && v.Rule == compliance.Rule_19005_1_6_1_12_t02:
			r.maxReal = compliance.MaxRealAbs1
		case part == 2 && v.Rule == compliance.Rule_19005_2_6_1_13_t02,
			part == 3 && v.Rule == compliance.Rule_19005_3_6_1_13_t02:
			r.maxReal = compliance.MaxRealAbs2
		case part == 2 && v.Rule == compliance.Rule_19005_2_6_1_13_t05,
			part == 3 && v.Rule == compliance.Rule_19005_3_6_1_13_t05:
			r.denormals = true
		}
	}
	return r
}

// fixRealLimits brings inside the implementation limits of the PDF/A part the
// integers and reals of every indirect object's body that the violations
// report, see [numberRepairsFor]: ISO 19005-1, 6.1.12 and ISO 19005-2, 6.1.13
// bound every number of the file, not page content alone. A number is changed
// only when it fails a test the violations name, which compliance judges over
// every object, so every change answers a finding. The numbers of content
// streams are clamped by [fixContentLimits], a content-tier repair.
//
// The repair used to clamp every real past 32767 and every integer past the
// integer range whatever the part, and ran on any violation of the clauses.
// Under ISO 19005-2 that moved a real of 40000, which 6.1.13 t2 admits, to
// 32767, and under ISO 19005-4, which states no numeric limit, it moved one
// for a catalog /Version finding.
//
// # Corpus evidence
//
//   - All corpora: over the PDF/A-1b, PDF/A-2b and PDF/A-4 remediations of the
//     58 files holding a number past the PDF/A-1 limits in an object, and the
//     PDF/A-4 remediations of 110 more whose catalog /Version is not 2.n, the
//     violations litho and veraPDF 1.30 report on the outputs do not change,
//     and neither does their extracted text.
//   - litho limits_1b.pdf and limits_2b.pdf: their PDF/A-2b remediations
//     clamped 2 and 1 reals past 32767, which ISO 19005-2 admits, to 32767.
//   - pdfjs issue20872.pdf: its PDF/A-4 remediation clamped the four
//     309-digit coordinates of an object for its catalog /Version finding.
func fixRealLimits(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, profile string) ([]string, error) {
	repairs := numberRepairsFor(violations, compliance.PDFAPart(profile))
	if repairs == (numberRepairs{}) {
		return nil, nil
	}
	total := 0
	clamp := func(objNum int) *numberClamp {
		return &numberClamp{numberRepairs: repairs, overflow: countIntegerOverflows(doc.IntegerOverflows(objNum))}
	}
	// Stream bodies are not walked here: the content streams are
	// fixContentLimits's.
	if _, err := walkNumberedObjects(ctx, alloc, doc,
		func(objNum int, d ast.PdfDict) (ast.PdfDict, bool) {
			c := clamp(objNum)
			out, changed := c.dict(d)
			total += c.clamped
			return out, changed
		},
		func(objNum int, a ast.PdfArray) (ast.PdfArray, bool) {
			c := clamp(objNum)
			out, changed := c.array(a)
			total += c.clamped
			return out, changed
		}); err != nil {
		return nil, err
	}

	if total == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("clamped %d out-of-range numeric value(s)", total)}, nil
}

// countIntegerOverflows counts, by value, the integer literals past the 64-bit
// range the parse of one object read as reals.
func countIntegerOverflows(values []float64) map[float64]int {
	if len(values) == 0 {
		return nil
	}
	counts := make(map[float64]int, len(values))
	for _, v := range values {
		counts[v]++
	}
	return counts
}

// numberClamp carries one object's [fixRealLimits] walk: the repairs to make,
// the integer literals the parse of the object read as reals (see
// [document.Document.IntegerOverflows]), and how many numbers it changed.
type numberClamp struct {
	numberRepairs
	overflow map[float64]int
	clamped  int
}

// dict returns dict with every number [numberClamp.value] changes changed,
// and whether it changed one. The entries are walked in key order, the order
// compliance judges them in, so an integer literal read as a real is matched
// to the same real compliance matched it to.
func (c *numberClamp) dict(dict ast.PdfDict) (ast.PdfDict, bool) {
	var out ast.PdfDict
	for _, k := range slices.Sorted(maps.Keys(dict)) {
		v, changed := c.value(dict[k])
		if !changed {
			continue
		}
		if out == nil {
			out = maps.Clone(dict)
		}
		out[k] = v
	}
	return out, out != nil
}

// array is [numberClamp.dict] for an array.
func (c *numberClamp) array(arr ast.PdfArray) (ast.PdfArray, bool) {
	var out ast.PdfArray
	for i, e := range arr {
		v, changed := c.value(e)
		if !changed {
			continue
		}
		if out == nil {
			out = slices.Clone(arr)
		}
		out[i] = v
	}
	return out, out != nil
}

// value returns obj brought inside the limits the repairs name, and whether it
// changed. An array or a dictionary is walked whole.
//
// A real the parse read from an integer literal past the 64-bit range is the
// integer ISO 32000-1, 7.3.3 makes of it, and compliance judges it against the
// integer limit alone, as veraPDF does, so it is clamped as an integer.
func (c *numberClamp) value(obj ast.PdfObject) (ast.PdfObject, bool) {
	switch v := obj.(type) {
	case ast.PdfArray:
		return c.array(v)
	case ast.PdfDict:
		return c.dict(v)
	case ast.PdfInt:
		if c.integers && (v > compliance.MaxIntegerValue || v < -compliance.MaxIntegerValue-1) {
			c.clamped++
			return clampInteger(v > 0), true
		}
	case ast.PdfReal:
		f := float64(v)
		if c.overflow[f] > 0 {
			c.overflow[f]--
			if !c.integers {
				return obj, false
			}
			c.clamped++
			return clampInteger(f > 0), true
		}
		if c.maxReal > 0 && math.Abs(f) > c.maxReal {
			c.clamped++
			return ast.PdfReal(math.Copysign(c.maxReal, f)), true
		}
		if c.denormals && f != 0 && math.Abs(f) < compliance.MinRealAbs2 {
			c.clamped++
			return ast.PdfReal(0), true
		}
	}
	return obj, false
}

// clampInteger returns the largest integer ISO 19005-1, 6.1.12 t1 and ISO
// 19005-2/3, 6.1.13 t1 admit when positive is set, and the smallest otherwise.
func clampInteger(positive bool) ast.PdfInt {
	if positive {
		return ast.PdfInt(compliance.MaxIntegerValue)
	}
	return ast.PdfInt(-compliance.MaxIntegerValue - 1)
}
