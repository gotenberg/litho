package remediation

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// matchUnknownOps matches ISO 19005-2/4 §6.2.2 content-stream violations
// that report operators not defined in ISO 32000. These are dead bytes at
// the viewer level, so stripping them is content-preserving.
func matchUnknownOps(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.2") {
		return false
	}
	return descriptionContains(v, "undefined operator") ||
		descriptionContains(v, "not defined in ISO") ||
		descriptionContains(v, "unknown operator") ||
		descriptionContains(v, "operator \"")
}

// fixUnknownOps strips operators absent from [validOperators] from every
// page content stream and every Form XObject referenced through page
// Resources. Viewers ignore these tokens per ISO 32000 §A.2, so removal
// has no visible effect and only eliminates the PDF/A violation.
func fixUnknownOps(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var total, repaired int
	visited := make(map[int]bool)
	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, r, err := stripUnknownFromStream(ctx, alloc, doc, page, pageNum+1)
		if err != nil {
			return nil, err
		}
		total += n
		repaired += r
		total += stripUnknownInResources(ctx, alloc, doc, page.Resources, visited)
		total += stripUnknownInAnnotations(ctx, alloc, doc, page.Dict, visited)
	}

	var fixes []string
	if total > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d unknown content-stream operator(s)", total))
	}
	// Reported separately: a repair rewrites the stream without removing an
	// operator, so folding it into the removal count would say an operator went
	// that did not, and saying nothing would rewrite page content silently.
	if repaired > 0 {
		fixes = append(fixes, fmt.Sprintf("repaired %d malformed number(s) in content streams", repaired))
	}
	return fixes, nil
}

// stripUnknownFromStream parses a page's live content (alloc-first, so an
// earlier fixer's rewrite is seen), repairs malformed numbers, strips operators
// outside [validOperators], and writes the result back through
// [replaceContentStream]. Returns the number of operators removed and the number
// of malformed numbers folded back into their operator.
func stripUnknownFromStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, pageNum int) (int, int, error) {
	ops, contentsObj, err := livePageContentOps(ctx, alloc, doc, page)
	if err != nil || contentsObj == nil {
		return 0, 0, nil
	}
	ops, repairs := repairMalformedNumbers(ops)
	removed := 0
	modified := repairs > 0
	for i, op := range ops {
		if op.Operator == "" || op.Operator == "BX" || op.Operator == "EX" {
			continue
		}
		if !validOperators[op.Operator] {
			ops[i].Operator = ""
			removed++
			modified = true
		}
	}
	if !modified {
		return 0, 0, nil
	}
	filtered := filterOps(ops)
	newData := serializeOps(filtered)
	if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum); err != nil {
		return removed, repairs, fmt.Errorf("replacing content stream on page %d: %w", pageNum, err)
	}
	return removed, repairs, nil
}

// stripUnknownInAnnotations strips undefined operators from the appearance
// streams of a page's annotations.
//
// An appearance stream is a content stream (ISO 32000-1, 12.5.5), so the ISO
// 19005-2, 6.2.2 prohibition reaches it, but it hangs off /Annots[]/AP rather
// than the page resources this fixer walked, and veraPDF reports an operator
// there that litho left in place.
func stripUnknownInAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageDict ast.PdfDict, visited map[int]bool) int {
	if pageDict == nil {
		return 0
	}
	annots, err := document.ResolveArray(ctx, doc, pageDict["Annots"])
	if err != nil || len(annots) == 0 {
		return 0
	}

	total := 0
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
				total += stripUnknownFromStreamObject(ctx, alloc, doc, entry, visited)
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

// stripUnknownFromStreamObject rewrites one appearance stream without the
// operators outside [validOperators], returning how many it removed. The
// replacement body is decoded content, so /Filter and /DecodeParms go with it.
func stripUnknownFromStreamObject(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, visited map[int]bool) int {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum == 0 || visited[ref.ObjectNum] {
		return 0
	}
	visited[ref.ObjectNum] = true

	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return 0
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return 0
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return 0
	}

	removed := 0
	for i, op := range ops {
		if op.Operator == "" || op.Operator == "BX" || op.Operator == "EX" {
			continue
		}
		if !validOperators[op.Operator] {
			ops[i].Operator = ""
			removed++
		}
	}
	if removed == 0 {
		return 0
	}

	newData := serializeOps(filterOps(ops))
	newDict := make(ast.PdfDict, len(stream.Dict)+1)
	for k, v := range stream.Dict {
		if k == "Length" || k == "Filter" || k == "DecodeParms" {
			continue
		}
		newDict[k] = v
	}
	newDict["Length"] = ast.PdfInt(len(newData))
	alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, newData))
	return removed
}

