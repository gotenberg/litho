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
// names, strings, integers, reals, arrays, dicts, and indirect objects. An
// object in twice fails each limit twice; see [cosReachedTwice].
//
// The trailer is judged once, and a document information dictionary written
// directly in it twice, as veraPDF reaches it (see [trailerFormatViolations]).
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: a direct /Info holding
// an integer past its range, a string past the length limit or a name key past
// its length limit, or a real past the PDF/A-1 range at PDF/A-1b, fails the
// limit twice, and a name key past it in the trailer itself once. Only the
// newest section's trailer counts: an integer past its range in a direct
// /Info, or in another entry, of an older section's trailer fails nothing.
//
// # Corpus evidence
//
//   - pdfjs issue5599.pdf: writes /ID 77777777777777777 in its trailer, which
//     veraPDF reports against its root/trailer[0] and litho did not look at.
//   - pdfjs bug1980958.pdf: is 219 damaged bytes whose synthesized trailer
//     /Size is 2147483648, a value that appears nowhere in it.
//   - All corpora: a violation carrying the zero Rule serialized as a
//     clause-less "-t00" residual.
func checkNumericLimits(ctx context.Context, doc *document.Document, profile string, twice map[int]bool) []Violation {
	part := PDFAPart(profile)
	var results []Violation

	// Count the objects the walk below judges ([sweptXRefEntries]).
	objCount := doc.OpenedXRefEntryCount()
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
	for objNum, entry := range sweptXRefEntries(doc) {
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
		results = append(results, asReached(twice, objNum, checkObjectNumericLimits(obj, fmt.Sprintf("object %d", objNum), part, profile, overflow))...)
	}

	// The trailer dictionary is not an indirect object, so the walk above never
	// reaches it, yet ISO 19005-2 §6.1.13 constrains every number "a conforming
	// file shall not contain" wherever it is written, and veraPDF reports one
	// in the trailer against its root/trailer[0]. A trailer litho made up
	// because the file has none holds litho's numbers, not the file's, so it is
	// left alone, and so is what the reader carried into it from an older
	// section ([document.Document.TrailerAsWritten]).
	//
	// A document information dictionary written directly in the trailer is
	// judged once more, as the CosInfo veraPDF builds from it
	// (root/trailer[0]/Info[0]); see [trailerFormatViolations].
	if !doc.TrailerSynthesized {
		trailer := doc.TrailerAsWritten()
		results = append(results, checkObjectNumericLimits(trailer, "trailer", part, profile, nil)...)
		if info, ok := trailer["Info"].(ast.PdfDict); ok {
			results = append(results, checkObjectNumericLimits(info, "trailer /Info", part, profile, nil)...)
		}
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

// checkContentStreamLimits checks the string, name, integer and real operands
// of every content stream the document draws against the implementation
// limits: the content of each page, and every stream [drawnStreamWalk] reaches
// from it, its form XObjects, tiling patterns, Type3 glyphs and annotation
// appearances included. Only content streams are scanned, not ICC profiles,
// font programs or other binary streams.
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
// The parts of a /Contents array are judged as the one content stream ISO
// 32000-1, 7.7.3.3 makes them, which is how veraPDF reads them and how the
// other content-stream rules judge them ([resolvePageContentBytes]). Judged
// part by part, the limits still fired in a part veraPDF never reads. Measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, an integer past the limit given
// to w fails nothing in the part after one whose stray "]" ends veraPDF's
// reading, nor in a page whose other part, before or after it, holds an array
// veraPDF gives up on (see [content.ParseStreamData]). An integer in a dash
// array, a TJ array or an operand list a part opens and the next one closes
// fails it once. "99999" ending one part and "999999 w" starting the next fail
// nothing: the parts join no token.
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
//   - All corpora: judging the parts of a /Contents array as one stream
//     changes what compliance reports on no file at PDF/A-1b and PDF/A-2b,
//     veraPDF files at their own profile.
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
		// A page's content is judged whole even when another page's shares a
		// part of it, and a part the page lists twice is judged once.
		page := &vctx.pages[i]
		if page.contentJudged || len(page.content) == 0 {
			continue
		}
		ops, overflows, err := content.ParseStreamDataWithOverflows(ctx, page.content)
		if err != nil {
			doc.Logger.Debug("compliance.file_structure: cannot parse page content", "page", i+1, "error", err)
			continue
		}
		where := fmt.Sprintf("page %d (stream content)", i+1)
		results = append(results, checkOperandLimits(ops, countOverflows(overflows), where, false, maxStr, profile)...)
	}

	walk := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		results = append(results, checkOperandLimits(s.ops, s.overflow, streamContentWhere(s.objNum), s.uncoloured, maxStr, profile)...)
	})
	for i := range vctx.pages {
		if ctx.Err() != nil {
			return results
		}
		walk.walkPage(ctx, &vctx.pages[i], i+1)
	}
	return withoutZeroRule(results)
}

