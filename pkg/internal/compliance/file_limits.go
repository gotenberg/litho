package compliance

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// PDF implementation limits. The PDF/A-1 (suffix 1) values mirror
// ISO 32000-1:2008 Annex C, Table C.1 (Architectural limits) and are
// imposed as conformance limits by ISO 19005-1 §6.1.12. The PDF/A-2..4
// values (suffix 2) reflect PDF 2.0 (ISO 32000-2), which raises the real
// and string limits and the DeviceN colorant count; ISO 19005-2/3
// §6.1.13 imposes them. maxQNesting (q/Q graphics-state nesting) and
// maxCID (16-bit CID space) are spec structural maxima, not Table C.1.
const (
	maxIntegerValue = 2_147_483_647  // ISO 32000-1 Table C.1: largest integer.
	minIntegerValue = -2_147_483_648 // ISO 32000-1 Table C.1: smallest integer.
	maxNameBytes    = MaxNameBytes   // ISO 32000-1 Table C.1: name length in bytes.
	maxIndirectObjs = 8_388_607      // ISO 32000-1 Table C.1: objects in a single file.
	maxStringBytes1 = 65535          // ISO 32000-1 Table C.1: string length.
	maxStringBytes2 = 32767          // ISO 32000-2: PDF 2.0 string length.
	maxArrayCap1    = 8191           // ISO 32000-1 Table C.1: array elements.
	maxDictCap1     = 4095           // ISO 32000-1 Table C.1: dictionary entries.
	maxRealAbs1     = 32767.0        // ISO 32000-1 Table C.1: largest real magnitude.
	maxRealAbs2     = 3.403e38       // ISO 32000-2: PDF 2.0 real magnitude (IEEE single).
	minRealAbs2     = 1.175e-38      // ISO 32000-2: smallest normal IEEE single magnitude.
	maxDeviceN1     = 8              // ISO 32000-1: DeviceN colorant count.
	maxDeviceN2     = 32             // ISO 32000-2: PDF 2.0 DeviceN colorant count.
	maxQNesting     = 28             // ISO 32000-1 Table C.1: graphics-state (q/Q) nesting.
	maxCID          = 65535          // 16-bit CID space.
)

// MaxNameBytes is the 127-byte cap on PDF name length from ISO 32000-1
// Table C.1, imposed as a conformance limit by ISO 19005-1 §6.1.12 test 4
// and ISO 19005-2/3/4 §6.1.13 test 4. Exported so the remediation package
// repairs against the identical bound this package detects with, instead
// of redefining it and letting the two drift apart.
const MaxNameBytes = 127

func integerRangeRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_12_t01, Rule_19005_2_6_1_13_t01, Rule_19005_3_6_1_13_t01, Rule{})
}

// realDenormalRule returns the rule for a real too close to zero to be an IEEE
// single. Only ISO 19005-2/3, 6.1.13 t5 states it: ISO 19005-1 caps the
// magnitude alone and ISO 19005-4 has no CosReal rule at all.
func realDenormalRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_1_13_t05, Rule_19005_3_6_1_13_t05, Rule{})
}

func realRangeRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_12_t02, Rule_19005_2_6_1_13_t02, Rule_19005_3_6_1_13_t02, Rule{})
}

func stringLengthRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_12_t03, Rule_19005_2_6_1_13_t03, Rule_19005_3_6_1_13_t03, Rule{})
}

func nameLengthRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_12_t04, Rule_19005_2_6_1_13_t04, Rule_19005_3_6_1_13_t04, Rule{})
}

func indirectObjCountRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_12_t07, Rule_19005_2_6_1_13_t07, Rule_19005_3_6_1_13_t07, Rule{})
}

// checkNumericLimits validates §6.1.12/6.1.13: numeric value limits for
// names, strings, integers, reals, arrays, dicts, and indirect objects.
func checkNumericLimits(ctx context.Context, doc *document.Document, profile string) []Violation {
	part := PDFAPart(profile)
	var results []Violation

	// Check indirect object count.
	objCount := len(doc.Index.Entries)
	if objCount > maxIndirectObjs {
		results = append(results, Violation{
			Rule:    indirectObjCountRule(profile),
			Context: fmt.Sprintf("indirect object count %d exceeds %d", objCount, maxIndirectObjs),
		})
	}

	// Walk all objects checking names, strings, integers, reals, arrays, dicts.
	// In object-number order: the walk appends violations, and Go randomises map
	// iteration, so an unsorted walk reported the same findings in a different
	// order on consecutive runs of the same binary.
	objNums := slices.Sorted(maps.Keys(doc.Index.Entries))
	for _, objNum := range objNums {
		if ctx.Err() != nil {
			return results
		}
		if doc.Index.Entries[objNum].Free {
			continue
		}

		obj, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			doc.Logger.Debug("compliance.file_structure: cannot resolve object", "error", err)
			continue
		}

		results = append(results, checkObjectNumericLimits(obj, objNum, part, profile)...)
	}

	// Additionally, scan actual content streams for string/integer limits.
	// Content streams are those referenced by page /Contents entries.
	results = append(results, checkContentStreamLimits(ctx, doc, part, profile)...)

	// PDF/A-4 drops most ISO 32000-1 implementation limits, so several
	// rule selectors in this file legitimately return the zero Rule for
	// part 4. A violation carrying it must never escape: it serialized
	// as a clause-less "-t00" residual in the corpus.
	return slices.DeleteFunc(results, func(v Violation) bool { return v.Rule.Specification == "" })
}