// stripUnknownInResources recurses into a page's /Resources/XObject
// entries to process Form XObjects that may hold their own content
// streams with bad operators.
func stripUnknownInResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, visited map[int]bool) int {
	if resources == nil {
		return 0
	}
	xoObj, ok := resources["XObject"]
	if !ok {
		return 0
	}
	xoDict, err := document.ResolveDict(ctx, doc, xoObj)
	if err != nil || xoDict == nil {
		return 0
	}
	total := 0
	for _, entry := range xoDict {
		ref, ok := entry.(ast.PdfIndirectRef)
		if !ok || ref.ObjectNum == 0 || visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		rawObj, err := doc.Resolve(ctx, ref)
		if err != nil {
			continue
		}
		stream, ok := rawObj.(ast.PdfStream)
		if !ok {
			continue
		}
		subtype, _ := stream.Dict["Subtype"].(ast.PdfName)
		if subtype != "Form" {
			continue
		}

		// Decode through the filter pipeline: [ast.PdfStream.Bytes] hands back
		// the raw body, which for a FlateDecode form is compressed bytes the
		// content lexer reads as a run of undefined operators.
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			continue
		}
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			continue
		}
		removed := 0
		modified := false
		for i, op := range ops {
			if op.Operator == "" || op.Operator == "BX" || op.Operator == "EX" {
				continue
			}
			if !validOperators[op.Operator] {
				ops[i].Operator = ""
				removed++
				modified = true
			}
		}
		if !modified {
			continue
		}
		filtered := filterOps(ops)
		newData := serializeOps(filtered)
		// The replacement body is the decoded content, so the filter chain that
		// described the original body no longer applies: carrying /Filter or
		// /DecodeParms across leaves a stream a reader decodes into nothing
		// (ISO 32000-1, 7.3.8.2). /Length is rewritten from the data.
		newDict := make(ast.PdfDict, len(stream.Dict)+1)
		for k, v := range stream.Dict {
			if k == "Length" || k == "Filter" || k == "DecodeParms" {
				continue
			}
			newDict[k] = v
		}
		newDict["Length"] = ast.PdfInt(len(newData))
		alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, newData))
		total += removed

		// Recurse into nested Form XObjects.
		if nestedRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && nestedRes != nil {
			total += stripUnknownInResources(ctx, alloc, doc, nestedRes, visited)
		}
	}
	return total
}

// repairMalformedNumbers folds tokens the content parser surfaced as operators,
// but which are really numeric literals that lost their digits, back into the
// operand list of the operator that follows. Returns how many were folded.
//
// ISO 32000-1, 7.3.3 writes a real as an optional sign, digits and an optional
// decimal point, so "-." is not a number and the parser cannot treat it as one.
// It becomes an operator, which splits the operand run around it, and stripping
// it as an undefined operator (ISO 19005-2, 6.2.2) takes that op's operands with
// it. On pdfjs issue15604.pdf the text matrix
//
//	-. -.002 .002 -. 59.64 760.026 Tm
//
// came out as "59.64 760.026 Tm": Tm needs six operands (Table 108) and had two,
// so no text matrix was ever set and every one of the page's 1027 characters
// stopped extracting. The premise that stripping an undefined operator is
// content-preserving holds only for a real operator; a mangled operand is not
// dead bytes, it is part of the next operator's arguments.
//
// The salvaged value is the token's longest valid numeric prefix, or zero when
// it has none: "-." is negative zero, which is the value a tolerant reader
// already shows, and keeping the operand count right matters more than the
// digit no producer wrote.
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
