package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// matchAggressive matches violations that require content modification to fix.
// These are only applied when PreserveContent is false.
//
// # Corpus evidence
//
//   - All corpora: matching the bare 6.9 clause sent 13 cells to
//     [fixAnnotationAppearances]. Four of the 13 had no other claimant: parts
//     2 and 3 test 3, and part 4 tests 1 and 5.
//   - All corpora: the same short-circuit, a negative arm skipping every check
//     after it, cost 12 corpus cells in [matchCatalogCleanup].
func matchAggressive(v compliance.Violation) bool {
	switch v.Rule.Clause {
	case "6.3.3", "6.5.3": // annotation/form appearances
		// Both carry exactly one meaning in every part that spells them,
		// PDAnnot, so neither needs scoping.
		return true
	case "6.9":
		// Only ISO 19005-1 spells the form-appearance rules 6.9: the
		// NeedAppearances flag and the widget appearance dictionary. Parts 2
		// and 3 give that number to the optional content configuration
		// dictionary, and part 4 to embedded files.
		//
		// Matching the bare clause sent those rules to
		// fixAnnotationAppearances, which writes an /AP into every
		// annotation that lacks one. That repairs no optional-content or
		// embedded-file rule, and unlike a claim on a no-op fixer it MUTATES
		// the document on an unrelated trigger. Some of them had no other
		// claimant at all: parts 2 and 3 test 3, the /Order completeness rule
		// matchCatalogCleanup refuses for the same reason, and part 4 tests
		// 1 and 5.
		//
		// No negative arm here. Returning false would skip the Op_Undefined
		// check below, the same short-circuit that once broke
		// matchCatalogCleanup; this arm matches, or falls through.
		if v.Rule.Specification == "ISO_19005_1" {
			return true
		}
	case "6.2.9", "6.2.6": // rendering intent in content streams
		return true
	case "6.2.2": // unknown content stream operators
		return true
	}
	// Undefined content-stream operators key by Object across parts: ISO 19005-1
	// numbers the rule §6.2.10, parts 2-4 §6.2.2 (matched above). Clause strings
	// are reused across parts, so match the stable Object here.
	return v.Rule.Object == "Op_Undefined"
}

// fixAggressive applies fixes that modify content streams or generate visual
// content. Requires opts.PreserveContent == false.
func fixAggressive(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, _ string) ([]string, error) {
	if opts.PreserveContent {
		return nil, nil
	}

	clauseSet := make(map[string]bool)
	// Undefined operators key by Object: ISO 19005-1 numbers the rule §6.2.10,
	// parts 2-4 §6.2.2. Clause strings are reused across parts, so the stable
	// Object drives the unknown-operator removal regardless of profile.
	opUndefined := false
	for _, v := range violations {
		clauseSet[v.Rule.Clause] = true
		if v.Rule.Object == "Op_Undefined" {
			opUndefined = true
		}
	}

	var fixes []string

	if clauseSet["6.3.3"] || clauseSet["6.5.3"] || clauseSet["6.9"] {
		desc, err := fixAnnotationAppearances(ctx, alloc, doc)
		if err != nil {
			return nil, fmt.Errorf("fixing annotation appearances: %w", err)
		}
		fixes = append(fixes, desc...)
	}

	if clauseSet["6.2.9"] || clauseSet["6.2.6"] || opUndefined {
		desc, err := fixContentStreams(ctx, alloc, doc,
			clauseSet["6.2.9"] || clauseSet["6.2.6"],
			opUndefined)
		if err != nil {
			return nil, fmt.Errorf("fixing content streams: %w", err)
		}
		fixes = append(fixes, desc...)
	}

	return fixes, nil
}

