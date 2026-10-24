package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// nestedResourceCategory mirrors compliance's operatorResourceCategory table
// (content_state.go). The two must agree: a fixer predicate that disagrees with
// the checker's repairs the wrong thing, or declines the right one.
var nestedResourceCategory = map[string]ast.PdfName{
	"Tf":  "Font",
	"cs":  "ColorSpace",
	"CS":  "ColorSpace",
	"gs":  "ExtGState",
	"Do":  "XObject",
	"sh":  "Shading",
	"DP":  "Properties",
	"BDC": "Properties",
}

// deviceColourSpaces need no resource lookup, so a cs/CS naming one is not an
// inherited reference. Mirrors the same skip in the detector.
var deviceColourSpaces = map[ast.PdfName]bool{
	"DeviceRGB": true, "DeviceCMYK": true, "DeviceGray": true, "Pattern": true,
}

// colourResourceOperators are the colour operators that name a resource,
// which a stream whose colour operators are off ignores. Mirrors the resource
// naming half of compliance's colourOperators (ISO 32000-1, 9.6.5, Table 113,
// and 8.7.3.3).
var colourResourceOperators = map[string]bool{
	"cs": true, "CS": true, "scn": true, "SCN": true, "sh": true,
}

// matchNestedResources claims ISO 19005-2/3/4 §6.2.2 test 2, the case where a
// content stream resolves a resource name only through the page's resources.
//
// [matchPageResources] already claims the same violation, but repairs PAGES: it
// pins an inherited /Resources onto each page dict. That cannot help when the
// flagged stream is NESTED, because the page already declares the name and the
// Form XObject or Type 3 font does not. Both fixers may claim it; this one
// repairs the half the other cannot reach.
//
// The context gate is what distinguishes the rule: ISO 19005-1 numbers an
// unrelated OutputIntents rule §6.2.2 t2, and its context never carries this
// wording.
func matchNestedResources(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.2") {
		return false
	}
	return contextContains(v, "only through inherited Resources")
}

// fixNestedResources gives a nested content stream the explicit /Resources
// ISO 19005-2/3/4 §6.2.2 requires, copying from the page only the names the
// stream actually references. The nested streams are the form XObjects, the
// tiling patterns and the Type 3 fonts the page declares: a tiling pattern's
// cell is a content stream with resources of its own (ISO 32000-1, 8.7.3.1,
// Table 75), and veraPDF 1.30.2 fails t2 on a name its cell finds only on the
// page, with its /Resources empty or absent alike.
//
// A Form XObject or a pattern shared by several pages may see a different
// binding for the same name on each, so a singly-referenced one is repaired
// here and a shared one by [fixSharedNestedResources], which copies it per
// group of pages that bind its names alike.
//
// # Corpus evidence
//
//   - All corpora: every fixture carrying this violation is single-page, so the
//     corpus cannot tell the singly-referenced repair from a per-page deep copy.
//   - pdfjs issue6541.pdf, litho colouroff_2b.pdf: a tiling pattern cell
//     resolves a name through the page, and veraPDF 1.30.2 reported 6.2.2 t2
//     on each remediated output at PDF/A-2b and PDF/A-4, twice and once, and
//     reports it on neither now.
//   - litho nestedpattern_2b.pdf: the only file whose remediation copies the
//     pattern a form XObject selects.
func fixNestedResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool))

	// An XObject or a pattern reachable from more than one page goes to
	// [fixSharedNestedResources]: the pages may bind the same name to
	// different objects.
	refCount := make(map[int]int)
	for _, pe := range pages {
		pageRes := pageResourcesFor(ctx, alloc, doc, pe)
		if pageRes == nil {
			continue
		}
		for _, category := range []ast.PdfName{"XObject", "Pattern"} {
			sub, err := document.ResolveDict(ctx, doc, pageRes[category])
			if err != nil || sub == nil {
				continue
			}
			for _, entry := range sub {
				if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
					refCount[ref.ObjectNum]++
				}
			}
		}
	}

	forms, patterns, type3 := 0, 0, 0
	for _, pe := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pageRes := pageResourcesFor(ctx, alloc, doc, pe)
		if pageRes == nil {
			continue
		}
		forms += fixFormXObjectResources(ctx, alloc, doc, pageRes, refCount)
		patterns += fixTilingPatternResources(ctx, alloc, doc, pageRes, refCount)
		type3 += fixType3FontResources(ctx, alloc, doc, pageRes)
	}
	sharedForms, sharedPatterns, err := fixSharedNestedResources(ctx, alloc, doc, pages, refCount)
	if err != nil {
		return nil, err
	}
	forms += sharedForms
	patterns += sharedPatterns
	resourceless, err := fixResourcelessType3Fonts(ctx, alloc, doc, pages)
	if err != nil {
		return nil, err
	}
	type3 += resourceless

	var fixes []string
	if forms > 0 {
		fixes = append(fixes, fmt.Sprintf("gave %d form XObject(s) their own /Resources", forms))
	}
	if patterns > 0 {
		fixes = append(fixes, fmt.Sprintf("gave %d tiling pattern(s) their own /Resources", patterns))
	}
	if type3 > 0 {
		fixes = append(fixes, fmt.Sprintf("gave %d Type 3 font(s) their own /Resources", type3))
	}
	return fixes, nil
}

