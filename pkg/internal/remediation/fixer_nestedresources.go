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
// stream actually references.
//
// A Form XObject shared by several pages may see a different binding for the
// same name on each, so only a singly-referenced one is repaired. Deep-copying
// per page would also be correct and reaches more documents.
//
// # Corpus evidence
//
//   - All corpora: every fixture carrying this violation is single-page, so the
//     corpus cannot tell the singly-referenced repair from a per-page deep copy.
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
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)

	// An XObject reachable from more than one page is left alone: the pages may
	// bind the same name to different objects.
	refCount := make(map[int]int)
	for _, pe := range pages {
		pageRes := pageResourcesFor(ctx, alloc, doc, pe.dict)
		if pageRes == nil {
			continue
		}
		if xo, err := document.ResolveDict(ctx, doc, pageRes["XObject"]); err == nil && xo != nil {
			for _, entry := range xo {
				if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
					refCount[ref.ObjectNum]++
				}
			}
		}
	}

	forms, type3 := 0, 0
	for _, pe := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pageRes := pageResourcesFor(ctx, alloc, doc, pe.dict)
		if pageRes == nil {
			continue
		}
		forms += fixFormXObjectResources(ctx, alloc, doc, pageRes, refCount)
		type3 += fixType3FontResources(ctx, alloc, doc, pageRes)
	}

	var fixes []string
	if forms > 0 {
		fixes = append(fixes, fmt.Sprintf("gave %d form XObject(s) their own /Resources", forms))
	}
	if type3 > 0 {
		fixes = append(fixes, fmt.Sprintf("gave %d Type 3 font(s) their own /Resources", type3))
	}
	return fixes, nil
}

// pageResourcesFor returns the resources a page's streams resolve against,
// falling back to the inherited dictionary the /Parent chain provides.
func pageResourcesFor(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageDict ast.PdfDict) ast.PdfDict {
	obj, ok := pageDict["Resources"]
	if !ok {
		obj = findInheritedResources(ctx, alloc, doc, pageDict)
	}
	if obj == nil {
		return nil
	}
	res, err := document.ResolveDict(ctx, doc, obj)
	if err != nil {
		return nil
	}
	return res
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
		own, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		newRes, added := resourcesForStream(ctx, doc, data, own, pageRes)
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

// fixType3FontResources repairs every Type 3 font the page declares. The
// target is the FONT dictionary's /Resources, which ISO 32000-1, 9.6.5 scopes
// to the font's CharProcs.
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

		own, _ := document.ResolveDict(ctx, doc, fd["Resources"])
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
			res, n := resourcesForStream(ctx, doc, data, newRes, pageRes)
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
func resourcesForStream(ctx context.Context, doc *document.Document, data []byte, own, pageRes ast.PdfDict) (ast.PdfDict, int) {
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("remediation.nestedresources: cannot parse content stream", "error", err)
		return own, 0
	}

	result := make(ast.PdfDict, len(own)+1)
	maps.Copy(result, own)
	added := 0

	for _, op := range ops {
		category, ok := nestedResourceCategory[op.Operator]
		if !ok || len(op.Operands) == 0 {
			continue
		}
		name, ok := op.Operands[0].(ast.PdfName)
		if !ok {
			continue
		}
		// BDC and DP name a tag first and the properties second, and only a
		// name operand needs a resource lookup: an inline dictionary does not.
		if op.Operator == "BDC" || op.Operator == "DP" {
			if len(op.Operands) < 2 {
				continue
			}
			if name, ok = op.Operands[1].(ast.PdfName); !ok {
				continue
			}
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

		if ownSub, err := document.ResolveDict(ctx, doc, result[category]); err == nil && ownSub != nil {
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

		sub, err := document.ResolveDict(ctx, doc, result[category])
		if err != nil || sub == nil {
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