// fixAnnotationAppearances generates minimal empty appearance streams for
// annotations missing /AP.
func fixAnnotationAppearances(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, err
	}

	var count int
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Read the page, annot array, and annot dicts alloc-first: an
		// earlier fixer may have filtered /Annots or rewritten an
		// annotation, and patching the stale originals would clobber
		// those fixes (and resurrect removed annotations) on AllocAt.
		pageDict := livePageDict(ctx, alloc, doc, page)
		annotsObj, ok := pageDict["Annots"]
		if !ok {
			continue
		}
		annots := liveArray(ctx, alloc, doc, annotsObj)
		if annots == nil {
			continue
		}

		var inlineFixed map[int]ast.PdfDict

		for i, annotObj := range annots {
			ref, isRef := annotObj.(ast.PdfIndirectRef)
			var annotDict ast.PdfDict
			if isRef {
				annotDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
			} else if d, err := document.ResolveDict(ctx, doc, annotObj); err == nil {
				annotDict = d
			}
			if annotDict == nil {
				continue
			}
			if _, hasAP := annotDict["AP"]; hasAP {
				continue
			}

			// ISO 32000-1, 12.5.2 does not require an annotation to be an
			// indirect object. An inline one has no object number to address,
			// so writing only through the allocator dropped the appearance
			// while still counting it, and the stream allocated just above was
			// left orphaned in the output on every fixpoint round. Install it
			// through the /Annots array instead, and only allocate once the
			// page can actually take it.
			if !isRef && page.Ref.ObjectNum <= 0 {
				continue
			}

			emptyStream := ast.NewStream(ast.PdfDict{
				"Type":    ast.PdfName("XObject"),
				"Subtype": ast.PdfName("Form"),
				"BBox":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
			}, []byte("q Q"))
			streamRef := alloc.Alloc(emptyStream)

			newAnnot := make(ast.PdfDict, len(annotDict)+1)
			maps.Copy(newAnnot, annotDict)
			newAnnot["AP"] = ast.PdfDict{"N": streamRef}

			if isRef {
				alloc.AllocAt(ref.ObjectNum, newAnnot)
				count++
				continue
			}

			if inlineFixed == nil {
				inlineFixed = make(map[int]ast.PdfDict, 1)
			}
			inlineFixed[i] = newAnnot
		}

		count += installInlineAppearances(alloc, page, pageDict, annotsObj, annots, inlineFixed)
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("generated empty appearance streams for %d annotations", count)}, nil
}

