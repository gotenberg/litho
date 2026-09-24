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

// matchContentTag claims content that is neither tagged as real content nor
// marked as an artifact. ISO 14289-2 §8.2.2 and ISO 14289-1 §7.1 test 3 state
// the same requirement in the two standards, so both are claimed. Only the
// §8.2.2 wording ("shall be an artifact") reaches the description test, so the
// PDF/UA-1 twin ("shall be marked as Artifact or tagged as real content") was
// silently unclaimed: [matchAutoTag] composes this matcher and its contract
// already names §7.1, so the omission left auto-tagging unreachable for a
// PDF/UA-1 document whose only signal was untagged content.
//
// The §7.1 claim names its test number rather than calling [clausePrefix],
// which is a string prefix test: "7.1" also prefixes 7.10, 7.11, 7.18.x and
// 7.21.x, every one of which belongs to a different fixer.
func matchContentTag(v compliance.Violation) bool {
	return clausePrefix(v, "8.2.2") || // PDF/UA-2 content marking
		(v.Rule.Clause == "7.1" && v.Rule.TestNumber == 3) || // ISO 14289-1 twin
		descriptionContains(v, "shall be an artifact")
}

// fixContentTag wraps untagged page content streams in a marked-content
// sequence linked to the Document structure element. This is a shallow
// tagging: the entire page content becomes one structure element child.
// Not ideal for accessibility, but satisfies the spec requirement that
// all content must be tagged or artifact.
//
// # Corpus evidence
//
//   - All corpora: attaching the MCR straight to the Document accounted for
//     158 of the 173 files where PDF/UA-2 remediation introduced a violation.
func fixContentTag(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	// Read from allocator first (struct-tree fixer may have updated it).
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		return nil, nil
	}

	// Find the StructTreeRoot and Document element.
	// Check the allocated catalog (may have been updated by struct-tree fixer).
	stObj, ok := catalogDict["StructTreeRoot"]
	if !ok {
		return nil, nil
	}
	var stRef ast.PdfIndirectRef
	if ref, ok := stObj.(ast.PdfIndirectRef); ok {
		stRef = ref
	}
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		if d, err := document.ResolveDict(ctx, doc, stObj); err == nil {
			stDict = d
		}
	}
	if stDict == nil {
		return nil, nil
	}

	// Find the root element (/K).
	kObj, ok := stDict["K"]
	if !ok {
		return nil, nil
	}
	var docElemRef ast.PdfIndirectRef
	if ref, ok := kObj.(ast.PdfIndirectRef); ok {
		docElemRef = ref
	}
	docElem := resolveFromAllocOrDoc(ctx, alloc, doc, docElemRef)
	if docElem == nil {
		if d, err := document.ResolveDict(ctx, doc, kObj); err == nil {
			docElem = d
		}
	}
	if docElem == nil {
		return nil, nil
	}

	// Both the Document element and the StructTreeRoot must be writable
	// for the wrapper to be reachable from the structure tree.
	if docElemRef.ObjectNum == 0 || stRef.ObjectNum == 0 {
		return nil, nil
	}

	// Preserve the Document element's existing children: a single
	// non-array child (reference or inline dict) is normalized to a
	// one-element array, never dropped.
	var kChildren ast.PdfArray
	switch existing := docElem["K"].(type) {
	case nil:
	case ast.PdfArray:
		kChildren = append(kChildren, existing...)
	default:
		kChildren = append(kChildren, existing)
	}

	// Merge into the existing ParentTree rather than replacing it: its
	// keys also serve annotation /StructParent entries and already-tagged
	// pages. New entries are appended after the highest existing key so
	// /Nums stays sorted (ISO 32000-2 §7.9.7).
	parentTreeNums, nextKey, mergeable := existingParentTreeNums(ctx, alloc, doc, stDict)
	if !mergeable {
		doc.Logger.Debug("remediation: content-tag skipped, ParentTree is not safely mergeable")
		return nil, nil
	}

	var count int
	for _, page := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Check if page already has tagged content (any BDC/BMC in content
		// stream). Resolved alloc-first so a prior fixer's content rewrite
		// (e.g. auto-tagging) is seen and not wrapped a second time.
		if pageHasMarkedContent(ctx, alloc, doc, page) {
			continue
		}
		if page.Ref.ObjectNum == 0 {
			continue
		}

		// Read alloc-first so a page rewrite queued by an earlier fixer
		// is extended, not clobbered.
		pageDict := livePageDict(ctx, alloc, doc, page)
		if _, has := pageDict["StructParents"]; has {
			// The page already owns a ParentTree entry whose array is
			// indexed by MCID; extending it safely would require
			// rebuilding that entry, so leave the page for manual repair.
			continue
		}
		contentsObj, ok := pageDict["Contents"]
		if !ok {
			continue
		}

		contentBytes, err := liveContentBytes(ctx, alloc, doc, contentsObj)
		if err != nil || len(contentBytes) == 0 {
			continue
		}

		// MCID 0: the wrapper is the only marked content on the page, and
		// the ParentTree entry array for the page is indexed by MCID.
		var wrapped []byte
		wrapped = append(wrapped, "/Document <</MCID 0>> BDC\n"...)
		wrapped = append(wrapped, contentBytes...)
		wrapped = append(wrapped, "\nEMC\n"...)

		newStream := ast.NewStream(ast.PdfDict{
			"Length": ast.PdfInt(len(wrapped)),
		}, wrapped)
		newStreamRef := alloc.Alloc(newStream)

		newPage := make(ast.PdfDict, len(pageDict)+2)
		maps.Copy(newPage, pageDict)
		newPage["Contents"] = newStreamRef
		newPage["StructParents"] = ast.PdfInt(nextKey)
		alloc.AllocAt(page.Ref.ObjectNum, newPage)

		// Hang the marked content off a P of its own rather than off the
		// Document. ISO/TS 32005 Table 5 forbids a Document from holding a
		// direct content item ("Table 5. Document-content"), so attaching the
		// MCR straight to it repaired the untagged content and introduced a
		// structure fault in its place, the dominant source of violations
		// PDF/UA-2 remediation introduced.
		//
		// P is the wrapper because it may hold content (it is outside Table 5's
		// noContent set) and, unlike Div, Part and NonStruct, it is not a
		// pass-through type: Table 5 looks THROUGH those and would attribute
		// the content to the Document again.
		wrapperRef := alloc.Alloc(ast.PdfDict{
			"Type": ast.PdfName("StructElem"),
			"S":    ast.PdfName("P"),
			"P":    docElemRef,
			"Pg":   page.Ref,
			"K": ast.PdfDict{
				"Type": ast.PdfName("MCR"),
				"Pg":   page.Ref,
				"MCID": ast.PdfInt(0),
			},
		})
		kChildren = append(kChildren, wrapperRef)

		// The parent tree maps the page's marked content to the element that
		// owns it, which is now the wrapper rather than the Document.
		parentTreeNums = append(parentTreeNums,
			ast.PdfInt(nextKey),
			ast.PdfArray{wrapperRef},
		)
		nextKey++
		count++
	}

	if count == 0 {
		return nil, nil
	}

	// Update Document element with new /K children.
	newDocElem := make(ast.PdfDict, len(docElem))
	maps.Copy(newDocElem, docElem)
	newDocElem["K"] = kChildren
	alloc.AllocAt(docElemRef.ObjectNum, newDocElem)

	newST := make(ast.PdfDict, len(stDict))
	maps.Copy(newST, stDict)
	newST["ParentTree"] = ast.PdfDict{
		"Nums": parentTreeNums,
	}
	alloc.AllocAt(stRef.ObjectNum, newST)

	return []string{fmt.Sprintf("tagged %d page(s) with /Document marked-content wrapper", count)}, nil
}

