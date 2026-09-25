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

// PDF implementation limits. The PDF/A-1 (suffix 1) values mirror the
// architectural limits of PDF Reference 1.4, Table C.1, which ISO 19005-1,
// 6.1.12 imposes as conformance limits; veraPDF's PDF/A-1 profile cites that
// table for every 6.1.12 test. The PDF/A-2/3 values (suffix 2) mirror ISO
// 32000-1, Annex C, Table C.1, which ISO 19005-2/3, 6.1.13 imposes. That table
// raises the real limit and the DeviceN colorant count, lowers the string
// limit, and lists no array or dictionary capacity at all. Unsuffixed values
// are the same in both tables.
const (
	maxIntegerValue = 2_147_483_647  // Table C.1: largest integer.
	minIntegerValue = -2_147_483_648 // Table C.1: smallest integer.
	maxNameBytes    = MaxNameBytes   // Table C.1: name length in bytes.
	maxIndirectObjs = 8_388_607      // Table C.1: objects in a single file.
	maxStringBytes1 = 65535          // PDF Reference 1.4, Table C.1: string length.
	maxStringBytes2 = 32767          // ISO 32000-1, Table C.1: string length.
	maxArrayCap1    = 8191           // PDF Reference 1.4, Table C.1: array elements.
	maxDictCap1     = 4095           // PDF Reference 1.4, Table C.1: dictionary entries.
	maxRealAbs1     = 32767.0        // PDF Reference 1.4, Table C.1: largest real magnitude.
	maxRealAbs2     = 3.403e38       // ISO 32000-1, Table C.1: largest real magnitude.
	minRealAbs2     = 1.175e-38      // ISO 32000-1, Table C.1: nonzero real closest to 0.
	maxDeviceN1     = 8              // PDF Reference 1.4, Table C.1: DeviceN colorant count.
	maxDeviceN2     = 32             // ISO 32000-1, Table C.1: DeviceN colorant count.
	maxQNesting     = 28             // Table C.1: graphics-state (q/Q) nesting.
	maxCID          = 65535          // Table C.1: largest CID.
)

// MaxNameBytes is the 127-byte cap on PDF name length from ISO 32000-1
// Table C.1, imposed as a conformance limit by ISO 19005-1 §6.1.12 test 4
// and ISO 19005-2/3/4 §6.1.13 test 4. Exported so the remediation package
// repairs against the identical bound this package detects with, instead
// of redefining it and letting the two drift apart.
const MaxNameBytes = 127

// MaxRealAbs1, MaxRealAbs2 and MinRealAbs2 export the real bounds above, and
// MaxIntegerValue the integer bound, so the remediation package writes a
// number inside the bounds this package detects with, as [MaxNameBytes] does.
const (
	MaxRealAbs1     = maxRealAbs1
	MaxRealAbs2     = maxRealAbs2
	MinRealAbs2     = minRealAbs2
	MaxIntegerValue = maxIntegerValue
)

// SaturatedNumberNote ends the context of an implementation-limit violation
// in a content stream whose number is the largest integer or real, signed,
// veraPDF gives a number it cannot read. The content parse reads that value in
// a run such as "-" or ".020.594", which spells no number of ISO 32000-1, 7.3.3
// (see [content.ParseStreamData]), and in a number past the range of a double.
// Exported so the remediation that respells such a run claims the violations
// it answers.
const SaturatedNumberNote = "the largest value veraPDF gives a number it cannot read"

// saturatedNumber reports whether obj is a number [SaturatedNumberNote]
// describes.
func saturatedNumber(obj ast.PdfObject) bool {
	switch v := obj.(type) {
	case ast.PdfInt:
		return v == math.MaxInt64 || v == -math.MaxInt64
	case ast.PdfReal:
		return math.Abs(float64(v)) == math.MaxFloat64
	}
	return false
}

