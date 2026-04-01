package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// --- File structure numeric limits (ISO 32000-1 / ISO 32000-2) ---

const (
	maxIntegerValue = 2_147_483_647
	minIntegerValue = -2_147_483_648
	maxNameBytes    = 127
	maxIndirectObjs = 8_388_607
	maxStringBytes1 = 65535
	maxStringBytes2 = 32767
	maxArrayCap1    = 8191
	maxDictCap1     = 4095
	maxRealAbs1     = 32767.0
	maxRealAbs2     = 3.403e38
	minRealNonZero2 = 1.175e-38 //nolint:unused // reserved for future PDF/A-2+ real range check.
	maxDeviceN1     = 8
	maxDeviceN2     = 32
	maxQNesting     = 28
	maxCID          = 65535
)

// --- Rule selectors for numeric limit checks ---

func integerRangeRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t01
	case 2:
		return Rule_19005_2_6_1_13_t01
	case 3:
		return Rule_19005_3_6_1_13_t01
	default:
		return Rule_19005_1_6_1_12_t01
	}
}

func realRangeRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t02
	case 2:
		return Rule_19005_2_6_1_13_t02
	case 3:
		return Rule_19005_3_6_1_13_t02
	default:
		return Rule_19005_1_6_1_12_t02
	}
}

func stringLengthRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t03
	case 2:
		return Rule_19005_2_6_1_13_t03
	case 3:
		return Rule_19005_3_6_1_13_t03
	default:
		return Rule_19005_1_6_1_12_t03
	}
}

func nameLengthRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t04
	case 2:
		return Rule_19005_2_6_1_13_t04
	case 3:
		return Rule_19005_3_6_1_13_t04
	default:
		return Rule_19005_1_6_1_12_t04
	}
}

func indirectObjCountRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t07
	case 2:
		return Rule_19005_2_6_1_13_t07
	case 3:
		return Rule_19005_3_6_1_13_t07
	default:
		return Rule_19005_1_6_1_12_t07
	}
}

// --- Check implementations ---

// checkNumericLimits validates §6.1.12/6.1.13: numeric value limits for
// names, strings, integers, reals, arrays, dicts, and indirect objects.
func checkNumericLimits(ctx context.Context, doc *document.Document, profile string) []Violation {
	part := pdfaPart(profile)
	var results []Violation

	// Check indirect object count.
	objCount := len(doc.Index.Entries)
	if objCount > maxIndirectObjs {
		slog.Debug("compliance.file_structure: too many indirect objects",
			"count", objCount)
		results = append(results, Violation{
			Rule:    indirectObjCountRule(profile),
			Context: fmt.Sprintf("indirect object count %d exceeds %d", objCount, maxIndirectObjs),
		})
	} else {
		slog.Debug("compliance.file_structure: indirect object count OK",
			"count", objCount)
	}

	// Walk all objects checking names, strings, integers, reals, arrays, dicts.
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			slog.Debug("compliance.file_structure: context cancelled in numeric limits check")
			return results
		}
		if entry.Free {
			continue
		}

		obj, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			slog.Debug("compliance.file_structure: cannot resolve object", "error", err)
			continue
		}

		results = append(results, checkObjectNumericLimits(obj, objNum, part, profile)...)
	}

	// Additionally, scan actual content streams for string/integer limits.
	// Content streams are those referenced by page /Contents entries.
	results = append(results, checkContentStreamLimits(ctx, doc, part, profile)...)

	if len(results) == 0 {
		slog.Debug("compliance.file_structure: all numeric limits OK")
	}
	return results
}

// checkContentStreamLimits finds all page content streams and Form XObject
// streams and checks string/integer operands for implementation limit
// violations. Only actual content streams are scanned — not ICC profiles,
// font programs, or other binary streams.
func checkContentStreamLimits(ctx context.Context, doc *document.Document, part int, profile string) []Violation {
	catalog, err := getCatalog(ctx, doc)
	if err != nil || catalog == nil {
		return nil
	}
	pages := collectPages(ctx, doc, catalog)
	maxStr := maxStringBytes1
	if part >= 2 { //nolint:mnd
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
			results = append(results, scanStreamOperandLimits(doc, stream, objNum, maxStr, profile)...)
		}
	}
	return results
}

// scanStreamOperandLimits decodes a content stream and checks string/integer
// operands for limit violations.
func scanStreamOperandLimits(doc *document.Document, stream ast.PdfStream, objNum int, maxStr int, profile string) []Violation {
	data, err := doc.DecodeStream(stream)
	if err != nil || len(data) == 0 {
		return nil
	}
	ops, err := content.ParseStreamData(data)
	if err != nil {
		slog.Debug("compliance.file_structure: cannot parse content stream", "error", err)
		return nil
	}
	var results []Violation
	for _, op := range ops {
		for _, operand := range op.Operands {
			switch v := operand.(type) {
			case ast.PdfString:
				if len(v) > maxStr {
					results = append(results, Violation{
						Rule:    stringLengthRule(profile),
						Context: fmt.Sprintf("object %d (stream content): string length %d exceeds %d", objNum, len(v), maxStr),
					})
				}
			case ast.PdfInt:
				val := int64(v)
				if val > maxIntegerValue || val < minIntegerValue {
					results = append(results, Violation{
						Rule:    integerRangeRule(profile),
						Context: fmt.Sprintf("object %d (stream content): integer %d out of range", objNum, val),
					})
				}
			case ast.PdfArray:
				// TJ arrays can contain strings.
				for _, elem := range v {
					if s, ok := elem.(ast.PdfString); ok && len(s) > maxStr {
						results = append(results, Violation{
							Rule:    stringLengthRule(profile),
							Context: fmt.Sprintf("object %d (stream content): string length %d exceeds %d", objNum, len(s), maxStr),
						})
					}
				}
			}
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
		fval := float64(v)
		switch part {
		case 1:
			if math.Abs(fval) > maxRealAbs1 {
				results = append(results, Violation{
					Rule:    realRangeRule(profile),
					Context: fmt.Sprintf("object %d: real %g exceeds +/-%g", objNum, fval, maxRealAbs1),
				})
			}
		case 2, 3: //nolint:mnd
			if math.Abs(fval) > maxRealAbs2 {
				results = append(results, Violation{
					Rule:    realRangeRule(profile),
					Context: fmt.Sprintf("object %d: real %g exceeds +/-%g", objNum, fval, maxRealAbs2),
				})
			}
		}

	case ast.PdfString:
		maxStr := maxStringBytes1
		if part >= 2 { //nolint:mnd
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
		for key, val := range v {
			results = append(results, checkObjectNumericLimits(key, objNum, part, profile)...)
			results = append(results, checkObjectNumericLimits(val, objNum, part, profile)...)
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
			i += 3 //nolint:mnd // #XX is 3 characters encoding 1 byte.
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
