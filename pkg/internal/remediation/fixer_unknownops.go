package remediation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// matchUnknownOps matches the content-stream violations that report an
// operator not defined in ISO 32000: ISO 19005-2/3/4, 6.2.2 t1 and ISO
// 19005-1, 6.2.10 t1. These are dead bytes at the viewer level, so stripping
// them is content-preserving.
//
// The part-1 spelling is matched by the rule's Object, as [matchAggressive]
// matches it, because clause strings are reused across parts. Claiming 6.2.2
// alone left every part-1 finding to the aggressive pass, which reads page
// content only, runs only when content may change, and never sees a number
// written with an exponent, which the content parser reads as a number.
//
// # Corpus evidence
//
//   - All corpora: at PDF/A-1b the part-1 claim clears 6.2.10 t1 for litho and
//     veraPDF on 13 default cells, among them pdfjs bug1130815.pdf with 1674
//     undefined operators, and on 3 aggressive cells whose operator sits in a
//     form XObject the aggressive pass never reads.
//   - pdfjs poppler-90-0-fuzzed.pdf: at PDF/A-1b default veraPDF stops
//     reporting 6.2.10 t1, and litho's count drops from 1617 to 1571.
func matchUnknownOps(v compliance.Violation) bool {
	if v.Rule.Object == "Op_Undefined" {
		return true
	}
	if !clausePrefix(v, "6.2.2") {
		return false
	}
	return descriptionContains(v, "undefined operator") ||
		descriptionContains(v, "not defined in ISO") ||
		descriptionContains(v, "unknown operator") ||
		descriptionContains(v, "operator \"")
}

// unknownOpsTally counts what [fixUnknownOps] changed.
type unknownOpsTally struct {
	// removed counts operators outside [validOperators] that were stripped.
	removed int
	// repaired counts malformed numbers folded back into their operator.
	repaired int
	// exponents counts numbers written with an exponent that became plain.
	exponents int
}

func (t *unknownOpsTally) add(o unknownOpsTally) {
	t.removed += o.removed
	t.repaired += o.repaired
	t.exponents += o.exponents
}

// fixUnknownOps strips operators absent from [validOperators] from every page
// content stream, every Form XObject referenced through page Resources, and
// every annotation appearance stream. Viewers ignore these tokens per
// ISO 32000 §A.2, so removing a genuine one has no visible effect and only
// eliminates the PDF/A violation.
//
// That premise holds for a real operator alone, so each of the three streams
// is repaired first by [repairContentData]: a number written with an exponent,
// which veraPDF reads as a number and an undefined operator, is rewritten as
// the plain number it denotes, and a numeric literal the parser could not read
// surfaces as an operator whose removal would take the operands of the
// operator that follows. See [rewriteExponentNumbers] and the worked Tm
// example on [repairMalformedNumbers].
func fixUnknownOps(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	part := compliance.PDFAPart(profile)
	var total unknownOpsTally
	visited := make(map[int]bool)
	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := stripUnknownFromStream(ctx, alloc, doc, page, pageNum+1, part)
		if err != nil {
			return nil, err
		}
		total.add(n)
		total.add(stripUnknownInResources(ctx, alloc, doc, page.Resources, visited, part))
		total.add(stripUnknownInAnnotations(ctx, alloc, doc, page.Dict, visited, part))
	}

	var fixes []string
	if total.removed > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d unknown content-stream operator(s)", total.removed))
	}
	// Reported separately: a repair rewrites the stream without removing an
	// operator, so folding it into the removal count would say an operator went
	// that did not, and saying nothing would rewrite page content silently.
	if total.repaired > 0 {
		fixes = append(fixes, fmt.Sprintf("repaired %d malformed number(s) in content streams", total.repaired))
	}
	if total.exponents > 0 {
		fixes = append(fixes, fmt.Sprintf("rewrote %d number(s) written with an exponent in content streams", total.exponents))
	}
	return fixes, nil
}