// existingParentTreeNums returns a copy of the structure tree root's
// /ParentTree /Nums pairs and the next free key (highest existing key + 1).
// mergeable is false when a ParentTree exists but cannot be extended safely:
// it uses a /Kids subtree (a deep number tree this fixer would have to
// rebuild), its /Nums is malformed, or a key is not an integer. A missing
// ParentTree or empty /Nums yields an empty slice and key 0.
func existingParentTreeNums(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stDict ast.PdfDict) (ast.PdfArray, int, bool) {
	ptObj, ok := stDict["ParentTree"]
	if !ok {
		return nil, 0, true
	}

	var ptDict ast.PdfDict
	if ref, isRef := ptObj.(ast.PdfIndirectRef); isRef {
		ptDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	} else if d, err := document.ResolveDict(ctx, doc, ptObj); err == nil {
		ptDict = d
	}
	if ptDict == nil {
		return nil, 0, false
	}
	if _, hasKids := ptDict["Kids"]; hasKids {
		return nil, 0, false
	}

	numsObj, ok := ptDict["Nums"]
	if !ok {
		return nil, 0, true
	}
	nums, err := document.ResolveArray(ctx, doc, numsObj)
	if err != nil || len(nums)%2 != 0 {
		return nil, 0, false
	}

	nextKey := 0
	out := make(ast.PdfArray, 0, len(nums))
	out = append(out, nums...)
	for i := 0; i < len(nums); i += 2 {
		key, isInt := nums[i].(ast.PdfInt)
		if !isInt {
			return nil, 0, false
		}
		if int(key) >= nextKey {
			nextKey = int(key) + 1
		}
	}
	return out, nextKey, true
}

