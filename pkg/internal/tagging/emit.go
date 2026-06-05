package tagging

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxTagDepth bounds structure-tree recursion. It is an anti-DoS guard, not
// spec-mandated: real logical trees are shallow (Document > section > H/P,
// rarely past a handful of levels), so 64 is far above any genuine document
// while still capping a pathological or hostile classifier input.
const maxTagDepth = 64

// maxTaggedContentSize bounds a page's decoded content before marking. It is
// an anti-DoS guard matching the limit in [content.ParsePageContent]; no
// legitimate page needs more.
const maxTaggedContentSize = 32 << 20

// Emit synthesizes a PDF structure tree from the logical tree rooted at root,
// injects per-element marked content into the pages its leaves reference, and
// wires the catalog (/StructTreeRoot and /MarkInfo). The mutations are queued
// in alloc as replacement and new objects; the caller serializes them.
//
// root must have role [RoleDocument]. Emit returns human-readable descriptions
// of what it produced. It does not set /Lang, /ViewerPreferences, or the XMP
// pdfuaid identifier: those are the province of the existing catalog-lang,
// viewer-prefs, and xmp fixers.
//
// Emit assumes an untagged document: it overwrites catalog /StructTreeRoot and
// /MarkInfo and rewrites page content, so a caller must gate on the document
// being untagged ([AutoTag] does this). Emit mutates alloc and is not safe for
// concurrent use.
func Emit(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, root *Element) ([]string, error) {
	if root == nil || root.Role != RoleDocument {
		return nil, fmt.Errorf("tagging: root element must have role Document")
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("tagging: resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("tagging: resolving pages: %w", err)
	}

	e := &emitter{alloc: alloc, doc: doc, pages: pages, perPage: make(map[int][]leafEntry)}

	// Reserve the StructTreeRoot object so child elements can name it as
	// their ancestor /P, then build the element subtree.
	stRef := alloc.Alloc(ast.PdfDict{})
	docRef, err := e.build(ctx, root, stRef, 0)
	if err != nil {
		return nil, err
	}

	parentNums, err := e.applyMarks(ctx)
	if err != nil {
		return nil, err
	}

	stDict := ast.PdfDict{
		"Type":       ast.PdfName("StructTreeRoot"),
		"K":          docRef,
		"ParentTree": ast.PdfDict{"Nums": parentNums},
	}
	// Per ISO 32000-1 §14.7.4.4, /ParentTreeNextKey is one greater than the
	// largest key in the parent tree, so a later pass can allocate keys.
	if e.maxParentKey >= 0 {
		stDict["ParentTreeNextKey"] = ast.PdfInt(e.maxParentKey + 1)
	}
	alloc.AllocAt(stRef.ObjectNum, stDict)

	if err := e.wireCatalog(ctx, stRef); err != nil {
		return nil, err
	}

	return []string{
		fmt.Sprintf("synthesized structure tree: %d element(s) across %d page(s)", e.elemCount, len(e.perPage)),
	}, nil
}

// leafEntry pairs a leaf's marked-content span with the structure element
// that owns it, so the ParentTree can map MCID back to the element.
type leafEntry struct {
	span markSpan
	ref  ast.PdfIndirectRef
}

type emitter struct {
	alloc        *writer.ObjAllocator
	doc          *document.Document
	pages        []*document.Page
	perPage      map[int][]leafEntry // page index -> leaves in MCID order
	elemCount    int
	maxParentKey int // highest StructParents key written, or -1
}

// build allocates a structure element for node and its subtree, returning the
// element's reference. parentRef becomes the element's /P. Leaves record their
// span and MCID for later marked-content injection.
func (e *emitter) build(ctx context.Context, node *Element, parentRef ast.PdfIndirectRef, depth int) (ast.PdfIndirectRef, error) {
	if err := ctx.Err(); err != nil {
		return ast.PdfIndirectRef{}, err
	}
	if depth > maxTagDepth {
		return ast.PdfIndirectRef{}, fmt.Errorf("tagging: structure tree exceeds max depth %d", maxTagDepth)
	}
	if node.Span != nil && len(node.Kids) > 0 {
		return ast.PdfIndirectRef{}, fmt.Errorf("tagging: element %q has both a span and children", node.Role)
	}

	selfRef := e.alloc.Alloc(ast.PdfDict{}) // reserve object number
	e.elemCount++

	dict := ast.PdfDict{
		"Type": ast.PdfName("StructElem"),
		"S":    ast.PdfName(string(node.Role)),
		"P":    parentRef,
	}
	if node.Lang != "" {
		dict["Lang"] = ast.PdfString(node.Lang)
	}
	if node.Alt != "" {
		dict["Alt"] = ast.PdfString(node.Alt)
	}
	if node.ActualText != "" {
		dict["ActualText"] = ast.PdfString(node.ActualText)
	}
	if node.Title != "" {
		dict["T"] = ast.PdfString(node.Title)
	}

	switch {
	case node.Span != nil:
		pi := node.Span.PageIndex
		if pi < 0 || pi >= len(e.pages) {
			return ast.PdfIndirectRef{}, fmt.Errorf("tagging: element %q references page %d of %d", node.Role, pi, len(e.pages))
		}
		mcid := len(e.perPage[pi])
		dict["Pg"] = e.pages[pi].Ref
		dict["K"] = ast.PdfInt(mcid)
		e.perPage[pi] = append(e.perPage[pi], leafEntry{
			span: markSpan{start: node.Span.Start, end: node.Span.End, mcid: mcid, tag: node.Role},
			ref:  selfRef,
		})
	default:
		kids := make(ast.PdfArray, 0, len(node.Kids))
		for _, child := range node.Kids {
			childRef, err := e.build(ctx, child, selfRef, depth+1)
			if err != nil {
				return ast.PdfIndirectRef{}, err
			}
			kids = append(kids, childRef)
		}
		dict["K"] = kids
	}

	e.alloc.AllocAt(selfRef.ObjectNum, dict)
	return selfRef, nil
}

// applyMarks rewrites every page's content so all painting content is marked:
// structure leaves become BDC/EMC sequences carrying their MCID, and the
// remaining regions (graphics, and any text the classifier did not place in
// the tree) are wrapped as /Artifact. This satisfies the "all content marked
// or artifact" rule (ISO 14289-1 §7.1). It returns the StructTreeRoot
// /ParentTree /Nums array mapping each tagged page's StructParents key to its
// structure elements in MCID order.
func (e *emitter) applyMarks(ctx context.Context) (ast.PdfArray, error) {
	var nums ast.PdfArray
	e.maxParentKey = -1

	for pi := 0; pi < len(e.pages); pi++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := pageContentBytes(ctx, e.doc, e.pages[pi])
		if err != nil {
			return nil, fmt.Errorf("tagging: page %d content: %w", pi, err)
		}
		if len(data) == 0 {
			continue
		}

		leaves := e.perPage[pi]
		sort.Slice(leaves, func(i, j int) bool { return leaves[i].span.start < leaves[j].span.start })

		spans, elemRefs := pageSpans(data, leaves)
		if len(spans) == 0 {
			continue // no paintable content to mark
		}

		marked, err := injectMarks(data, spans)
		if err != nil {
			return nil, fmt.Errorf("tagging: page %d: %w", pi, err)
		}

		streamRef := e.alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(marked))}, marked))
		page := e.pages[pi]
		newPage := make(ast.PdfDict, len(page.Dict)+2)
		maps.Copy(newPage, page.Dict)
		newPage["Contents"] = streamRef
		if len(leaves) > 0 {
			newPage["StructParents"] = ast.PdfInt(pi)
			nums = append(nums, ast.PdfInt(pi), elemRefs)
			if pi > e.maxParentKey {
				e.maxParentKey = pi
			}
		}
		e.alloc.AllocAt(page.Ref.ObjectNum, newPage)
	}
	return nums, nil
}

