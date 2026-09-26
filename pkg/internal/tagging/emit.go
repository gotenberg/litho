package tagging

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// maxTagDepth bounds structure-tree recursion. It is an anti-DoS guard, not
// spec-mandated: real logical trees are shallow (Document > section > H/P,
// rarely past a handful of levels), so 64 is far above any genuine document
// while still capping a pathological or hostile classifier input.
const maxTagDepth = 64

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
// being untagged ([AutoTag] does this). It reads the catalog, the page dicts,
// their content and their resources alloc-first (see [stagedValue]), as
// [Classify] does, so root's spans index the content it marks, and an edit an
// earlier stage queued to any of them (a remediation fixer's catalog patch or
// content repair, a stamp) is built on rather than overwritten. Emit mutates
// alloc and is not safe for concurrent use.
func Emit(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, root *Element) ([]string, error) {
	if root == nil || root.Role != RoleDocument {
		return nil, fmt.Errorf("tagging: root element must have role Document")
	}

	pages, err := stagedPages(ctx, alloc, doc)
	if err != nil {
		return nil, err
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
	// Per ISO 32000-1, 14.7.4.4, /ParentTreeNextKey is one greater than the
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
	// /Lang, /Alt, /ActualText and /T are text strings (ISO 32000-1, Table 323
	// and 14.9.3), so they take PDFDocEncoding or UTF-16BE with a byte order
	// mark. Casting the Go string put raw UTF-8 in the file, which a reader
	// decodes one byte per character.
	if node.Lang != "" {
		dict["Lang"] = document.EncodePdfString(node.Lang)
	}
	if node.Alt != "" {
		dict["Alt"] = document.EncodePdfString(node.Alt)
	}
	if node.ActualText != "" {
		dict["ActualText"] = document.EncodePdfString(node.ActualText)
	}
	if node.Title != "" {
		dict["T"] = document.EncodePdfString(node.Title)
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
// or artifact" rule (ISO 14289-1, 7.1). It returns the StructTreeRoot
// /ParentTree /Nums array mapping each tagged page's StructParents key to its
// structure elements in MCID order.
func (e *emitter) applyMarks(ctx context.Context) (ast.PdfArray, error) {
	var nums ast.PdfArray
	e.maxParentKey = -1

	for pi := 0; pi < len(e.pages); pi++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := pageContentBytes(ctx, e.alloc, e.doc, e.pages[pi])
		if err != nil {
			return nil, fmt.Errorf("tagging: page %d content: %w", pi, err)
		}
		if len(data) == 0 {
			continue
		}

		leaves := e.perPage[pi]
		sort.Slice(leaves, func(i, j int) bool { return leaves[i].span.start < leaves[j].span.start })

		spans, elemRefs := pageSpans(data, leaves, textBearingXObjects(ctx, e.alloc, e.doc, e.pages[pi]))
		if len(spans) == 0 {
			continue // no paintable content to mark
		}

		marked, err := injectMarks(data, spans)
		if err != nil {
			return nil, fmt.Errorf("tagging: page %d: %w", pi, err)
		}

		streamRef := e.alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(marked))}, marked))
		page := e.pages[pi]
		// Read the page dict alloc-first so an edit an earlier pipeline stage
		// queued at this object number (a remediation fixer injecting a
		// /DefaultRGB colour space, say) is preserved rather than overwritten.
		baseDict := stagedPageDict(e.alloc, page)
		newPage := make(ast.PdfDict, len(baseDict)+2)
		maps.Copy(newPage, baseDict)
		newPage["Contents"] = streamRef
		if len(leaves) > 0 {
			newPage["StructParents"] = ast.PdfInt(pi)
			nums = append(nums, ast.PdfInt(pi), elemRefs)
			if pi > e.maxParentKey {
				e.maxParentKey = pi
			}
		}
		// A page inlined in a /Kids array has no object number to replace, so
		// its marks cannot be written back. Tag the rest of the document rather
		// than fail it; writing at object 0 produced a body every reader
		// ignores (see writer.ObjAllocator.AllocAt).
		if !e.alloc.AllocAt(page.Ref.ObjectNum, newPage) {
			e.doc.Logger.Debug("skipping tag marks on a page with no indirect reference",
				slog.Int("page", pi+1),
			)
		}
	}
	return nums, nil
}