// checkContentStreamLimits finds all page content streams and Form XObject
// streams and checks string/integer operands for implementation limit
// violations. Only actual content streams are scanned, not ICC profiles,
// font programs, or other binary streams.
func checkContentStreamLimits(ctx context.Context, doc *document.Document, part int, profile string) []Violation {
	catalog, err := resolveCatalog(ctx, doc)
	if err != nil || catalog == nil {
		return nil
	}
	pages := collectPages(ctx, doc, catalog)
	maxStr := maxStringBytes1
	if part >= 2 {
		maxStr = maxStringBytes2
	}
	var results []Violation
	visited := make(map[int]bool)

	for _, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		// Page content streams.
		contentsObj := page["Contents"]
		if contentsObj == nil {
			continue
		}
		// Contents can be a single stream ref or an array of stream refs.
		var refs []ast.PdfObject
		if arr, err := document.ResolveArray(ctx, doc, contentsObj); err == nil && arr != nil {
			refs = arr
		} else {
			refs = []ast.PdfObject{contentsObj}
		}
		for _, ref := range refs {
			objNum := 0
			if r, ok := ref.(ast.PdfIndirectRef); ok {
				objNum = r.ObjectNum
				if visited[objNum] {
					continue
				}
				visited[objNum] = true
			}
			stream, sErr := document.ResolveStream(ctx, doc, ref)
			if sErr != nil {
				continue
			}
			results = append(results, scanStreamOperandLimits(ctx, doc, stream, objNum, maxStr, profile)...)
		}
	}
	return results
}

// scanStreamOperandLimits decodes a content stream and checks string/integer
// operands for limit violations.
func scanStreamOperandLimits(ctx context.Context, doc *document.Document, stream ast.PdfStream, objNum int, maxStr int, profile string) []Violation {
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return nil
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance.file_structure: cannot parse content stream", "error", err)
		return nil
	}
	part := PDFAPart(profile)
	where := fmt.Sprintf("object %d (stream content)", objNum)
	var results []Violation
	for _, op := range ops {
		for _, operand := range op.Operands {
			switch v := operand.(type) {
			case ast.PdfString:
				if len(v) > maxStr {
					results = append(results, Violation{
						Rule:    stringLengthRule(profile),
						Context: fmt.Sprintf("%s: string length %d exceeds %d", where, len(v), maxStr),
					})
				}
			case ast.PdfInt:
				val := int64(v)
				if val > maxIntegerValue || val < minIntegerValue {
					results = append(results, Violation{
						Rule:    integerRangeRule(profile),
						Context: fmt.Sprintf("%s: integer %d out of range", where, val),
					})
				}
			case ast.PdfReal:
				results = append(results, checkRealLimits(float64(v), part, profile, where)...)
			case ast.PdfArray:
				// TJ arrays carry strings and displacement numbers alike.
				for _, elem := range v {
					switch e := elem.(type) {
					case ast.PdfString:
						if len(e) > maxStr {
							results = append(results, Violation{
								Rule:    stringLengthRule(profile),
								Context: fmt.Sprintf("%s: string length %d exceeds %d", where, len(e), maxStr),
							})
						}
					case ast.PdfReal:
						results = append(results, checkRealLimits(float64(e), part, profile, where)...)
					}
				}
			}
		}
	}
	return results
}