// pageResourcesFor returns the resources a page's streams resolve against:
// its own, or the ones it inherits through the page tree ([resourcesHolder]),
// read through the allocator.
func pageResourcesFor(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry) ast.PdfDict {
	holder, ok := resourcesHolder(ctx, alloc, doc, pe)
	if !ok {
		return nil
	}
	return liveDictValue(ctx, alloc, doc, holder.dict["Resources"])
}

// fixFormXObjectResources repairs every singly-referenced form XObject the page
// declares.
func fixFormXObjectResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageRes ast.PdfDict, refCount map[int]int) int {
	xoDict, err := document.ResolveDict(ctx, doc, pageRes["XObject"])
	if err != nil || xoDict == nil {
		return 0
	}

	fixed := 0
	for _, entry := range xoDict {
		ref, isRef := entry.(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum == 0 || refCount[ref.ObjectNum] != 1 {
			continue
		}
		// Alloc-aware read. A form XObject stream is among the most rewritten
		// objects in this package (aggressive, images, output-intent,
		// need-appearances, unknown-ops, graphics and transparency all write
		// one), and the registry drives every fixer against ONE allocator with
		// no flush between them. Rebuilding the dictionary from the on-disk
		// copy discards whatever an earlier fixer left pending. The refCount
		// guard above does not cover this: it declines a SHARED XObject, which
		// says nothing about whether a peer fixer already wrote this one.
		stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, entry)
		if !ok || stream.Dict == nil {
			continue
		}
		if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); subtype != "Form" {
			continue
		}

		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) == 0 {
			continue
		}
		// The copy goes inline into the form, so an indirect /Resources is read
		// alloc-first: output-intent injects a default colour space into it at
		// its own object number, and copying the document's version would
		// orphan that write.
		own := liveDictValue(ctx, alloc, doc, stream.Dict["Resources"])
		newRes, added := resourcesForStream(ctx, alloc, doc, data, own, pageRes)
		if added == 0 {
			continue
		}

		newDict := make(ast.PdfDict, len(stream.Dict)+1)
		maps.Copy(newDict, stream.Dict)
		newDict["Resources"] = newRes
		// The dictionary changes, the bytes do not: a lazy stream carries the
		// RAW bytes across, so a filtered stream is never decoded and
		// re-encoded behind the caller's back.
		alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(newDict, stream.Bytes))
		fixed++
	}
	return fixed
}