// pageSpans builds the marked-content spans for one page: the structure leaves,
// plus an /Artifact span over each non-blank gap before, between, and after
// them, so no painting operator is left outside marked content. elemRefs maps
// MCID to its structure element for the page's ParentTree entry.
//
// # Corpus evidence
//
//   - pdfjs TAMReview.pdf: keeps most of its words inside text-painting Form
//     XObjects, and sweeping them into an /Artifact dropped the document from
//     11914 extractable characters to 2266.
func pageSpans(data []byte, leaves []leafEntry, textXObjects map[string]bool) ([]markSpan, ast.PdfArray) {
	elemRefs := make(ast.PdfArray, len(leaves))
	var spans []markSpan
	cursor := 0
	addArtifact := func(start, end int) {
		if start >= end || isBlank(data[start:end]) {
			return
		}
		// An /Artifact is content with no meaning for a reader
		// (ISO 32000-1, 14.8.2.2), so marking one over a Form XObject that
		// paints TEXT hides that text from assistive technology entirely, and
		// real documents keep most of their words inside such XObjects.
		// Leaving the region unmarked keeps an honest ISO 14289-1, 7.1
		// residual instead of asserting the words are decoration.
		if paintsTextXObject(data[start:end], textXObjects) {
			return
		}
		spans = append(spans, markSpan{start: start, end: end, artifact: true})
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

// textBearingXObjects returns the page's Form XObject resource names whose
// content paints text. Their invocations must not be swept into an /Artifact.
// It reads the resources alloc stages (see [stagedResources]).
func textBearingXObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) map[string]bool {
	if page == nil {
		return nil
	}
	resources := stagedResources(ctx, alloc, doc, page)
	if resources == nil {
		return nil
	}
	xobjs, err := stagedDict(ctx, alloc, doc, resources["XObject"])
	if err != nil || xobjs == nil {
		return nil
	}
	names := make(map[string]bool)
	// One memo for the whole page: sibling resources routinely invoke the same
	// forms, and re-deciding a shared subtree per name is the same waste the
	// memo exists to stop inside a single scan.
	answers := make(map[int]bool)
	for name, entry := range xobjs {
		if paints, _ := formPaintsText(ctx, alloc, doc, entry, make(map[int]bool), answers, 0); paints {
			names[string(name)] = true
		}
	}
	return names
}

// formPaintsText reports whether a Form XObject shows text itself or reaches
// text through the forms it invokes.
//
// A form that only paints other forms carries no text-showing operator of its
// own, and a filled-in field can draw its value that way. Judging such a form
// by its own bytes marks it decoration and loses the value. The answer errs
// toward "paints text": a form wrongly kept out of an /Artifact leaves an
// honest ISO 14289-1, 7.1 residual, while one wrongly swept in hides real words
// from assistive technology.
//
// visited holds the forms on the current path and breaks the cycles ISO 32000-1,
// 8.10.1 forbids but files still contain. answers memoises the decision per
// object so a form reached by several paths is decoded once: forms nest as a
// DAG, not a tree, and re-deciding each subtree per path costs time exponential
// in the nesting depth.
//
// depth is how many forms sit between the page and entry, so entry itself is
// depth+1 forms below the page. A form deeper than the document's form budget
// (limits.Limits.MaxFormDepth) is not read: text extraction reads the same
// budget, so a form deep enough to escape this scan carries no text the
// extractor would reach either.
//
// complete reports whether the answer was reached without the depth cap cutting
// the search short. Only a complete answer is memoised: a false produced by
// running out of depth says nothing about the same form reached higher up,
// where the remaining budget would let the search continue.
//
// The forms and their resources are read alloc-first (see [stagedValue]).
//
// # Corpus evidence
//
//   - pdfjs issue15629: draws its field values through /X0, which does nothing
//     but invoke three /Annot_* forms; judging /X0 by its own bytes marked it
//     decoration and lost the name the form was filled in with.
//   - pdfjs issue6961.pdf: nests forms ten levels deep, and without the memo
//     `litho remediate -profile PDF/UA-1` on it ran for over 50 minutes at
//     142% CPU, re-inflating the same streams, until the depth cap finally
//     exhausted the search. It never returned within any run of the corpus
//     harness.
func formPaintsText(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, entry ast.PdfObject, visited map[int]bool, answers map[int]bool, depth int) (paints, complete bool) {
	if depth+1 > doc.Limits().MaxFormDepth {
		return false, false
	}
	objNum := 0
	if ref, ok := entry.(ast.PdfIndirectRef); ok {
		objNum = ref.ObjectNum
		if known, ok := answers[objNum]; ok {
			return known, true
		}
		if visited[objNum] {
			// A cycle contributes no text of its own, and the path that
			// opened it still owns the answer, so this is not memoised.
			return false, true
		}
		visited[objNum] = true
		defer delete(visited, objNum)
	}
	memoise := func(paints, complete bool) (bool, bool) {
		if objNum > 0 && complete {
			answers[objNum] = paints
		}
		return paints, complete
	}

	stream, err := stagedStream(ctx, alloc, doc, entry)
	if err != nil || stream.Dict == nil {
		return memoise(false, true)
	}
	if subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); subtype != "Form" {
		return memoise(false, true)
	}
	body, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("decoding form xobject for text scan", slog.Any("err", err))
		return memoise(false, true)
	}
	if showsText(body) {
		return memoise(true, true)
	}
	nested, err := stagedDict(ctx, alloc, doc, stream.Dict["Resources"])
	if err != nil || nested == nil {
		return memoise(false, true)
	}
	inner, err := stagedDict(ctx, alloc, doc, nested["XObject"])
	if err != nil || inner == nil {
		return memoise(false, true)
	}

	childrenComplete := true
	for _, child := range inner {
		paints, complete := formPaintsText(ctx, alloc, doc, child, visited, answers, depth+1)
		if paints {
			return memoise(true, true)
		}
		if !complete {
			childrenComplete = false
		}
	}
	return memoise(false, childrenComplete)
}