// noteSaturated ends the context of each of vs with [SaturatedNumberNote] when
// num is a number it describes, and returns vs.
func noteSaturated(vs []Violation, num ast.PdfObject) []Violation {
	if !saturatedNumber(num) {
		return vs
	}
	for i := range vs {
		vs[i].Context += ", " + SaturatedNumberNote
	}
	return vs
}

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
//
// # Corpus evidence
//
//   - pdfjs issue5599.pdf: writes /ID 77777777777777777 in its trailer, which
//     veraPDF reports against its root/trailer[0] and litho did not look at.
//   - pdfjs bug1980958.pdf: is 219 damaged bytes whose synthesized trailer
//     /Size is 2147483648, a value that appears nowhere in it.
//   - All corpora: a violation carrying the zero Rule serialized as a
//     clause-less "-t00" residual.
func checkNumericLimits(ctx context.Context, doc *document.Document, profile string) []Violation {
	part := PDFAPart(profile)
	var results []Violation

	// Check indirect object count.
	objCount := doc.XRefEntryCount()
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
	for objNum, entry := range doc.XRefEntries() {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free {
			continue
		}

		obj, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			doc.Logger.Debug("compliance.file_structure: cannot resolve object", "error", err)
			continue
		}

		overflow := countOverflows(doc.IntegerOverflows(objNum))
		results = append(results, checkObjectNumericLimits(obj, fmt.Sprintf("object %d", objNum), part, profile, overflow)...)
	}

	// The trailer dictionary is not an indirect object, so the walk above never
	// reaches it, yet ISO 19005-2 §6.1.13 constrains every number "a conforming
	// file shall not contain" wherever it is written, and veraPDF reports one
	// in the trailer against its root/trailer[0]. A trailer litho made up
	// because the file has none holds litho's numbers, not the file's, so it is
	// left alone.
	if !doc.TrailerSynthesized {
		results = append(results, checkObjectNumericLimits(doc.Index.Trailer, "trailer", part, profile, nil)...)
	}

	// The content streams are scanned by checkContentStreamLimits.
	return withoutZeroRule(results)
}

// withoutZeroRule drops the violations carrying the zero Rule. PDF/A-4 drops
// most ISO 32000-1 implementation limits, so several rule selectors in this
// file legitimately return the zero Rule for part 4. A violation carrying it
// must never escape: it serializes as a clause-less "-t00" residual.
func withoutZeroRule(results []Violation) []Violation {
	return slices.DeleteFunc(results, func(v Violation) bool { return v.Rule.Specification == "" })
}

