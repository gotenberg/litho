package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// validOperators is the set of PDF content stream operators defined in
// ISO 32000-2, Tables 50-77.
//
//nolint:gochecknoglobals // constant lookup table
var validOperators = map[string]bool{
	"w": true, "J": true, "j": true, "M": true, "d": true, "ri": true, "i": true, "gs": true,
	"q": true, "Q": true, "cm": true,
	"m": true, "l": true, "c": true, "v": true, "y": true, "h": true, "re": true,
	"S": true, "s": true, "f": true, "F": true, "f*": true, "B": true, "B*": true,
	"b": true, "b*": true, "n": true, "W": true, "W*": true,
	"BT": true, "ET": true,
	"Tc": true, "Tw": true, "Tz": true, "TL": true, "Tf": true, "Tr": true, "Ts": true,
	"Td": true, "TD": true, "Tm": true, "T*": true, "Tj": true, "TJ": true, "'": true, "\"": true,
	"d0": true, "d1": true,
	"CS": true, "cs": true, "SC": true, "SCN": true, "sc": true, "scn": true,
	"G": true, "g": true, "RG": true, "rg": true, "K": true, "k": true,
	"sh": true,
	"BI": true, "ID": true, "EI": true,
	"Do": true,
	"MP": true, "DP": true, "BMC": true, "BDC": true, "EMC": true, "BX": true, "EX": true,
}

// matchAggressive matches violations that require content modification to fix.
// These are only applied when PreserveContent is false.
func matchAggressive(v compliance.Violation) bool {
	switch v.Rule.Clause {
	case "6.3.3", "6.5.3", "6.9": // annotation/form appearances
		return true
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

		for _, annotObj := range annots {
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
			}
			count++
		}
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("generated empty appearance streams for %d annotations", count)}, nil
}

// fixContentStreams parses page content streams and fixes rendering intent
// operators and removes unknown operators between BX/EX markers.
func fixContentStreams(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fixRI, fixUnknown bool) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, err
	}

	var totalRI, totalUnknown int

	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ops, contentsObj, err := livePageContentOps(ctx, alloc, doc, page)
		if err != nil || contentsObj == nil {
			continue
		}

		modified := false
		hasInlineImage := false
		inCompat := 0
		pageRI, pageUnknown := 0, 0

		for i, op := range ops {
			if op.Operator == "BI" {
				hasInlineImage = true
			}
			if op.Operator == "BX" {
				inCompat++
			}
			if op.Operator == "EX" && inCompat > 0 {
				inCompat--
			}

			if fixRI && op.Operator == "ri" && len(op.Operands) == 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					if !isValidRenderingIntent(string(name)) {
						ops[i].Operands[0] = ast.PdfName("RelativeColorimetric")
						pageRI++
						modified = true
					}
				}
			}

			if fixUnknown && op.Operator != "BX" && op.Operator != "EX" && op.Operator != "" {
				// Per ISO 19005-2 §6.2.2, undefined operators are
				// forbidden even inside BX/EX compatibility brackets,
				// so strip them wherever they appear.
				if !validOperators[op.Operator] {
					ops[i].Operator = ""
					pageUnknown++
					modified = true
				}
			}
		}

		if !modified {
			continue
		}

		// serializeOps cannot round-trip an inline image: the content parser
		// consumes the image's binary data off-token, so a BI operation retains
		// only its dict. Re-serialising a content stream that contains one would
		// drop the image data and emit invalid "<dict> BI" syntax. Leave such
		// streams untouched; any rendering-intent or unknown-operator defect in
		// them remains a reported residual rather than being silently corrupted.
		if hasInlineImage {
			continue
		}

		totalRI += pageRI
		totalUnknown += pageUnknown

		filtered := filterOps(ops)
		newData := serializeOps(filtered)

		if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum+1); err != nil {
			return nil, err
		}
	}

	var fixes []string
	if totalRI > 0 {
		fixes = append(fixes, fmt.Sprintf("replaced %d invalid rendering intents in content streams", totalRI))
	}
	if totalUnknown > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d unknown operators from BX/EX sections", totalUnknown))
	}
	return fixes, nil
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

func serializeOps(ops []content.Operation) []byte {
	var buf bytes.Buffer
	for _, op := range ops {
		for i, operand := range op.Operands {
			if i > 0 {
				buf.WriteByte(' ')
			}
			writeOperand(&buf, operand)
		}
		if len(op.Operands) > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(op.Operator)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func writeOperand(buf *bytes.Buffer, obj ast.PdfObject) {
	switch v := obj.(type) {
	case ast.PdfInt:
		fmt.Fprintf(buf, "%d", int64(v))
	case ast.PdfReal:
		// %g emits exponential form for small magnitudes ("1e-05"), which
		// ISO 32000-1, 7.3.3 forbids in a PDF real. An aggressive content
		// rewrite of issue11678.pdf turned 5 violations into 64 that way.
		buf.WriteString(writer.FormatReal(float64(v)))
	case ast.PdfName:
		buf.WriteByte('/')
		buf.WriteString(string(v))
	case ast.PdfString:
		buf.WriteByte('(')
		for _, b := range []byte(v) {
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
			writeOperand(buf, elem)
		}
		buf.WriteByte(']')
	case ast.PdfDict:
		// Sorted: this dictionary is written straight into a content stream,
		// so ranging the map would put the same operands in a different order
		// on every run. An inline /Artifact came out as "/Attached /Type
		// /Subtype" one run and "/Type /Subtype /Attached" the next.
		buf.WriteString("<<")
		for _, k := range slices.Sorted(maps.Keys(v)) {
			buf.WriteString(" /")
			buf.WriteString(string(k))
			buf.WriteByte(' ')
			writeOperand(buf, v[k])
		}
		buf.WriteString(">>")
	default:
		fmt.Fprintf(buf, "%v", obj)
	}
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

// livePageContentOps parses a page's current content operations, reading the
// page dict and its content stream(s) alloc-first so a rewrite queued by an
// earlier fixer (auto-tag replaces /Contents with a marked stream; content-tag
// wraps the page) is seen instead of the orphaned original. It returns the
// operations and the /Contents object that addresses the live stream(s), which
// [replaceContentStream] writes back to. A nil /Contents means the page has no
// content to rewrite.
//
// When no earlier fixer touched the page (the common case) it resolves entirely
// from the document, so the operations match [content.ParsePageContent].
func livePageContentOps(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) ([]content.Operation, ast.PdfObject, error) {
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
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return nil, contentsObj, err
	}
	return ops, contentsObj, nil
}

// liveContentBytes returns a page's decoded content bytes, preferring a single
// stream queued in alloc (a content rewrite not yet in the document) over the
// parsed document. The queued stream is decoded, not read raw: an earlier fixer
// may have queued a filtered replacement at this object number (the LZW fixer
// re-encodes content as FlateDecode), so its body is not necessarily plaintext.
// Anything not queued in alloc falls back to the document resolver, matching the
// prior doc-only behavior.
func liveContentBytes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, contentsObj ast.PdfObject) ([]byte, error) {
	if ref, ok := contentsObj.(ast.PdfIndirectRef); ok {
		if s, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream); ok {
			return doc.DecodeStream(ctx, s)
		}
	}
	return resolveContentBytes(ctx, doc, contentsObj)
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