// repairContentData repairs one content stream's decoded bytes: numbers
// written with an exponent become plain numbers ([rewriteExponentNumbers]),
// malformed numbers fold back into their operator ([repairMalformedNumbers]),
// and operators outside [validOperators] go. It returns the bytes to write and
// what it changed; a zero tally means nothing is to be written. The bytes can
// be empty: a stream holding nothing but undefined operators is written back
// with none.
//
// A stream whose only defect is an exponent is written back as spliced, not
// re-serialized, so nothing else in it moves. Removing an operator or folding a
// number re-serializes the parsed operations, which cannot round-trip an inline
// image: the parser consumes its data off-token, so the BI operation keeps its
// dictionary alone, and writing it back leaves "<< ... >> BI" with no ID, data
// or EI, a content stream no reader can parse (ISO 32000-1, 8.9.7). Such a
// stream keeps its operators and malformed numbers as a reported residual, as
// [fixContentStreams] keeps them.
func repairContentData(ctx context.Context, data []byte, part int) ([]byte, unknownOpsTally) {
	var t unknownOpsTally
	data, t.exponents = rewriteExponentNumbers(ctx, data, part)
	spliced := func() ([]byte, unknownOpsTally) {
		if t.exponents == 0 {
			return nil, unknownOpsTally{}
		}
		return data, unknownOpsTally{exponents: t.exponents}
	}

	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return spliced()
	}
	ops, t.repaired = repairMalformedNumbers(ops)
	hasInlineImage := false
	for i, op := range ops {
		if op.Operator == "BI" {
			hasInlineImage = true
		}
		if op.Operator == "" || op.Operator == "BX" || op.Operator == "EX" {
			continue
		}
		if !validOperators[op.Operator] {
			ops[i].Operator = ""
			t.removed++
		}
	}
	if (t.removed == 0 && t.repaired == 0) || hasInlineImage {
		return spliced()
	}
	return serializeOps(filterOps(ops)), t
}

// stripUnknownFromStream repairs a page's live content (alloc-first, so an
// earlier fixer's rewrite is seen) with [repairContentData] and writes the
// result back through [replaceContentStream].
func stripUnknownFromStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, pageNum, part int) (unknownOpsTally, error) {
	contentsObj, ok := livePageDict(ctx, alloc, doc, page)["Contents"]
	if !ok {
		return unknownOpsTally{}, nil
	}
	data, err := liveContentBytes(ctx, alloc, doc, contentsObj)
	if err != nil {
		return unknownOpsTally{}, nil
	}
	newData, t := repairContentData(ctx, data, part)
	if t == (unknownOpsTally{}) {
		return t, nil
	}
	if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum); err != nil {
		return t, fmt.Errorf("replacing content stream on page %d: %w", pageNum, err)
	}
	return t, nil
}

// stripUnknownInAnnotations repairs the appearance streams of a page's
// annotations with [repairContentData].
//
// An appearance stream is a content stream (ISO 32000-1, 12.5.5), so the ISO
// 19005-2, 6.2.2 prohibition reaches it, but it hangs off /Annots[]/AP rather
// than the page resources this fixer walked, and veraPDF reports an operator
// there that litho left in place.
func stripUnknownInAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageDict ast.PdfDict, visited map[int]bool, part int) unknownOpsTally {
	var total unknownOpsTally
	if pageDict == nil {
		return total
	}
	annots, err := document.ResolveArray(ctx, doc, pageDict["Annots"])
	if err != nil || len(annots) == 0 {
		return total
	}

	for _, annotObj := range annots {
		if err := ctx.Err(); err != nil {
			return total
		}
		annot, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annot == nil {
			continue
		}
		apDict, err := document.ResolveDict(ctx, doc, annot["AP"])
		if err != nil || apDict == nil {
			continue
		}
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apObj, ok := apDict[key]
			if !ok {
				continue
			}
			for _, entry := range appearanceEntries(ctx, doc, apObj) {
				total.add(stripUnknownFromStreamObject(ctx, alloc, doc, entry, visited, part))
			}
		}
	}
	return total
}

// appearanceEntries returns the objects an /AP entry names: the entry itself
// when it is a stream, or every value of the appearance-state subdictionary a
// multi-state annotation uses (ISO 32000-1, 12.5.5), as a checkbox does.
func appearanceEntries(ctx context.Context, doc *document.Document, apObj ast.PdfObject) []ast.PdfObject {
	if _, err := document.ResolveStream(ctx, doc, apObj); err == nil {
		return []ast.PdfObject{apObj}
	}
	stateDict, err := document.ResolveDict(ctx, doc, apObj)
	if err != nil || stateDict == nil {
		return nil
	}
	out := make([]ast.PdfObject, 0, len(stateDict))
	for _, v := range stateDict {
		out = append(out, v)
	}
	return out
}