// checkContentStreamLimits checks the string, integer and real operands of
// every content stream the document draws against the implementation limits:
// the content of each page, and every stream [drawnStreamWalk] reaches from it,
// its form XObjects, tiling patterns, Type3 glyphs and annotation appearances
// included. Only content streams are scanned, not ICC profiles, font programs
// or other binary streams.
//
// ISO 19005-2 §6.1.13 constrains every number a conforming file contains, and
// ISO 19005-1, 6.1.12 t2 states the real-magnitude limit over CosReal, which is
// every real the file writes. A stream a page draws is as much part of the file
// as the page's content: a tiling pattern paints its cell with a content stream
// of its own (ISO 32000-1, 8.7.3.1), and a form XObject, a Type3 glyph and an
// appearance are content streams too. veraPDF reaches them (its context path
// runs page/contentStream/operators[N]/xObject), and judges each once for the
// whole document: measured with veraPDF 1.30 at PDF/A-1b, a real past the
// limit fails 6.1.12 t2 once per real in a form nested in a form, a tiling
// pattern, an annotation appearance and a form one of them paints, and once
// for a form two pages paint.
//
// A page's content is judged once for each /Contents object, see
// [markJudgedContents]: measured the same way, two pages whose /Contents are
// direct arrays naming the same stream fail the limit once each, and a stream
// one page's array names twice fails it once.
//
// # Corpus evidence
//
//   - pdfjs issue6342.pdf: writes two out-of-range reals in the form its page
//     invokes at operator 11, and veraPDF reports both. It reports twelve more
//     there, for malformed numbers such as ".020.594" that it reads as
//     2147483647.
//   - pdfjs 22060_A1_01_Plans.pdf: keeps its 136 KB page stream inside the
//     limit and holds six out-of-range reals in patterns /R21 and /R24, and
//     veraPDF reports each of them, where litho reported one per pattern.
//   - pdfjs tracemonkey.pdf: veraPDF reports 328 out-of-range reals, and
//     litho reported 331, pages 10, 11 and 13 each stating one of theirs
//     twice.
func checkContentStreamLimits(ctx context.Context, vctx *validationCtx) []Violation {
	doc, profile := vctx.doc, vctx.profile
	maxStr := maxStringBytes1
	if vctx.part >= 2 {
		maxStr = maxStringBytes2
	}
	var results []Violation

	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}
		contentsObj := vctx.pages[i].dict["Contents"]
		if contentsObj == nil || vctx.pages[i].contentJudged {
			continue
		}
		// A page's content is judged whole even when another page's shares a
		// part of it, and a part the page lists twice is judged once.
		visited := make(map[int]bool)
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
			stream, err := document.ResolveStream(ctx, doc, ref)
			if err != nil {
				continue
			}
			data, err := doc.DecodeStream(ctx, stream)
			if err != nil || len(data) == 0 {
				continue
			}
			ops, overflows, err := content.ParseStreamDataWithOverflows(ctx, data)
			if err != nil {
				doc.Logger.Debug("compliance.file_structure: cannot parse content stream", "error", err)
				continue
			}
			results = append(results, checkOperandLimits(ops, countOverflows(overflows), objNum, maxStr, profile)...)
		}
	}

	walk := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		results = append(results, checkOperandLimits(s.ops, s.overflow, s.objNum, maxStr, profile)...)
	})
	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}
		walk.walkPage(ctx, &vctx.pages[i], i+1)
	}
	return withoutZeroRule(results)
}

// checkOperandLimits checks the strings, integers and reals of one parsed
// content stream, object objNum, against the implementation limits: every
// operand, and every one an array or a dictionary operand holds at any depth.
// overflow counts the integer literals past the 64-bit range the parse read as
// reals (see [checkNumberLimits]), and is consumed.
//
// ISO 19005-1, 6.1.12 and ISO 19005-2, 6.1.13 bound every integer, real and
// string the file contains, and an operand array or dictionary holds some: a
// dash array (ISO 32000-1, 8.4.3.6), a TJ array (9.4.3), a marked-content
// property list (14.6.2) and an inline image's dictionary (8.9.7). veraPDF
// judges each of them whole: measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b, an integer, a real, a denormal real, a long string, a "-" and a
// "1.5.5" there fail their limit once each, in an array or a dictionary nested
// in one too, as they fail it written as an operand. The parse reads the
// numbers of an array or a dictionary as veraPDF reads them, the "-" being the
// largest integer veraPDF gives a number and the "1.5.5" the largest real (see
// [content.ParseStreamData]).
//
// An array or a dictionary is judged whatever operator takes it, as the
// clauses state their limits. veraPDF judges the arrays and dictionaries its
// operators link alone: the ones above, and the numbers of an array given to
// c, cm, d1, l, m, re, Tm, v or y. Measured the same way, an integer past the
// limit in an array given to w, or in a dictionary given to w or BMC, fails
// nothing. A string past the limit as the /ActualText or /Alt of a property
// list fails the string limit twice, once as a value of the list and once as
// the text veraPDF reads for the marked content. Such a string is one string
// of the file, and is judged once.
//
// The operands of an operator ISO 32000-1, Annex A does not define are not
// checked. ISO 19005-1, 6.2.10 t1 and ISO 19005-2, 6.2.2 t1 already fail the
// operator, and veraPDF judges none of its operands against the limits:
// measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, a string, an integer
// and a real past the limits, a "-" or a "1.5.5", which it reads as the
// largest integer or real it gives a number, and an array holding them, each
// written before an undefined operator, inside a BX/EX section or not, fail
// the undefined-operator test alone, and fail the limit written before a
// defined one.
//
// # Corpus evidence
//
//   - pdfjs issue5039.pdf: a Type3 glyph writes "-inf" four times, which
//     veraPDF reads as the integer -9223372036854775807 and the undefined
//     operator inf, and it reports the operators alone.
//   - All corpora: judging the numbers of arrays and dictionaries changes
//     what compliance reports on 3 files at PDF/A-1b and PDF/A-2b, veraPDF
//     files at their own profile, all of them pdfjs, each a "-" in a TJ
//     array that fails the integer limit once.
//   - pdfjs bug1354114.pdf and bug1953099.pdf: veraPDF reports that integer,
//     and no other limit, in both. veraPDF cannot open bug1130815.pdf.
func checkOperandLimits(ops []content.Operation, overflow map[float64]int, objNum, maxStr int, profile string) []Violation {
	l := operandLimits{
		where:    fmt.Sprintf("object %d (stream content)", objNum),
		maxStr:   maxStr,
		part:     PDFAPart(profile),
		profile:  profile,
		overflow: overflow,
	}
	for _, op := range ops {
		if !content.IsOperator(op.Operator) {
			continue
		}
		for _, operand := range op.Operands {
			l.judge(operand)
		}
	}
	return l.results
}