// checkOperandLimits checks the strings, names, integers and reals of one
// parsed content stream against the implementation limits: the ones veraPDF
// reaches through the operators that use them ([ContentOperandLink]). where
// names the stream in the context of a violation, and uncoloured reports that
// it starts with its colour operators ignored ([NewColourState]). overflow
// counts the integer literals past the 64-bit range the parse read as reals
// (see [checkNumberLimits]), and is consumed.
//
// ISO 19005-1, 6.1.12 and ISO 19005-2, 6.1.13 bound every integer, real,
// string and name the file contains, whatever operand holds it, a name to 127
// bytes (t4) as the object walk judges it ([checkObjectNumericLimits]): its
// #XX escapes expanded. Deliberate leniency toward veraPDF, which judges an
// operand only as the object its operator reads it as: an operand the
// operator does not read, before the ones it needs or of another type, is
// judged by no rule, see [ContentOperandLink]. An operand array or dictionary
// veraPDF reads whole is judged whole, its keys and values: a dash
// array (ISO 32000-1, 8.4.3.6), a TJ array (9.4.3), a marked-content property
// list (14.6.2) and an inline image's dictionary (8.9.7). Measured with
// veraPDF 1.30 at PDF/A-1b and PDF/A-2b, an integer, a real, a denormal real,
// a long string, a "-" and a "1.5.5" there fail their limit once each, in an
// array or a dictionary nested in one too, as they fail it written as an
// operand. The parse reads the numbers of an array or a dictionary as veraPDF
// reads them, the "-" being the largest integer veraPDF gives a number and the
// "1.5.5" the largest real (see [content.ParseStreamData]). A string past the
// limit as the /ActualText or /Alt of a property list fails the string limit
// twice, once as a value of the list and once as the text veraPDF reads for
// the marked content. Such a string is one string of the file, and is judged
// once. So is a property list BDC or DP names from /Properties, which veraPDF
// reads through the operator as well as in the resources: measured the same
// way, an integer past the limit in it fails the limit twice. The object walk
// judges it once ([checkNumericLimits]). The name by which BDC or DP names the
// list is no name veraPDF judges: a long one fails nothing.
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
//   - All corpora: judging the names of a content stream changes what
//     compliance reports on 2 files at PDF/A-1b and PDF/A-2b, veraPDF files
//     at their own profile, each now failing the name limit as often as
//     veraPDF 1.30 does.
//   - pdfjs issue6151.pdf: selects its font by a name of 139 bytes, written
//     with #20 escapes, which veraPDF reports as a Tf operand beside the /Font
//     key the object walk reports.
//   - litho limits_2b.pdf: gives cs a name of 203 bytes, which veraPDF reports
//     beside the /ColorSpace key.
func checkOperandLimits(ops []content.Operation, overflow map[float64]int, where string, uncoloured bool, maxStr int, profile string) []Violation {
	l := operandLimits{
		where:    where,
		maxStr:   maxStr,
		part:     PDFAPart(profile),
		profile:  profile,
		overflow: overflow,
	}
	colour := NewColourState(uncoloured)
	for _, op := range ops {
		n := len(op.Operands)
		reads := content.IsOperator(op.Operator)
		if op.Operator == "BI" && n > 0 {
			reads = colour.ReadsInlineImage(inlineImageMask(op.Operands[n-1]))
		}
		for i, operand := range op.Operands {
			link := OperandLink(0)
			if reads {
				link = ContentOperandLink(op.Operator, i, n, colour.On())
			}
			l.judgeOperand(operand, link)
		}
		colour.Operator(op.Operator)
	}
	return l.results
}