// pageSpans builds the marked-content spans for one page: the structure leaves,
// plus an /Artifact span over each non-blank gap before, between, and after
// them, so no painting operator is left outside marked content. elemRefs maps
// MCID to its structure element for the page's ParentTree entry.
func pageSpans(data []byte, leaves []leafEntry) ([]markSpan, ast.PdfArray) {
	elemRefs := make(ast.PdfArray, len(leaves))
	var spans []markSpan
	cursor := 0
	addArtifact := func(start, end int) {
		if start < end && !isBlank(data[start:end]) {
			spans = append(spans, markSpan{start: start, end: end, artifact: true})
		}
	}
	for _, leaf := range leaves {
		s := leaf.span
		addArtifact(cursor, s.start)
		spans = append(spans, s)
		elemRefs[s.mcid] = leaf.ref
		if s.end > cursor {
			cursor = s.end
		}
	}
	addArtifact(cursor, len(data))
	return spans, elemRefs
}

// isBlank reports whether b is empty or only PDF whitespace (ISO 32000-1
// Table 1), in which case it carries no painting content and needs no artifact.
func isBlank(b []byte) bool {
	for _, c := range b {
		switch c {
		case 0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20:
		default:
			return false
		}
	}
	return true
}

// wireCatalog points the catalog at the new structure tree and marks the
// document as tagged.
func (e *emitter) wireCatalog(ctx context.Context, stRef ast.PdfIndirectRef) error {
	catalogRef, ok := e.doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("tagging: trailer /Root is not an indirect reference")
	}
	catalogDict, err := document.ResolveDict(ctx, e.doc, catalogRef)
	if err != nil {
		return fmt.Errorf("tagging: resolving catalog dict: %w", err)
	}

	newCatalog := make(ast.PdfDict, len(catalogDict)+2)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["StructTreeRoot"] = stRef
	newCatalog["MarkInfo"] = ast.PdfDict{"Marked": ast.PdfBool(true)}
	e.alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	return nil
}