// fixContentStreams parses page content streams and fixes rendering intent
// operators and removes unknown operators, BX/EX sections included.
//
// The removal is [dropUndefinedOperators], the one the unknown-ops fixer makes,
// so a run of operators written with no white space between them, such as
// "ETBT", is written back as the operators a viewer reads (ISO 32000-1,
// Annex A, Table A.1) rather than deleted whole. Deleting it removed the ET
// and BT a viewer draws with, and the text after them moved.
//
// A page whose content holds an inline image is not serialized again: the
// content parser reads an image's data off-token, so its BI operation keeps
// its dictionary alone, and writing the operations back would drop the data
// and leave "<< ... >> BI" with no ID or EI (ISO 32000-1, 8.9.7). Its repairs
// are spliced in place instead ([spliceAggressiveRepairs]), the invalid
// intent an image's own dictionary names among them, its images' data
// carried across byte for byte, and the spliced stream read back: one that
// does not read as the repair intends leaves the page as written, its
// findings reported residuals rather than silently corrupted. Such a page used
// to be left as written whatever it held, so an invalid rendering intent in
// it stayed a residual (ISO 19005-1, 6.2.9 t1; ISO 19005-2/3, 6.2.6 t1).
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf, bug1130815.pdf: the two files with such a run in
//     page content. The unknown-ops fixer answers the same findings ahead of
//     this rewrite and separates the runs first, so the violations litho and
//     veraPDF report after an aggressive PDF/A-1b, PDF/A-2b or PDF/A-4
//     remediation, and the extracted text, are unchanged.
//   - All corpora: 134 files draw inline images. Of their aggressive PDF/A-1b,
//     PDF/A-2b and PDF/A-4 remediations, 4 outputs change, all from the image
//     intents of [respellInlineImageIntents]. No page holding an inline image
//     gives ri an invalid operand, and every other output is byte for byte
//     the same.
func fixContentStreams(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fixRI, fixUnknown bool) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, err
	}

	var total opSpliceTally

	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		data, contentsObj, err := livePageContentData(ctx, alloc, doc, page)
		if err != nil || contentsObj == nil {
			continue
		}
		ops, err := content.ParseStreamData(ctx, data, content.ForRepair())
		if err != nil {
			continue
		}

		// serializeOps cannot write an inline image back, so a page holding
		// one has its repairs spliced in place.
		if slices.ContainsFunc(ops, beginsInlineImage) {
			newData, t := spliceAggressiveRepairs(ctx, data, fixRI, fixUnknown)
			if t == (opSpliceTally{}) {
				continue
			}
			if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum+1); err != nil {
				return nil, err
			}
			total.add(t)
			continue
		}

		var tally opSpliceTally

		// The removal the unknown-ops fixer makes, shared so that a run of
		// operators written with no white space between them comes apart here
		// too; see [dropUndefinedOperators].
		if fixUnknown {
			ops, tally.separated, tally.removed = dropUndefinedOperators(ops)
		}
		// A run such as "QBI" comes apart into a BI no dictionary follows,
		// which would begin an inline image with no data or EI (ISO 32000-1,
		// 8.9.7), so the page is left as written, as the splice leaves it
		// ([repairOperation]).
		if slices.ContainsFunc(ops, beginsInlineImage) {
			continue
		}

		if fixRI {
			for i, op := range ops {
				if invalidIntent(op) {
					ops[i].Operands[0] = ast.PdfName("RelativeColorimetric")
					tally.intents++
				}
			}
		}

		if tally == (opSpliceTally{}) {
			continue
		}

		newData, err := serializeOps(filterOps(ops))
		if err != nil {
			doc.Logger.Debug("remediation: cannot rewrite a content stream for its operators",
				"page", pageNum+1, "error", err)
			continue
		}
		if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum+1); err != nil {
			return nil, err
		}
		total.add(tally)
	}

	var fixes []string
	if total.intents > 0 {
		fixes = append(fixes, fmt.Sprintf("replaced %d invalid rendering intents in content streams", total.intents))
	}
	if total.removed > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d unknown operators from BX/EX sections", total.removed))
	}
	if total.folded > 0 {
		fixes = append(fixes, fmt.Sprintf("repaired %d malformed number(s) in content streams", total.folded))
	}
	if total.separated > 0 {
		fixes = append(fixes, fmt.Sprintf("separated %d content-stream operator(s) written with no white space before them", total.separated))
	}
	return fixes, nil
}

// spliceAggressiveRepairs returns the bytes [fixContentStreams] writes for a
// page whose content, data, holds an inline image, and what it changed: the
// invalid rendering intents of the images' dictionaries respelled in place
// ([respellInlineImageIntents]), then the repairs of [aggressiveOperation]
// spliced in place of the operations they change ([spliceOperations]). Each
// step reads its result back and is dropped when it does not read as
// intended, so a zero tally leaves the page as written.
func spliceAggressiveRepairs(ctx context.Context, data []byte, fixRI, fixUnknown bool) ([]byte, opSpliceTally) {
	var t opSpliceTally
	if fixRI {
		data, t.intents = respellInlineImageIntents(ctx, data)
	}
	if spliced, s, ok := spliceOperations(ctx, data, aggressiveOperation(fixRI, fixUnknown)); ok && s != (opSpliceTally{}) {
		data = spliced
		t.add(s)
	}
	return data, t
}

