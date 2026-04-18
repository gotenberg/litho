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
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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

	// Build page ref map for MCR dicts.
	pageRefs := make(map[int]ast.PdfIndirectRef, len(pages))
	for i, p := range pages {
		pageRefs[i] = p.Ref
	}

	var count int
	var kChildren ast.PdfArray
	if existing, ok := docElem["K"].(ast.PdfArray); ok {
		kChildren = append(kChildren, existing...)
	}

	// ParentTree /Nums array: [pageIdx, structElemRef, pageIdx, structElemRef, ...]
	var parentTreeNums ast.PdfArray

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Check if page already has tagged content (any BDC/BMC in content stream).
		if pageHasMarkedContent(ctx, doc, page) {
			continue
		}

		contentsObj, ok := page.Dict["Contents"]
		if !ok {
			continue
		}

		// Resolve existing content stream(s) to raw bytes.
		contentBytes, err := resolveContentBytes(ctx, doc, contentsObj)
		if err != nil || len(contentBytes) == 0 {
			continue
		}

		mcid := pageIdx // Use page index as MCID for simplicity.

		// Wrap content in marked-content sequence.
		var wrapped []byte
		wrapped = append(wrapped, fmt.Sprintf("/Document <</MCID %d>> BDC\n", mcid)...)
		wrapped = append(wrapped, contentBytes...)
		wrapped = append(wrapped, "\nEMC\n"...)

		// Create new content stream.
		newStream := ast.NewStream(ast.PdfDict{
			"Length": ast.PdfInt(len(wrapped)),
		}, wrapped)
		newStreamRef := alloc.Alloc(newStream)

		// Update page dict to point to new content stream.
		newPage := make(ast.PdfDict, len(page.Dict))
		maps.Copy(newPage, page.Dict)
		newPage["Contents"] = newStreamRef

		// Mark page as tagged.
		if _, hasStructParents := newPage["StructParents"]; !hasStructParents {
			newPage["StructParents"] = ast.PdfInt(pageIdx)
		}

		alloc.AllocAt(page.Ref.ObjectNum, newPage)

		// Add MCR (marked-content reference) to the Document element's /K.
		mcr := ast.PdfDict{
			"Type": ast.PdfName("MCR"),
			"Pg":   page.Ref,
			"MCID": ast.PdfInt(mcid),
		}
		kChildren = append(kChildren, mcr)

		// ParentTree entry: maps StructParents index → structure element.
		parentTreeNums = append(parentTreeNums,
			ast.PdfInt(pageIdx),
			ast.PdfArray{docElemRef},
		)

		count++
	}

	if count == 0 {
		return nil, nil
	}

	// Update Document element with new /K children.
	newDocElem := make(ast.PdfDict, len(docElem))
	maps.Copy(newDocElem, docElem)
	newDocElem["K"] = kChildren
	if docElemRef.ObjectNum > 0 {
		alloc.AllocAt(docElemRef.ObjectNum, newDocElem)
	}

	// Update StructTreeRoot ParentTree.
	newST := make(ast.PdfDict, len(stDict))
	maps.Copy(newST, stDict)
	newST["ParentTree"] = ast.PdfDict{
		"Nums": parentTreeNums,
	}
	if stRef.ObjectNum > 0 {
		alloc.AllocAt(stRef.ObjectNum, newST)
	}

	return []string{fmt.Sprintf("tagged %d page(s) with /Document marked-content wrapper", count)}, nil
}

// pageHasMarkedContent checks if a page's content stream already contains
// any marked-content operators (BDC, BMC).
func pageHasMarkedContent(ctx context.Context, doc *document.Document, page *document.Page) bool {
	contentsObj, ok := page.Dict["Contents"]
	if !ok {
		return true // No content = nothing to tag.
	}

	data, err := resolveContentBytes(ctx, doc, contentsObj)
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