// showsText reports whether a content stream carries a text-showing operator
// (ISO 32000-1, Table 109).
func showsText(data []byte) bool {
	for _, op := range [][]byte{[]byte("Tj"), []byte("TJ"), []byte("'"), []byte("\"")} {
		if bytes.Contains(data, op) {
			return true
		}
	}
	return false
}

// paintsTextXObject reports whether a content region invokes one of the named
// text-bearing Form XObjects with Do.
func paintsTextXObject(region []byte, textXObjects map[string]bool) bool {
	if len(textXObjects) == 0 {
		return false
	}
	for name := range textXObjects {
		if bytes.Contains(region, append([]byte("/"), name...)) && bytes.Contains(region, []byte("Do")) {
			return true
		}
	}
	return false
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
// document as tagged. It reads the catalog alloc-first so a queued edit from an
// earlier pipeline stage (a remediation fixer's catalog patch, an /Metadata
// addition) is preserved rather than overwritten.
func (e *emitter) wireCatalog(ctx context.Context, stRef ast.PdfIndirectRef) error {
	catalogRef, ok := catalogRefForTagging(ctx, e.doc)
	if !ok {
		return fmt.Errorf("tagging: no catalog to wire the structure tree into")
	}
	catalogDict, err := stagedDict(ctx, e.alloc, e.doc, catalogRef)
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

// pageContentBytes returns a page's decoded, concatenated content as alloc
// stages it: the page dict, its /Contents and each content stream are read
// alloc-first (see [stagedValue]), so the content marked is the content the
// output holds, a repair an earlier stage queued included. It mirrors the
// stream assembly in [content.ParsePageContent] (each decoded stream followed
// by a newline) so byte offsets the classifier derives from
// [content.ParseStreamDataWithOffsets] line up with the bytes marked here.
// Classify and Emit both call it with the same alloc so they agree on the
// page's content bytes: between the two, Emit queues only new objects and page
// dicts, and it reads each page's content before rewriting that page.
//
// The content is bounded by [limits.Limits.MaxPageContentBytes] of the
// document's budgets, an anti-DoS guard, not spec-mandated. It reads the same
// bound as [content.ParsePageContent] rather than a value of its own. A bound
// of its own was written to match, then drifted when that limit rose from 32 MB
// to 64 MB, leaving tagging to refuse pages the rest of the pipeline reads. The
// gap was reachable rather than theoretical: real pages come close to the stale
// bound.
//
// # Corpus evidence
//
//   - pdfjs issue21089.pdf: page 1 decodes to 27.51 MB, within 16% of the
//     stale 32 MB bound.
func pageContentBytes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) ([]byte, error) {
	contentsObj, ok := stagedPageDict(alloc, page)["Contents"]
	if !ok {
		return nil, nil
	}
	resolved, err := stagedObject(ctx, alloc, doc, contentsObj)
	if err != nil {
		return nil, err
	}

	var streams []ast.PdfStream
	switch v := resolved.(type) {
	case ast.PdfStream:
		streams = append(streams, v)
	case ast.PdfArray:
		for _, item := range v {
			s, err := stagedStream(ctx, alloc, doc, item)
			if err != nil || s.Dict == nil {
				continue
			}
			streams = append(streams, s)
		}
	default:
		return nil, fmt.Errorf("unexpected /Contents type %T", resolved)
	}

	limit := doc.Limits().MaxPageContentBytes()
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
		if int64(len(all)) > limit {
			return nil, fmt.Errorf("%w of %d bytes", limits.ErrPageContentTooLarge, limit)
		}
	}
	return all, nil
}