// stripUnknownFromStreamObject repairs one appearance stream with
// [repairContentData]. An appearance stream is a content stream (ISO 32000-1,
// 12.5.5), so a mangled operand splits its operand run exactly as it does on a
// page, and stripping the token takes the next operator's arguments with it.
//
// # Corpus evidence
//
//   - pdfjs issue15604.pdf: the page path gained the malformed-number repair
//     when the Tm on this file lost four of its six operands.
func stripUnknownFromStreamObject(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, visited map[int]bool, part int) unknownOpsTally {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum == 0 || visited[ref.ObjectNum] {
		return unknownOpsTally{}
	}
	visited[ref.ObjectNum] = true

	// Read alloc-first, for the same reason as the resources walk below: an
	// appearance stream is a Form XObject (ISO 32000-1, 12.5.5), so graphics,
	// transparency and nested-resources reach it from registry entries ahead of
	// unknown-ops, and rebuilding the body from the on-disk copy would discard
	// what they queued. ref is known non-zero from the guard above.
	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, obj)
	if !ok {
		return unknownOpsTally{}
	}
	t, _ := repairStreamObject(ctx, alloc, doc, ref, stream, part)
	return t
}

// repairStreamObject repairs the content stream at ref, a form XObject or an
// appearance stream, with [repairContentData] and writes it back. It returns
// what changed and whether the stream could be decoded at all.
//
// The replacement body is the decoded content, so the filter chain that
// described the original body no longer applies: carrying /Filter or
// /DecodeParms across leaves a stream a reader decodes into nothing (ISO
// 32000-1, 7.3.8.2). /Length is rewritten from the data.
func repairStreamObject(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef, stream ast.PdfStream, part int) (unknownOpsTally, bool) {
	// Decode through the filter pipeline: ast.PdfStream.Bytes hands back the
	// raw body, which for a FlateDecode form is compressed bytes the content
	// lexer reads as a run of undefined operators.
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return unknownOpsTally{}, false
	}
	newData, t := repairContentData(ctx, data, part)
	if t == (unknownOpsTally{}) {
		return t, true
	}
	newDict := make(ast.PdfDict, len(stream.Dict)+1)
	for k, v := range stream.Dict {
		if k == "Length" || k == "Filter" || k == "DecodeParms" {
			continue
		}
		newDict[k] = v
	}
	newDict["Length"] = ast.PdfInt(len(newData))
	alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, newData))
	return t, true
}

// stripUnknownInResources recurses into a page's /Resources/XObject
// entries to process Form XObjects that may hold their own content
// streams with bad operators, repairing each with [repairContentData].
//
// A form XObject holds a content stream (ISO 32000-1, 8.10.2), so a mangled
// operand costs it the next operator's arguments here just as it does on a
// page. The forms a form draws are reached whether or not the form itself
// needed a repair: the walk descended only from a form it had rewritten, so a
// defect one level below a clean form was never seen.
func stripUnknownInResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, visited map[int]bool, part int) unknownOpsTally {
	var total unknownOpsTally
	if resources == nil {
		return total
	}
	xoObj, ok := resources["XObject"]
	if !ok {
		return total
	}
	xoDict, err := document.ResolveDict(ctx, doc, xoObj)
	if err != nil || xoDict == nil {
		return total
	}
	for _, entry := range xoDict {
		if err := ctx.Err(); err != nil {
			return total
		}
		ref, ok := entry.(ast.PdfIndirectRef)
		if !ok || ref.ObjectNum == 0 || visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		// Read alloc-first. graphics, transparency and nested-resources all
		// write XObject streams from registry entries ahead of unknown-ops, so
		// rebuilding the body from the on-disk copy below would discard their
		// edits when this one lands.
		stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, ref)
		if !ok {
			continue
		}
		if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); subtype != "Form" {
			continue
		}
		t, readable := repairStreamObject(ctx, alloc, doc, ref, stream, part)
		if !readable {
			continue
		}
		total.add(t)

		if nestedRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && nestedRes != nil {
			total.add(stripUnknownInResources(ctx, alloc, doc, nestedRes, visited, part))
		}
	}
	return total
}

// exponentTokenCheckInterval is how many tokens [rewriteExponentNumbers] reads
// between two checks of its context, the interval the content parser uses.
const exponentTokenCheckInterval = 500