// respellInlineImageIntents returns data with every invalid rendering intent
// an inline image's dictionary names ([isValidRenderingIntent]) respelled as
// RelativeColorimetric, and how many it respelled.
//
// ISO 32000-1, 8.9.7 lets an inline image carry the /Intent entry of an image
// XObject (Table 89), which it writes in full, and ISO 19005-1, 6.2.9 and ISO
// 19005-2/3, 6.2.6 hold it to the four intents of Table 70 as they hold the
// operand of ri. The names are spliced in place and the result read back
// ([respellInlineImageEntries]), so the image data is carried across byte for
// byte, and a respelling that reads as anything else leaves data unchanged
// with a count of 0. An image holds one /Intent, so the count is the images
// respelled.
//
// # Corpus evidence
//
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-2-9-t01-fail-a.pdf and
//     PDF_A-2b__veraPDF_test_suite_6-2-6-t01-fail-a.pdf: an inline image of
//     page content names the intent Custom, which litho and veraPDF 1.30
//     reported after their aggressive PDF/A-1b and PDF/A-2b remediations.
//     Respelled, the 4 outputs pass both validators, and their image data
//     and extracted text do not change.
func respellInlineImageIntents(ctx context.Context, data []byte) ([]byte, int) {
	return respellInlineImageEntries(ctx, data, imageEntryRespell{
		splice: func(data []byte, key string, tok lexer.Token, at tokenPlace) (tokenSplice, bool) {
			if key != "/Intent" || tok.Type != lexer.TokenName {
				return tokenSplice{}, false
			}
			if name, _ := strings.CutPrefix(tok.Value, "/"); isValidRenderingIntent(name) {
				return tokenSplice{}, false
			}
			start, ok := nameSpan(data, at.end, tok.Value)
			if !ok {
				return tokenSplice{}, false
			}
			return tokenSplice{start: start, end: at.end, text: "/RelativeColorimetric"}, true
		},
		fixed: respelledIntent,
	})
}

// respelledIntent returns dict, an inline image's dictionary, with the invalid
// rendering intent it names replaced by RelativeColorimetric, and 1, or 0 when
// it names none.
func respelledIntent(dict ast.PdfDict) (ast.PdfDict, int) {
	if name, ok := dict["Intent"].(ast.PdfName); !ok || isValidRenderingIntent(string(name)) {
		return dict, 0
	}
	fixed := maps.Clone(dict)
	fixed["Intent"] = ast.PdfName("RelativeColorimetric")
	return fixed, 1
}

// aggressiveOperation returns the repair [fixContentStreams] splices into a
// content stream that holds an inline image ([spliceOperations]): with
// fixUnknown, the repair of the undefined-operator fixer ([repairOperation]),
// which folds a malformed number into the operator after it rather than
// removing it with that operator's operands, and with fixRI, an invalid
// rendering intent replaced ([invalidIntent]).
func aggressiveOperation(fixRI, fixUnknown bool) operationRepair {
	return operationRepair{fold: fixUnknown, op: func(op content.Operation) ([]content.Operation, opSpliceTally, bool) {
		ops := []content.Operation{op}
		var t opSpliceTally
		if fixUnknown {
			var ok bool
			if ops, t, ok = repairOperation(op); !ok {
				return nil, opSpliceTally{}, false
			}
		}
		if !fixRI {
			return ops, t, true
		}
		for i, o := range ops {
			if invalidIntent(o) {
				ops[i] = content.Operation{Operator: "ri", Operands: []ast.PdfObject{ast.PdfName("RelativeColorimetric")}}
				t.intents++
			}
		}
		return ops, t, true
	}}
}

// beginsInlineImage reports whether op is the BI operation of an inline image.
func beginsInlineImage(op content.Operation) bool {
	return op.Operator == "BI"
}

// invalidIntent reports whether op is an ri operator naming a rendering intent
// ISO 32000-1, 8.6.5.8, Table 70 does not define, which ISO 19005-1, 6.2.9 and
// ISO 19005-2/3, 6.2.6 forbid.
func invalidIntent(op content.Operation) bool {
	if op.Operator != "ri" || len(op.Operands) != 1 {
		return false
	}
	name, ok := op.Operands[0].(ast.PdfName)
	return ok && !isValidRenderingIntent(string(name))
}