// fixTilingPatternResources repairs every singly-referenced tiling pattern
// the page declares. A shading pattern is a dictionary with no cell to resolve
// names in (ISO 32000-1, 8.7.4.1), so only /PatternType 1 is a target.
func fixTilingPatternResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageRes ast.PdfDict, refCount map[int]int) int {
	patDict, err := document.ResolveDict(ctx, doc, pageRes["Pattern"])
	if err != nil || patDict == nil {
		return 0
	}

	fixed := 0
	for _, entry := range patDict {
		ref, isRef := entry.(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum == 0 || refCount[ref.ObjectNum] != 1 {
			continue
		}
		// Alloc-aware for the reason given in fixFormXObjectResources: an
		// earlier fixer may have queued this stream already.
		stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, entry)
		if !ok || stream.Dict == nil {
			continue
		}
		if patternType, err := document.ResolveInt(ctx, doc, stream.Dict["PatternType"]); err != nil || patternType != 1 {
			continue
		}

		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) == 0 {
			continue
		}
		// An uncoloured pattern's cell ignores its colour operators (ISO
		// 32000-1, 8.7.3.3, /PaintType 2), so their names need no entry.
		// Best-effort: an unreadable /PaintType reads as coloured, which
		// copies every name the cell uses.
		paintType, _ := document.ResolveInt(ctx, doc, stream.Dict["PaintType"])
		own := liveDictValue(ctx, alloc, doc, stream.Dict["Resources"])
		newRes, added := resourcesForStreamColour(ctx, alloc, doc, data, own, pageRes, paintType == 2)
		if added == 0 {
			continue
		}

		newDict := make(ast.PdfDict, len(stream.Dict)+1)
		maps.Copy(newDict, stream.Dict)
		newDict["Resources"] = newRes
		// The bytes stay raw, as in fixFormXObjectResources.
		alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(newDict, stream.Bytes))
		fixed++
	}
	return fixed
}

// fixType3FontResources repairs every Type 3 font with /Resources of its own
// the page declares. The target is the FONT dictionary's /Resources, which ISO
// 32000-1, 9.6.5 scopes to the font's CharProcs. A font stating none draws
// with the resources of whatever shows it, so [fixResourcelessType3Fonts]
// repairs it against every scope that does.
func fixType3FontResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageRes ast.PdfDict) int {
	fontDict, err := document.ResolveDict(ctx, doc, pageRes["Font"])
	if err != nil || fontDict == nil {
		return 0
	}

	fixed := 0
	for _, entry := range fontDict {
		ref, isRef := entry.(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum == 0 {
			continue
		}
		// Alloc-aware read. nested-resources (registry 131) runs after
		// cid-gidmap, font-keys and tounicode-ranges, and after transparency's
		// own Type 3 pass at 107, every one of which rewrites this same font
		// dictionary. Rebuilding it from the on-disk copy discards whatever
		// they left pending.
		fd := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if fd == nil {
			continue
		}
		if subtype, _ := fd["Subtype"].(ast.PdfName); subtype != "Type3" {
			continue
		}
		charProcs, err := document.ResolveDict(ctx, doc, fd["CharProcs"])
		if err != nil || charProcs == nil {
			continue
		}

		// Alloc-first for the reason given in fixFormXObjectResources: output-
		// intent reaches a Type 3 font's /Resources through
		// patchType3FontColorSpace.
		own := liveDictValue(ctx, alloc, doc, fd["Resources"])
		if own == nil {
			continue
		}
		newRes := own
		added := 0
		for _, cpObj := range charProcs {
			stream, err := document.ResolveStream(ctx, doc, cpObj)
			if err != nil {
				continue
			}
			data, err := doc.DecodeStream(ctx, stream)
			if err != nil || len(data) == 0 {
				continue
			}
			res, n := resourcesForStream(ctx, alloc, doc, data, newRes, pageRes)
			if n > 0 {
				newRes = res
				added += n
			}
		}
		if added == 0 {
			continue
		}

		newFont := make(ast.PdfDict, len(fd)+1)
		maps.Copy(newFont, fd)
		newFont["Resources"] = newRes
		alloc.AllocAt(ref.ObjectNum, newFont)
		fixed++
	}
	return fixed
}

// resourcesForStream returns own extended with every name the stream
// references that only the page's resources define, and how many were added.
//
// Each sub-dictionary of own it extends is read alloc-first, because the
// extended copy replaces it inline: icc-dedup, devicen and sep-canon rewrite an
// indirect /ColorSpace dictionary at its own object number ahead of
// nested-resources, and copying the document's version would orphan that
// write.
//
// The stream is read whole ([content.WholeStream]): a viewer draws with the
// names used after a stray "]" that validation reads no further than, so the
// stream needs them in its resources as much as the others.
func resourcesForStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, data []byte, own, pageRes ast.PdfDict) (ast.PdfDict, int) {
	return resourcesForStreamColour(ctx, alloc, doc, data, own, pageRes, false)
}