// rewriteExponentNumbers returns data with every number written with an
// exponent, such as "1.5e1" or "1E-3", replaced by the plain number it denotes
// ([plainNumber]), and how many it replaced. A stream holding no such number
// where an operand stands comes back unchanged with a count of 0.
//
// ISO 32000-1, 7.3.3 gives a number no exponent, so the token is no operand.
// veraPDF ends the number at the exponent letter and reads the rest as an
// operator: "1.5e1 w" is the number 1.5, an operator "e1" that ISO 32000-1
// does not define (ISO 19005-1, 6.2.10 t1; ISO 19005-2, 6.2.2 t1), and a w
// with no operand. Stripping "e1" the way an undefined operator is stripped
// would take the 1.5 with it and leave w without its width, which is not what
// the producer wrote: a reader that accepts the form, as litho's parser and
// PDF.js do, reads the one number 15, and exponential format is only a
// spelling of a number the clause rules out (ISO 32000-1, 7.3.3 names
// "6.02E23"). Writing that number plainly keeps every operator's operands and
// leaves nothing undefined.
//
// Only a stream with a finding is rewritten: a number where an operand
// stands, outside any array or dictionary, which is the one the check reports
// ([parser.ErrNumberExponentOperand]). The rest of such a stream's exponents go
// too, since veraPDF stops reading a stream at one inside an array or a
// dictionary. An inline image's dictionary is left as written: the walk skips
// it with its data, which it cannot address token by token.
//
// The rewrite splices the tokens in place, so the stream's other bytes, inline
// image data included, are carried across unchanged. A stream the lexer cannot
// read to its end is left alone.
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: the one corpus file with an exponent where an
//     operand stands, "/F1 1e2 Tf" after the stray keyword ETBT. Removing ETBT
//     re-serialized the page with "1e2" and the Tf after it gone, and wrote
//     "/F1 50 600 Td". The rewrite keeps "/F1 100 Tf", which puts the next
//     line above the page, where PDF.js draws it. litho's own reading of the
//     original lost the same two tokens, which left that Td without effect
//     and the line on the page, so the integrity probe counted a text drop
//     and rolled the content repairs back. Read with the operand dropped
//     alone, the original puts the line above the page too, and the repairs
//     stay: veraPDF's undefined-operator findings on the output go at
//     PDF/A-1b and PDF/A-2b.
func rewriteExponentNumbers(ctx context.Context, data []byte, part int) ([]byte, int) {
	type splice struct {
		start, end int
		text       string
	}
	var splices []splice
	operands, depth := 0, 0
	p := parser.NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))).WithOffsetTracking()
	for n := 1; ; n++ {
		if n%exponentTokenCheckInterval == 0 && ctx.Err() != nil {
			return data, 0
		}
		tok, err := p.Peek(0)
		if err != nil || tok.Type == lexer.TokenError {
			return data, 0
		}
		if tok.Type == lexer.TokenEOF {
			break
		}
		end := int(p.PeekEnd(0))
		if _, err := p.Consume(); err != nil {
			return data, 0
		}
		switch tok.Type {
		case lexer.TokenKeyword:
			if tok.Value == "BI" && depth == 0 {
				if _, err := p.ParseInlineImageDict(ctx); err != nil {
					return data, 0
				}
			}
		case lexer.TokenDelimiter:
			switch tok.Value {
			case "[", "<<":
				depth++
			case "]", ">>":
				depth = max(depth-1, 0)
			}
		case lexer.TokenNumber:
			start := end - len(tok.Value)
			if !strings.ContainsAny(tok.Value, "eE") || start < 0 || string(data[start:end]) != tok.Value {
				continue
			}
			text, ok := plainNumber(tok.Value, part)
			if !ok {
				continue
			}
			splices = append(splices, splice{start: start, end: end, text: text})
			if depth == 0 {
				operands++
			}
		case lexer.TokenEOF, lexer.TokenError, lexer.TokenName, lexer.TokenString, lexer.TokenHexString:
			// No number, and no nesting: a string or a name is one token.
		}
	}
	if operands == 0 {
		return data, 0
	}

	var out bytes.Buffer
	out.Grow(len(data))
	prev := 0
	for _, s := range splices {
		out.Write(data[prev:s.start])
		out.WriteString(s.text)
		prev = s.end
	}
	out.Write(data[prev:])
	return out.Bytes(), len(splices)
}