func isValidRenderingIntent(intent string) bool {
	switch intent {
	case "AbsoluteColorimetric", "RelativeColorimetric", "Saturation", "Perceptual":
		return true
	}
	return false
}

func filterOps(ops []content.Operation) []content.Operation {
	result := make([]content.Operation, 0, len(ops))
	for _, op := range ops {
		if op.Operator != "" {
			result = append(result, op)
		}
	}
	return result
}

// serializeOps writes ops back as content-stream syntax, one operation per
// line. It fails, writing nothing, when an operand has no conforming
// spelling: a name holding a NUL byte, which ISO 32000-1, 7.3.5 excludes from
// every name ([writer.ErrNameHoldsNull]). A caller leaves that stream as it
// was.
func serializeOps(ops []content.Operation) ([]byte, error) {
	var buf bytes.Buffer
	for _, op := range ops {
		for i, operand := range op.Operands {
			if i > 0 {
				buf.WriteByte(' ')
			}
			if err := writeOperand(&buf, operand); err != nil {
				return nil, fmt.Errorf("writing an operand of %q: %w", op.Operator, err)
			}
		}
		if len(op.Operands) > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(op.Operator)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// writeOperand serialises one content-stream operand into buf in PDF syntax.
//
// A name, a dictionary key included, is spelled by [writer.SerializeName],
// which escapes every byte ISO 32000-1, 7.3.5 does not let a name carry as
// itself: white space, a delimiter, the "#" that starts an escape, and every
// byte outside 0x21 to 0x7E. The lexer decodes those escapes on the way in, so
// writing the decoded bytes back raw changed the operand. "/Font#20A Tf" came
// back as the name Font, a keyword A and a Tf short an operand.
//
// # Corpus evidence
//
//   - pdfjs issue11678.pdf: formatting reals with %g, an aggressive content
//     rewrite turned 5 violations into 64.
//   - All corpora: 53 files hold a name that needs escaping in page or form
//     content, most of them marked-content tags holding a space, such as
//     "Table Contents" and "Text body". No NUL is among them.
//   - pdfjs poppler-90-0-fuzzed.pdf: the one of them whose content a PDF/A-1b
//     or PDF/A-2b rewrite reaches, default or aggressive. Its name holds the
//     bytes 0x03, 0x92 and 0xE3, which read back the same when written raw,
//     and is now spelled with escapes: the operators parse the same, and
//     litho and veraPDF report the same on the output.
func writeOperand(buf *bytes.Buffer, obj ast.PdfObject) error {
	switch v := obj.(type) {
	case ast.PdfInt:
		fmt.Fprintf(buf, "%d", int64(v))
	case ast.PdfReal:
		// %g emits exponential form for small magnitudes ("1e-05"), which
		// ISO 32000-1, 7.3.3 forbids in a PDF real.
		buf.WriteString(writer.FormatReal(float64(v)))
	case ast.PdfName:
		if _, err := writer.SerializeName(buf, v); err != nil {
			return err
		}
	case ast.PdfString:
		buf.WriteByte('(')
		for _, b := range []byte(v) {
			// A literal end-of-line marker inside a string is not the byte it
			// looks like: ISO 32000-2, 7.3.4.2 has a reader normalise CR, and
			// CR LF, to a single LF. Writing a carriage return raw therefore
			// changes the value on the way back in, and a CR LF pair loses a
			// byte outright. Escaped, it survives, which the same clause's
			// table spells \r. LF, tab and every high byte already round-trip
			// as written, so they stay unescaped.
			if b == '\r' {
				buf.WriteString(`\r`)
				continue
			}
			if b == '(' || b == ')' || b == '\\' {
				buf.WriteByte('\\')
			}
			buf.WriteByte(b)
		}
		buf.WriteByte(')')
	case ast.PdfBool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case ast.PdfNull:
		buf.WriteString("null")
	case ast.PdfArray:
		buf.WriteByte('[')
		for i, elem := range v {
			if i > 0 {
				buf.WriteByte(' ')
			}
			if err := writeOperand(buf, elem); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case ast.PdfDict:
		// Sorted: this dictionary is written straight into a content stream,
		// so ranging the map would put the same operands in a different order
		// on every run. An inline /Artifact came out as "/Attached /Type
		// /Subtype" one run and "/Type /Subtype /Attached" the next.
		buf.WriteString("<<")
		for _, k := range slices.Sorted(maps.Keys(v)) {
			buf.WriteByte(' ')
			if _, err := writer.SerializeName(buf, k); err != nil {
				return err
			}
			buf.WriteByte(' ')
			if err := writeOperand(buf, v[k]); err != nil {
				return err
			}
		}
		buf.WriteString(">>")
	default:
		fmt.Fprintf(buf, "%v", obj)
	}
	return nil
}

func replaceContentStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, contentsObj ast.PdfObject, newData []byte, pageNum int) error {
	ref, isRef := contentsObj.(ast.PdfIndirectRef)
	if !isRef {
		// A direct /Contents array [s1 s2 ...] holds the content across several
		// indirect streams. Write the combined new content to the first stream
		// and empty the rest: the concatenation is unchanged and the page dict's
		// array keeps addressing the same objects, so no page-dict rewrite is
		// needed. Without this a direct-array /Contents aborts the whole
		// document's remediation.
		if arr, ok := contentsObj.(ast.PdfArray); ok {
			return replaceContentArrayStreams(alloc, arr, newData, pageNum)
		}
		return fmt.Errorf("page %d /Contents is not an indirect reference or array", pageNum)
	}

	// Decide stream-vs-array alloc-first: an earlier fixer (auto-tag,
	// content-tag) may have replaced /Contents with a stream that is queued in
	// alloc and not yet in the document. Writing back to the same object number
	// keeps the live page pointing at the rewritten content.
	if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
		switch pending.(type) {
		case ast.PdfStream:
			alloc.AllocAt(ref.ObjectNum, ast.NewStream(ast.PdfDict{}, newData))
			return nil
		case ast.PdfArray:
			alloc.AllocAt(ref.ObjectNum, alloc.Alloc(ast.NewStream(ast.PdfDict{}, newData)))
			return nil
		}
	}

	resolved, err := document.ResolveObject(ctx, doc, contentsObj)
	if err != nil {
		return fmt.Errorf("resolving /Contents for page %d: %w", pageNum, err)
	}

	switch resolved.(type) {
	case ast.PdfStream:
		alloc.AllocAt(ref.ObjectNum, ast.NewStream(ast.PdfDict{}, newData))
	case ast.PdfArray:
		newStream := ast.NewStream(ast.PdfDict{}, newData)
		newRef := alloc.Alloc(newStream)
		alloc.AllocAt(ref.ObjectNum, newRef)
	default:
		return fmt.Errorf("page %d /Contents has unexpected type %T", pageNum, resolved)
	}
	return nil
}

// replaceContentArrayStreams rewrites a direct /Contents array: the combined
// new content goes to the first indirect stream element and every subsequent
// stream element is emptied. Concatenating the streams then yields exactly
// newData, so page rendering is preserved without touching the page dictionary.
func replaceContentArrayStreams(alloc *writer.ObjAllocator, arr ast.PdfArray, newData []byte, pageNum int) error {
	wrote := false
	for _, elem := range arr {
		eref, ok := elem.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		if !wrote {
			alloc.AllocAt(eref.ObjectNum, ast.NewStream(ast.PdfDict{}, newData))
			wrote = true
			continue
		}
		alloc.AllocAt(eref.ObjectNum, ast.NewStream(ast.PdfDict{}, []byte{}))
	}
	if !wrote {
		return fmt.Errorf("page %d /Contents array has no stream references", pageNum)
	}
	return nil
}

// livePageContentData returns a page's current decoded content, reading the
// page dict and its content stream(s) alloc-first so a rewrite queued by an
// earlier fixer (auto-tag replaces /Contents with a marked stream; content-tag
// wraps the page) is seen instead of the orphaned original. It returns the
// bytes and the /Contents object that addresses the live stream(s), which
// [replaceContentStream] writes back to. A nil /Contents means the page has no
// content to rewrite.
//
// When no earlier fixer touched the page (the common case) it resolves entirely
// from the document. Every caller writes the operations back, so they are read
// as a repair reads them ([content.ForRepair]), which [content.ParsePageContent]
// matches save where the reading of veraPDF departs from what a repair keeps,
// from the bytes with every run such as "3-5" in an array or a dictionary
// written as the numbers veraPDF reads in it ([splitNestedNumberRuns]). A
// caller writes its repairs through [repairContentOps] or [spliceOperations],
// which carry the page's inline images across.
func livePageContentData(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) ([]byte, ast.PdfObject, error) {
	pageDict := resolveFromAllocOrDoc(ctx, alloc, doc, page.Ref)
	if pageDict == nil {
		pageDict = page.Dict
	}
	contentsObj, ok := pageDict["Contents"]
	if !ok {
		return nil, nil, nil
	}
	data, err := liveContentBytes(ctx, alloc, doc, contentsObj)
	if err != nil {
		return nil, contentsObj, err
	}
	data, _ = splitNestedNumberRuns(ctx, data)
	return data, contentsObj, nil
}

// liveContentBytes returns a page's decoded content bytes, preferring a
// replacement queued in alloc (a content rewrite not yet in the document) over
// the parsed document. A queued stream is decoded, not read raw: an earlier
// fixer may have queued a filtered replacement at this object number (the LZW
// fixer re-encodes content as FlateDecode), so its body is not necessarily
// plaintext.
//
// This holds for every shape /Contents may take, an array included. It used to
// hold only for a single indirect reference to a stream, so a page whose
// /Contents is an array read the document while a repair sat queued in alloc:
// the next fixer to rewrite that page built on the stale bytes and dropped the
// earlier repair. Nothing reported it, because the page still parsed.
func liveContentBytes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, contentsObj ast.PdfObject) ([]byte, error) {
	return resolveContentBytes(ctx, alloc, doc, contentsObj)
}

