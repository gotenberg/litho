package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchContentTag(v compliance.Violation) bool {
	return clausePrefix(v, "8.2.2") || // PDF/UA content marking
		descriptionContains(v, "shall be an artifact")
}

// fixContentTag wraps untagged page content streams in a marked-content
// sequence linked to the Document structure element. This is a shallow
// tagging: the entire page content becomes one structure element child.
// Not ideal for accessibility, but satisfies the spec requirement that
// all content must be tagged or artifact.
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

		// Add MCR (marked-content reference) to the Document element's /K.
		kChildren = append(kChildren, ast.PdfDict{
			"Type": ast.PdfName("MCR"),
			"Pg":   page.Ref,
			"MCID": ast.PdfInt(0),
		})

		parentTreeNums = append(parentTreeNums,
			ast.PdfInt(nextKey),
			ast.PdfArray{docElemRef},
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

// resolveContentBytes reads and concatenates all content streams for a page.
func resolveContentBytes(ctx context.Context, doc *document.Document, contentsObj ast.PdfObject) ([]byte, error) {
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
			stream, err := document.ResolveStream(ctx, doc, item)
			if err != nil {
				continue
			}
			data, err := doc.DecodeStream(ctx, stream)
			if err != nil {
				continue
			}
			all = append(all, data...)
			all = append(all, '\n')
		}
		return all, nil
	}
	return nil, fmt.Errorf("unexpected Contents type: %T", resolved)
}