// plainNumber writes the number an exponent token denotes the way ISO
// 32000-1, 7.3.3 spells a number, inside the limits of the PDF/A part: an
// integer when it is whole and within the integer range, and a real with a
// decimal point otherwise. ok is false when tok is not a number.
//
// A magnitude past the part's largest real becomes that real (ISO 19005-1,
// 6.1.12 t2; ISO 19005-2, 6.1.13 t2). Part 4 states no limit and takes the
// ISO 32000-1, Table C.1 one, which is also where a float64 overflow such as
// "1e999" lands. A nonzero magnitude closer to 0 than the smallest normal real
// of that table becomes 0, the only value ISO 19005-2, 6.1.13 t5 accepts
// there; parts 1 and 4 state no such floor and take the same one, since no
// operand draws differently at that magnitude. A whole number past the integer
// range keeps its decimal point, since read back as an integer it would break
// ISO 19005-2, 6.1.13 t1.
func plainNumber(tok string, part int) (string, bool) {
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return "", false
	}
	maxAbs := compliance.MaxRealAbs2
	if part == 1 {
		maxAbs = compliance.MaxRealAbs1
	}
	switch {
	case v > maxAbs:
		v = maxAbs
	case v < -maxAbs:
		v = -maxAbs
	case math.Abs(v) < compliance.MinRealAbs2:
		v = 0
	}
	if v == math.Trunc(v) && math.Abs(v) <= compliance.MaxIntegerValue {
		return strconv.FormatInt(int64(v), 10), true
	}
	s := writer.FormatReal(v)
	if !strings.ContainsRune(s, '.') {
		s += ".0"
	}
	return s, true
}

// repairMalformedNumbers folds tokens the content parser surfaced as operators,
// but which are really numeric literals that lost their digits, back into the
// operand list of the operator that follows. Returns how many were folded.
//
// ISO 32000-1, 7.3.3 writes a real as an optional sign, digits and an optional
// decimal point, so "-." is not a number and the parser cannot treat it as one.
// It becomes an operator, which splits the operand run around it, and stripping
// it as an undefined operator (ISO 19005-2, 6.2.2) takes that op's operands with
// it. The text matrix
//
//	-. -.002 .002 -. 59.64 760.026 Tm
//
// came out as "59.64 760.026 Tm": Tm needs six operands (Table 108) and had two,
// so no text matrix was ever set and the page's text stopped extracting. The
// premise that stripping an undefined operator is content-preserving holds only
// for a real operator; a mangled operand is not dead bytes, it is part of the
// next operator's arguments.
//
// The salvaged value is the token's longest valid numeric prefix, or zero when
// it has none: "-." is negative zero, which is the value a tolerant reader
// already shows, and keeping the operand count right matters more than the
// digit no producer wrote.
//
// # Corpus evidence
//
//   - pdfjs issue15604.pdf: carries that text matrix, and every one of the
//     page's 1027 characters stopped extracting.
func repairMalformedNumbers(ops []content.Operation) ([]content.Operation, int) {
	var pending []ast.PdfObject
	out := make([]content.Operation, 0, len(ops))
	repaired := 0

	for _, op := range ops {
		if malformedNumber(op.Operator) {
			pending = append(pending, op.Operands...)
			pending = append(pending, ast.PdfReal(numericPrefix(op.Operator)))
			repaired++
			continue
		}
		if len(pending) > 0 {
			merged := make([]ast.PdfObject, 0, len(pending)+len(op.Operands))
			merged = append(merged, pending...)
			merged = append(merged, op.Operands...)
			op.Operands = merged
			pending = nil
		}
		out = append(out, op)
	}
	// A trailing run with no operator to attach to invokes nothing, so it is
	// dropped rather than emitted as bare operands.
	return out, repaired
}

// malformedNumber reports whether tok is built only from the characters a
// numeric literal uses, which means the parser surfaced it as an operator only
// because it is not a well-formed number (ISO 32000-1, 7.3.3). A token made of
// digits alone would have parsed as a number and never reached here.
func malformedNumber(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if (r < '0' || r > '9') && r != '+' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

// numericPrefix returns the value of the longest leading run of tok that parses
// as a PDF number, or zero when there is none.
func numericPrefix(tok string) float64 {
	end, seenDigit, seenDot := 0, false, false
	for i, r := range tok {
		switch {
		case r == '+' || r == '-':
			if i != 0 {
				return prefixValue(tok, end, seenDigit)
			}
		case r == '.':
			if seenDot {
				return prefixValue(tok, end, seenDigit)
			}
			seenDot = true
		case r >= '0' && r <= '9':
			seenDigit = true
		default:
			return prefixValue(tok, end, seenDigit)
		}
		end = i + 1
	}
	return prefixValue(tok, end, seenDigit)
}

// prefixValue parses tok[:end], treating a run with no digit as zero.
func prefixValue(tok string, end int, seenDigit bool) float64 {
	if !seenDigit {
		return 0
	}
	v, err := strconv.ParseFloat(tok[:end], 64)
	if err != nil {
		return 0
	}
	return v
}