// DecodeStream is checked via interface to avoid import cycle.
// The document.Document type provides the DecodeStream method this
// package depends on.
func init() {
	// Compile-time check that document.Document carries DecodeStream.
	var _ interface {
		DecodeStream(context.Context, ast.PdfStream) ([]byte, error)
	} = (*document.Document)(nil)
}

// installInlineAppearances writes annotations repaired in place, returning how
// many landed. An inline annotation is reachable only through its page's
// /Annots array, so the array is rebuilt and written to whichever object owns
// it: the array itself when indirect, the page otherwise.
func installInlineAppearances(
	alloc *writer.ObjAllocator,
	page *document.Page,
	pageDict ast.PdfDict,
	annotsObj ast.PdfObject,
	annots ast.PdfArray,
	fixed map[int]ast.PdfDict,
) int {
	if len(fixed) == 0 {
		return 0
	}

	newArr := make(ast.PdfArray, len(annots))
	copy(newArr, annots)
	for i, dict := range fixed {
		if i < len(newArr) {
			newArr[i] = dict
		}
	}

	if annotsRef, isRef := annotsObj.(ast.PdfIndirectRef); isRef && annotsRef.ObjectNum > 0 {
		alloc.AllocAt(annotsRef.ObjectNum, newArr)
		return len(fixed)
	}

	if page.Ref.ObjectNum <= 0 {
		return 0
	}
	newPage := make(ast.PdfDict, len(pageDict))
	maps.Copy(newPage, pageDict)
	newPage["Annots"] = newArr
	alloc.AllocAt(page.Ref.ObjectNum, newPage)
	return len(fixed)
}