// operandLimits carries one [checkOperandLimits] run over the operands of a
// content stream: where names the stream in the context of a violation,
// maxStr is the string limit of the part, and overflow counts the integer
// literals the parse read as reals (see [checkNumberLimits]).
type operandLimits struct {
	where    string
	maxStr   int
	part     int
	profile  string
	overflow map[float64]int
	results  []Violation
}

// judge checks one operand, and every string, integer and real an array or a
// dictionary operand holds at any depth, dictionary values in key order. The
// recursion is bounded by the nesting the parser reads (maxParseNestingDepth
// in the parser package).
func (l *operandLimits) judge(obj ast.PdfObject) {
	switch v := obj.(type) {
	case ast.PdfString:
		if len(v) > l.maxStr {
			l.results = append(l.results, Violation{
				Rule:    stringLengthRule(l.profile),
				Context: fmt.Sprintf("%s: string length %d exceeds %d", l.where, len(v), l.maxStr),
			})
		}
	case ast.PdfInt:
		val := int64(v)
		if val > maxIntegerValue || val < minIntegerValue {
			l.results = append(l.results, noteSaturated([]Violation{{
				Rule:    integerRangeRule(l.profile),
				Context: fmt.Sprintf("%s: integer %d out of range", l.where, val),
			}}, v)...)
		}
	case ast.PdfReal:
		l.results = append(l.results, noteSaturated(checkNumberLimits(float64(v), l.part, l.profile, l.where, l.overflow), v)...)
	case ast.PdfArray:
		for _, elem := range v {
			l.judge(elem)
		}
	case ast.PdfDict:
		for _, key := range slices.Sorted(maps.Keys(v)) {
			l.judge(v[key])
		}
	}
}

// countOverflows counts, by value, the integer literals past the 64-bit range
// that the parser read as reals in one object or content stream.
func countOverflows(values []float64) map[float64]int {
	if len(values) == 0 {
		return nil
	}
	overflow := make(map[float64]int, len(values))
	for _, v := range values {
		overflow[v]++
	}
	return overflow
}