// pageContentBytes returns a page's decoded, concatenated content. It mirrors
// the stream assembly in [content.ParsePageContent] (each decoded stream
// followed by a newline) so byte offsets the classifier derives from
// [content.ParseStreamDataWithOffsets] line up with the bytes marked here.
// Classify and Emit both call it so they agree on the page's content bytes.
func pageContentBytes(ctx context.Context, doc *document.Document, page *document.Page) ([]byte, error) {
	contentsObj, ok := page.Dict["Contents"]
	if !ok {
		return nil, nil
	}
	resolved, err := document.ResolveObject(ctx, doc, contentsObj)
	if err != nil {
		return nil, err
	}

	var streams []ast.PdfStream
	switch v := resolved.(type) {
	case ast.PdfStream:
		streams = append(streams, v)
	case ast.PdfArray:
		for _, item := range v {
			s, err := document.ResolveStream(ctx, doc, item)
			if err != nil || s.Dict == nil {
				continue
			}
			streams = append(streams, s)
		}
	default:
		return nil, fmt.Errorf("unexpected /Contents type %T", resolved)
	}

	var all []byte
	for _, s := range streams {
		data, err := doc.DecodeStream(ctx, s)
		if err != nil {
			// A page may split content across streams and leave one
			// truncated; skip it and assemble the rest, matching
			// content.ParsePageContent so the byte offsets still line up.
			doc.Logger.Debug("tagging: content stream decode failed; skipping",
				slog.Any("err", err))
			continue
		}
		all = append(all, data...)
		all = append(all, '\n')
		if len(all) > maxTaggedContentSize {
			return nil, fmt.Errorf("content exceeds %d MB limit", maxTaggedContentSize>>20)
		}
	}
	return all, nil
}