// resourcesForStreamColour is [resourcesForStream] for a stream whose colour
// operators are off when colourOff is set: the cell of an uncoloured tiling
// pattern ignores them (ISO 32000-1, 8.7.3.3), and veraPDF resolves the name
// of no cs, CS, sh, scn or SCN there, so the names they use are no inherited
// reference to copy.
func resourcesForStreamColour(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, data []byte, own, pageRes ast.PdfDict, colourOff bool) (ast.PdfDict, int) {
	ops, err := content.ParseStreamData(ctx, data, content.WholeStream())
	if err != nil {
		doc.Logger.Debug("remediation.nestedresources: cannot parse content stream", "error", err)
		return own, 0
	}

	result := make(ast.PdfDict, len(own)+1)
	maps.Copy(result, own)
	added := 0

	for _, op := range ops {
		category, name, ok := streamResourceName(op)
		if !ok || colourOff && colourResourceOperators[op.Operator] {
			continue
		}
		// A cs/CS naming a device space resolves without any resource entry.
		if (op.Operator == "cs" || op.Operator == "CS") && deviceColourSpaces[name] {
			continue
		}
		// ISO 32000-1, 7.3.5 permits an empty name, which a resource dictionary
		// cannot usefully key on, so it references nothing.
		if name == "" {
			continue
		}

		if ownSub := liveDictValue(ctx, alloc, doc, result[category]); ownSub != nil {
			if _, exists := ownSub[name]; exists {
				continue
			}
		}
		pageSub, err := document.ResolveDict(ctx, doc, pageRes[category])
		if err != nil || pageSub == nil {
			continue
		}
		entry, exists := pageSub[name]
		// ISO 32000-1, 7.3.7: a null value is the same as an absent one, so the
		// page does not define the name either and copying it repairs nothing.
		if !exists || entryResolvesToNull(ctx, doc, entry) {
			continue
		}

		sub := liveDictValue(ctx, alloc, doc, result[category])
		if sub == nil {
			sub = make(ast.PdfDict)
		}
		newSub := make(ast.PdfDict, len(sub)+1)
		maps.Copy(newSub, sub)
		newSub[name] = entry
		result[category] = newSub
		added++
	}

	if added == 0 {
		return own, 0
	}
	return result, added
}

// streamResourceName returns the resource category and the name op looks up,
// and false when it looks up none.
//
// BDC and DP name a tag first and the properties second, and only a name
// operand needs a resource lookup: an inline dictionary does not. An scn or
// SCN whose last operand is a name selects the pattern of that name, after the
// components an uncoloured pattern takes (ISO 32000-1, 8.6.8, Table 74), and
// veraPDF 1.30.2 fails ISO 19005-2, 6.2.2 t2 on a form XObject whose pattern
// only the page defines. compliance looks the name up in a Pattern colour space
// alone; copying it outside one adds an entry nothing reads.
func streamResourceName(op content.Operation) (ast.PdfName, ast.PdfName, bool) {
	if len(op.Operands) == 0 {
		return "", "", false
	}
	if op.Operator == "scn" || op.Operator == "SCN" {
		name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName)
		return "Pattern", name, ok
	}
	category, ok := nestedResourceCategory[op.Operator]
	if !ok {
		return "", "", false
	}
	operand := op.Operands[0]
	if op.Operator == "BDC" || op.Operator == "DP" {
		if len(op.Operands) < 2 {
			return "", "", false
		}
		operand = op.Operands[1]
	}
	name, ok := operand.(ast.PdfName)
	return category, name, ok
}

// entryResolvesToNull reports whether a resource entry is null, which ISO
// 32000-1, 7.3.7 makes equivalent to absent.
func entryResolvesToNull(ctx context.Context, doc *document.Document, entry ast.PdfObject) bool {
	if entry == nil {
		return true
	}
	resolved, err := document.ResolveObject(ctx, doc, entry)
	if err != nil {
		return false
	}
	if resolved == nil {
		return true
	}
	_, isNull := resolved.(ast.PdfNull)
	return isNull
}