// checkNumberLimits checks a parsed real that may have been written as an
// integer. ISO 32000-1, 7.3.3 makes a signed run of digits an integer object
// whatever its magnitude, so a literal past the 64-bit range, which the parser
// can only hold as a real, is judged against the integer limit (ISO 19005-2,
// 6.1.13 t1). veraPDF reads it as an integer too. overflow counts those
// literals by value; each one matched is consumed.
//
// # Corpus evidence
//
//   - pdfjs issue20872.pdf: writes four 309-digit coordinates, and veraPDF
//     reports t1 for each.
func checkNumberLimits(fval float64, part int, profile, where string, overflow map[float64]int) []Violation {
	if overflow[fval] == 0 {
		return checkRealLimits(fval, part, profile, where)
	}
	overflow[fval]--
	return []Violation{{
		Rule:    integerRangeRule(profile),
		Context: fmt.Sprintf("%s: integer %g out of range", where, fval),
	}}
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
// violations. It handles nested dicts and arrays recursively. where names the
// carrier for the violation context, since not every carrier is an indirect
// object: the trailer dictionary has no object number of its own. overflow
// counts the integer literals the object's parse read as reals (see
// [checkNumberLimits]).
func checkObjectNumericLimits(obj ast.PdfObject, where string, part int, profile string, overflow map[float64]int) []Violation {
	var results []Violation

	switch v := obj.(type) {
	case ast.PdfInt:
		val := int64(v)
		if val > maxIntegerValue || val < minIntegerValue {
			results = append(results, Violation{
				Rule:    integerRangeRule(profile),
				Context: fmt.Sprintf("%s: integer %d out of range", where, val),
			})
		}

	case ast.PdfReal:
		results = append(results, checkNumberLimits(float64(v), part, profile, where, overflow)...)

	case ast.PdfString:
		maxStr := maxStringBytes1
		if part >= 2 {
			maxStr = maxStringBytes2
		}
		if len(v) > maxStr {
			results = append(results, Violation{
				Rule:    stringLengthRule(profile),
				Context: fmt.Sprintf("%s: string length %d exceeds %d", where, len(v), maxStr),
			})
		}

	case ast.PdfName:
		// The limit applies to the bytes the name stands for, not to its
		// written form, and the name holds those bytes: the lexer already
		// expanded its #XX escapes (ISO 32000-1, 7.3.5). Expanding them a
		// second time shortened a name whose bytes spell an escape: 43 times
		// /#2341 stands for 129 bytes, "#41" 43 times, and read as 43.
		// veraPDF 1.30 fails ISO 19005-1, 6.1.12 t4 and ISO 19005-2, 6.1.13
		// t4 on it.
		if len(v) > maxNameBytes {
			results = append(results, Violation{
				Rule:    nameLengthRule(profile),
				Context: fmt.Sprintf("%s: name length %d exceeds %d", where, len(v), maxNameBytes),
			})
		}

	case ast.PdfArray:
		// PDF/A-1: array capacity limit.
		if part == 1 && len(v) > maxArrayCap1 {
			results = append(results, Violation{
				Rule:    Rule_19005_1_6_1_12_t05,
				Context: fmt.Sprintf("%s: array capacity %d exceeds %d", where, len(v), maxArrayCap1),
			})
		}
		for _, elem := range v {
			results = append(results, checkObjectNumericLimits(elem, where, part, profile, overflow)...)
		}

	case ast.PdfDict:
		// PDF/A-1: dictionary capacity limit.
		if part == 1 && len(v) > maxDictCap1 {
			results = append(results, Violation{
				Rule:    Rule_19005_1_6_1_12_t06,
				Context: fmt.Sprintf("%s: dict capacity %d exceeds %d", where, len(v), maxDictCap1),
			})
		}
		// Key order, because the recursion emits violations: a dictionary is a
		// Go map, so an unsorted walk reported the same two out-of-range reals
		// in a different order on consecutive runs.
		for _, key := range slices.Sorted(maps.Keys(v)) {
			results = append(results, checkObjectNumericLimits(key, where, part, profile, overflow)...)
			results = append(results, checkObjectNumericLimits(v[key], where, part, profile, overflow)...)
		}

	case ast.PdfStream:
		// Check the stream dictionary.
		if v.Dict != nil {
			results = append(results, checkObjectNumericLimits(v.Dict, where, part, profile, overflow)...)
		}
	}

	return results
}