// checkRealLimits checks one real against the magnitude cap its part states,
// and against the denormal floor parts 2 and 3 add. where names the carrier
// for the violation context.
func checkRealLimits(fval float64, part int, profile, where string) []Violation {
	var results []Violation
	switch part {
	case 1:
		if math.Abs(fval) > maxRealAbs1 {
			results = append(results, Violation{
				Rule:    realRangeRule(profile),
				Context: fmt.Sprintf("%s: real %g exceeds +/-%g", where, fval, maxRealAbs1),
			})
		}
	case 2, 3:
		if math.Abs(fval) > maxRealAbs2 {
			results = append(results, Violation{
				Rule:    realRangeRule(profile),
				Context: fmt.Sprintf("%s: real %g exceeds +/-%g", where, fval, maxRealAbs2),
			})
		}
		// A magnitude below the smallest normal IEEE single is a denormal,
		// which ISO 19005-2/3, 6.1.13 t5 forbids separately from the magnitude
		// cap: `realValue == 0.0 || realValue <= -1.175e-38 ||
		// realValue >= 1.175e-38`. Zero is expressly permitted. Parts 1 and 4
		// state no such requirement, so they stay silent.
		if fval != 0 && math.Abs(fval) < minRealAbs2 {
			results = append(results, Violation{
				Rule:    realDenormalRule(profile),
				Context: fmt.Sprintf("%s: real %g is closer to zero than +/-%g", where, fval, minRealAbs2),
			})
		}
	}
	return results
}

// checkObjectNumericLimits checks a single resolved object for numeric limit
// violations. It handles nested dicts and arrays recursively.
func checkObjectNumericLimits(obj ast.PdfObject, objNum int, part int, profile string) []Violation {
	var results []Violation

	switch v := obj.(type) {
	case ast.PdfInt:
		val := int64(v)
		if val > maxIntegerValue || val < minIntegerValue {
			results = append(results, Violation{
				Rule:    integerRangeRule(profile),
				Context: fmt.Sprintf("object %d: integer %d out of range", objNum, val),
			})
		}

	case ast.PdfReal:
		results = append(results, checkRealLimits(float64(v), part, profile, fmt.Sprintf("object %d", objNum))...)

	case ast.PdfString:
		maxStr := maxStringBytes1
		if part >= 2 {
			maxStr = maxStringBytes2
		}
		if len(v) > maxStr {
			results = append(results, Violation{
				Rule:    stringLengthRule(profile),
				Context: fmt.Sprintf("object %d: string length %d exceeds %d", objNum, len(v), maxStr),
			})
		}

	case ast.PdfName:
		// PDF names may contain #XX hex escape sequences. The limit applies
		// to the decoded byte length, not the encoded representation.
		decodedLen := decodedNameLength(string(v))
		if decodedLen > maxNameBytes {
			results = append(results, Violation{
				Rule:    nameLengthRule(profile),
				Context: fmt.Sprintf("object %d: name length %d exceeds %d", objNum, decodedLen, maxNameBytes),
			})
		}

	case ast.PdfArray:
		// PDF/A-1: array capacity limit.
		if part == 1 && len(v) > maxArrayCap1 {
			results = append(results, Violation{
				Rule:    Rule_19005_1_6_1_12_t05,
				Context: fmt.Sprintf("object %d: array capacity %d exceeds %d", objNum, len(v), maxArrayCap1),
			})
		}
		for _, elem := range v {
			results = append(results, checkObjectNumericLimits(elem, objNum, part, profile)...)
		}

	case ast.PdfDict:
		// PDF/A-1: dictionary capacity limit.
		if part == 1 && len(v) > maxDictCap1 {
			results = append(results, Violation{
				Rule:    Rule_19005_1_6_1_12_t06,
				Context: fmt.Sprintf("object %d: dict capacity %d exceeds %d", objNum, len(v), maxDictCap1),
			})
		}
		// Key order, because the recursion emits violations: a dictionary is a
		// Go map, so an unsorted walk reported the same two out-of-range reals
		// in a different order on consecutive runs.
		for _, key := range slices.Sorted(maps.Keys(v)) {
			results = append(results, checkObjectNumericLimits(key, objNum, part, profile)...)
			results = append(results, checkObjectNumericLimits(v[key], objNum, part, profile)...)
		}

	case ast.PdfStream:
		// Check the stream dictionary.
		if v.Dict != nil {
			results = append(results, checkObjectNumericLimits(v.Dict, objNum, part, profile)...)
		}
	}

	return results
}

// decodedNameLength returns the byte length of a PDF name after decoding #XX
// hex escape sequences. Each #XX sequence represents a single byte in the
// decoded name.
func decodedNameLength(name string) int {
	decoded := 0
	i := 0
	for i < len(name) {
		if name[i] == '#' && i+2 < len(name) && isHexDigit(name[i+1]) && isHexDigit(name[i+2]) {
			decoded++
			i += 3
		} else {
			decoded++
			i++
		}
	}
	return decoded
}

// isHexDigit returns true if c is a valid hexadecimal digit.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