// pageHasMarkedContent checks if a page's content stream already contains any
// marked-content operators (BDC, BMC). It resolves the page dict and its content
// alloc-first, so a rewrite queued by an earlier fixer (the auto-tag fixer
// replaces a page's /Contents with a marked stream that is not yet in the
// document) is seen and the page is treated as already tagged.
func pageHasMarkedContent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) bool {
	pageDict := resolveFromAllocOrDoc(ctx, alloc, doc, page.Ref)
	if pageDict == nil {
		pageDict = page.Dict
	}
	contentsObj, ok := pageDict["Contents"]
	if !ok {
		return true // No content = nothing to tag.
	}

	data, err := liveContentBytes(ctx, alloc, doc, contentsObj)
	if err != nil {
		return true // Can't read = assume tagged.
	}

	// Simple byte scan for BDC or BMC keywords.
	for i := 0; i+2 < len(data); i++ {
		if data[i] == 'B' && ((data[i+1] == 'D' && data[i+2] == 'C') || (data[i+1] == 'M' && data[i+2] == 'C')) {
			return true
		}
	}
	return false
}

// maxContentIndirectionDepth bounds how far [resolveContentBytes] follows a
// reference before giving up. It is an anti-DoS guard, not spec-mandated: a
// queued /Contents legitimately reaches two levels (a reference whose pending
// replacement is another reference, which [replaceContentStream] writes when it
// swaps an array for a single stream), while a crafted or mistaken file can
// point a reference at itself.
const maxContentIndirectionDepth = 8

// resolveContentBytes reads and concatenates all content streams for a page,
// preferring a replacement queued in alloc over the version in the document.
//
// Every shape /Contents may take is resolved the same way (ISO 32000-1,
// 7.7.3.3 allows a stream or an array of streams, either one direct or
// indirect), because a fixer that queued a repair must see it again: an
// earlier fixer's rewrite lives only in alloc until the document is written.
// Reading the document for an array while reading alloc for a single stream
// made the second fixer of a page overwrite the first one's repair.
//
// A nil alloc reads the document alone, which is what a caller holding no
// allocator wants. With nothing queued the result matches
// [content.ParsePageContent], so the fixer and the checker still agree on an
// untouched page.
func resolveContentBytes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, contentsObj ast.PdfObject) ([]byte, error) {
	return resolveContentBytesAt(ctx, alloc, doc, contentsObj, 0)
}

func resolveContentBytesAt(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, contentsObj ast.PdfObject, depth int) ([]byte, error) {
	if depth > maxContentIndirectionDepth {
		return nil, fmt.Errorf("/Contents indirection exceeds %d levels", maxContentIndirectionDepth)
	}

	if ref, ok := contentsObj.(ast.PdfIndirectRef); ok && alloc != nil {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			return resolveContentBytesAt(ctx, alloc, doc, pending, depth+1)
		}
	}

	resolved, err := document.ResolveObject(ctx, doc, contentsObj)
	if err != nil {
		return nil, err
	}

	switch v := resolved.(type) {
	case ast.PdfStream:
		return doc.DecodeStream(ctx, v)
	case ast.PdfArray:
		var all []byte
		for _, item := range v {
			// Each element goes back through the walk so a queued replacement
			// of one stream of the array is seen, not just a queued
			// replacement of the array itself.
			data, err := resolveContentBytesAt(ctx, alloc, doc, item, depth+1)
			if err != nil {
				// Tolerate an element that will not resolve, as before: skip it
				// and keep the rest of the page's content.
				continue
			}
			all = append(all, data...)
			all = append(all, '\n')

			// Bounded for the reason given on content.MaxPageContentSize: the
			// per-stream decompression cap says nothing about their sum, so a
			// page naming thousands of streams grows this buffer with the file's
			// object count rather than with anything the page could draw.
			if len(all) > content.MaxPageContentSize {
				return nil, fmt.Errorf("page content exceeds %d MB limit", content.MaxPageContentSize>>20)
			}
		}
		return all, nil
	}
	return nil, fmt.Errorf("unexpected Contents type: %T", resolved)
}