// inlineImageMask reports whether dict, the dictionary of an inline image,
// makes it an image mask by its /IM entry (ISO 32000-1, 8.9.7, Table 93), the
// only spelling veraPDF reads (OperatorParser.processInlineImage).
func inlineImageMask(dict ast.PdfObject) bool {
	d, ok := dict.(ast.PdfDict)
	if !ok {
		return false
	}
	imageMask, ok := d["IM"].(ast.PdfBool)
	return ok && bool(imageMask)
}

// streamContentWhere names content stream objNum in the context of a
// violation.
func streamContentWhere(objNum int) string {
	return fmt.Sprintf("object %d (stream content)", objNum)
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

// judgeOperand checks what link reaches of one operand ([ContentOperandLink]),
// and passes over the rest of it ([operandLimits.skip]).
func (l *operandLimits) judgeOperand(obj ast.PdfObject, link OperandLink) {
	switch v := obj.(type) {
	case ast.PdfInt:
		if link&(LinkNumber|LinkInteger) != 0 {
			l.judge(v)
			return
		}
	case ast.PdfReal:
		// An integer literal past the 64-bit range is a real of the parse,
		// and an integer of ISO 32000-1, 7.3.3 and of veraPDF.
		if link&LinkNumber != 0 || link&LinkInteger != 0 && l.overflow[float64(v)] > 0 {
			l.judge(v)
			return
		}
	case ast.PdfString:
		if link&LinkString != 0 {
			l.judge(v)
			return
		}
	case ast.PdfName:
		if link&LinkName != 0 {
			l.judge(v)
			return
		}
	case ast.PdfArray:
		switch {
		case link&LinkArray != 0:
			l.judge(v)
			return
		case link&LinkArrayNumbers != 0:
			for _, elem := range v {
				l.judgeOperand(elem, pick(isNumber(elem), LinkNumber))
			}
			return
		}
	case ast.PdfDict:
		if link&LinkDict != 0 {
			l.judge(v)
			return
		}
	}
	l.skip(obj)
}

// isNumber reports whether obj is an integer or a real.
func isNumber(obj ast.PdfObject) bool {
	switch obj.(type) {
	case ast.PdfInt, ast.PdfReal:
		return true
	}
	return false
}

// skip passes over an object no rule judges, consuming the integer literals
// past the 64-bit range it holds, so that the count of a value left for the
// objects after it is theirs (see [checkNumberLimits]).
func (l *operandLimits) skip(obj ast.PdfObject) {
	switch v := obj.(type) {
	case ast.PdfReal:
		if l.overflow[float64(v)] > 0 {
			l.overflow[float64(v)]--
		}
	case ast.PdfArray:
		for _, elem := range v {
			l.skip(elem)
		}
	case ast.PdfDict:
		for _, key := range slices.Sorted(maps.Keys(v)) {
			l.skip(v[key])
		}
	}
}

// judge checks one object, and every string, name, integer and real an array
// or a dictionary holds at any depth, each key of a dictionary with its value,
// in key order. The recursion is bounded by the nesting the parser reads
// (maxParseNestingDepth in the parser package).
func (l *operandLimits) judge(obj ast.PdfObject) {
	switch v := obj.(type) {
	case ast.PdfName:
		// The limit applies to the bytes the name stands for, which the
		// lexer already expanded from its #XX escapes, as for a name of an
		// object (see [checkObjectNumericLimits]).
		if len(v) > maxNameBytes {
			l.results = append(l.results, Violation{
				Rule:    nameLengthRule(l.profile),
				Context: fmt.Sprintf("%s: name length %d exceeds %d", l.where, len(v), maxNameBytes),
			})
		}
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
			l.judge(key)
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
