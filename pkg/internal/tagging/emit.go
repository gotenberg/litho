package tagging

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
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

	e := &emitter{alloc: alloc, doc: doc, pages: pages, perPage: make(map[int][]leafEntry), furniture: make(map[int][]markSpan)}

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

	desc := fmt.Sprintf("synthesized structure tree: %d element(s) across %d page(s)", e.elemCount, len(e.perPage))
	if e.furnitureCount > 0 {
		desc += fmt.Sprintf(", %d running header or footer line(s) marked as pagination artifacts", e.furnitureCount)
	}
	return []string{desc}, nil
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
	// furniture holds, per page index, the pagination artifact spans of the
	// leaves that are page furniture (see [Element.Pagination]).
	furniture      map[int][]markSpan
	furnitureCount int
}

// paginate records a page-furniture leaf as a pagination artifact span of its
// page. It builds no structure element: an artifact is not part of the
// logical structure (ISO 32000-1, 14.8.2.2).
func (e *emitter) paginate(node *Element) error {
	if node.Span == nil || len(node.Kids) > 0 {
		return fmt.Errorf("tagging: pagination artifact %q must be a leaf with a span", node.Pagination)
	}
	pi := node.Span.PageIndex
	if pi < 0 || pi >= len(e.pages) {
		return fmt.Errorf("tagging: pagination artifact references page %d of %d", pi, len(e.pages))
	}
	e.furniture[pi] = append(e.furniture[pi], markSpan{
		start: node.Span.Start, end: node.Span.End, artifact: true, pagination: node.Pagination, inText: node.Span.InText,
	})
	e.furnitureCount++
	return nil
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
	if node.Span == nil && len(node.Continued) > 0 {
		return ast.PdfIndirectRef{}, fmt.Errorf("tagging: element %q continues a span it does not have", node.Role)
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
		dict["Pg"] = e.pages[pi].Ref
		// The MCIDs follow the spans in reading order, so /K lists the
		// element's sequences in the order they are read (ISO 32000-1,
		// 14.7.4.1). An integer MCID names a sequence on the /Pg page
		// (14.7.4.2), which is why every span of the leaf is on it.
		kids := make(ast.PdfArray, 0, 1+len(node.Continued))
		for _, span := range append([]ContentSpan{*node.Span}, node.Continued...) {
			if span.PageIndex != pi {
				return ast.PdfIndirectRef{}, fmt.Errorf("tagging: element %q continues from page %d onto page %d", node.Role, pi, span.PageIndex)
			}
			mcid := len(e.perPage[pi])
			kids = append(kids, ast.PdfInt(mcid))
			e.perPage[pi] = append(e.perPage[pi], leafEntry{
				span: markSpan{start: span.Start, end: span.End, mcid: mcid, tag: node.Role, inText: span.InText},
				ref:  selfRef,
			})
		}
		if len(kids) == 1 {
			dict["K"] = kids[0]
		} else {
			dict["K"] = kids
		}
	default:
		kids := make(ast.PdfArray, 0, len(node.Kids))
		for _, child := range node.Kids {
			if child.Pagination != "" {
				if err := e.paginate(child); err != nil {
					return ast.PdfIndirectRef{}, err
				}
				continue
			}
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
//
// # Corpus evidence
//
//   - litho sharedtag_ua1.pdf: a marked stream for each page of two sharing one
//     /Contents stream gave the marked form it draws a second reference, and
//     remediation at PDF/UA-1 added ISO 14289-1, 7.20 t2 on both litho and
//     veraPDF.
func (e *emitter) applyMarks(ctx context.Context) (ast.PdfArray, error) {
	var nums ast.PdfArray
	e.maxParentKey = -1
	marks := make(map[int]markedContents)

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

		structLeaves := len(e.perPage[pi])
		leaves := slices.Clone(e.perPage[pi])
		for _, f := range e.furniture[pi] {
			leaves = append(leaves, leafEntry{span: f})
		}
		sort.Slice(leaves, func(i, j int) bool { return leaves[i].span.start < leaves[j].span.start })

		// The spans stop where the last token that terminates ends, so the EMC
		// closing the trailing artifact is never written inside a string the
		// stream leaves open; the bytes past it are copied after the EMC.
		terminated := data[:content.TerminatedEnd(ctx, data)]
		var textTokens []textToken
		if slices.ContainsFunc(leaves, func(l leafEntry) bool { return l.span.inText }) {
			textTokens = textObjectTokens(ctx, terminated)
		}
		spans, elemRefs := pageSpans(terminated, leaves, textBearingXObjects(ctx, e.alloc, e.doc, e.pages[pi]), textTokens)
		if len(spans) == 0 {
			continue // no paintable content to mark
		}

		marked, err := injectMarks(data, spans)
		if err != nil {
			return nil, fmt.Errorf("tagging: page %d: %w", pi, err)
		}

		page := e.pages[pi]
		// Read the page dict alloc-first so an edit an earlier pipeline stage
		// queued at this object number (a remediation fixer injecting a
		// /DefaultRGB colour space, say) is preserved rather than overwritten.
		baseDict := stagedPageDict(e.alloc, page)
		// Pages sharing one /Contents object share its marked stream when they
		// mark it alike. Each page's MCIDs resolve through its own
		// /StructParents (ISO 32000-1, 14.7.4.4), so one stream serves both,
		// and a form the shared content draws keeps the single reference the
		// shared stream made: veraPDF parses a content stream once for the
		// document, which ISO 14289-1, 7.20 t2 counts, so a stream of its own
		// for each page gave every marked form it draws a second reference.
		contentsRef, sharable := baseDict["Contents"].(ast.PdfIndirectRef)
		var streamRef ast.PdfIndirectRef
		if prior, done := marks[contentsRef.ObjectNum]; sharable && done && bytes.Equal(prior.data, marked) {
			streamRef = prior.ref
		} else {
			streamRef = e.alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(marked))}, marked))
			if sharable && !done {
				marks[contentsRef.ObjectNum] = markedContents{data: marked, ref: streamRef}
			}
		}
		newPage := make(ast.PdfDict, len(baseDict)+2)
		maps.Copy(newPage, baseDict)
		newPage["Contents"] = streamRef
		if structLeaves > 0 {
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

// markedContents is the marked stream [emitter.applyMarks] wrote for the
// content of a /Contents object, kept so another page naming that object
// shares it when it marks the content alike.
type markedContents struct {
	data []byte
	ref  ast.PdfIndirectRef
}

// pageSpans builds the marked-content spans for one page: the structure leaves,
// plus an /Artifact span over each non-blank gap before, between, and after
// them, so no painting operator is left outside marked content. A leaf whose
// span is an artifact (a pagination artifact, see [Element.Pagination]) is
// planned as it stands and owns no structure element. elemRefs maps MCID to
// its structure element for the page's ParentTree entry.
//
// textTokens are the page's BT and ET operators, in content order, given when
// a leaf lies inside a text object (see [ContentSpan.InText]), and nil
// otherwise. ISO 32000-1, 14.6 asks each pair of BDC (or BMC) and EMC and each
// pair of BT and ET to nest, so a gap that holds an unmatched BT or ET, as the
// gaps beside such a leaf do, is not one artifact: it is artifacted piece by
// piece between them, and the BT and ET, which paint nothing, stay outside
// marked content. A gap whose text objects it holds whole stays one artifact.
//
// # Corpus evidence
//
//   - pdfjs TAMReview.pdf: keeps most of its words inside text-painting Form
//     XObjects, and sweeping them into an /Artifact dropped the document from
//     11914 extractable characters to 2266.
func pageSpans(data []byte, leaves []leafEntry, textXObjects map[string]bool, textTokens []textToken) ([]markSpan, ast.PdfArray) {
	structLeaves := 0
	for _, leaf := range leaves {
		if !leaf.span.artifact {
			structLeaves++
		}
	}
	elemRefs := make(ast.PdfArray, structLeaves)
	var spans []markSpan
	cursor := 0
	addPiece := func(start, end int) {
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
	addArtifact := func(start, end int) {
		inside := tokensWithin(textTokens, start, end)
		if nestsWhole(inside) {
			addPiece(start, end)
			return
		}
		from := start
		for _, tok := range inside {
			addPiece(from, tok.start)
			from = tok.end
		}
		addPiece(from, end)
	}
	for _, leaf := range leaves {
		s := leaf.span
		addArtifact(cursor, s.start)
		spans = append(spans, s)
		if !s.artifact && s.mcid >= 0 && s.mcid < len(elemRefs) {
			elemRefs[s.mcid] = leaf.ref
		}
		if s.end > cursor {
			cursor = s.end
		}
	}
	addArtifact(cursor, len(data))
	return spans, elemRefs
}

// textToken is one BT or ET operator of a page's content, by its byte range.
type textToken struct {
	start, end int
	// begin is true for a BT, false for an ET.
	begin bool
}

// textObjectTokens returns the BT and ET operators of content data, in content
// order, as [pageTextBlocks] reads the content, or nil when it does not parse.
func textObjectTokens(ctx context.Context, data []byte) []textToken {
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.WholeStream())
	if err != nil {
		return nil
	}
	var out []textToken
	for i, op := range ops {
		if op.Operator == "BT" || op.Operator == "ET" {
			out = append(out, textToken{start: ranges[i].Start, end: ranges[i].End, begin: op.Operator == "BT"})
		}
	}
	return out
}

// tokensWithin returns the tokens that lie in [start, end).
func tokensWithin(tokens []textToken, start, end int) []textToken {
	lo, _ := slices.BinarySearchFunc(tokens, start, func(t textToken, at int) int { return cmp.Compare(t.start, at) })
	hi := lo
	for hi < len(tokens) && tokens[hi].end <= end {
		hi++
	}
	return tokens[lo:hi]
}

// nestsWhole reports whether tokens, the BT and ET operators of one region,
// open and close whole text objects, so a sequence around the region nests
// with them (ISO 32000-1, 14.6).
func nestsWhole(tokens []textToken) bool {
	depth := 0
	for _, t := range tokens {
		if t.begin {
			depth++
			continue
		}
		depth--
		if depth < 0 {
			return false
		}
	}
	return depth == 0
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
	g := scanForms(ctx, alloc, doc, xobjs)
	names := make(map[string]bool)
	for name, node := range g.entries {
		if g.paintsText(node) {
			names[string(name)] = true
		}
	}
	return names
}

// formGraph is the graph of the Form XObjects a page's resources reach, read to
// decide which of the page's forms paint text.
//
// A form that only paints other forms carries no text-showing operator of its
// own, and a filled-in field can draw its value that way. Judging such a form
// by its own bytes marks it decoration and loses the value, so a form paints
// text when it shows text itself or reaches a form that does through the
// XObjects its resources list. The answer errs toward "paints text": a form
// wrongly kept out of an /Artifact leaves an honest ISO 14289-1, 7.1 residual,
// while one wrongly swept in hides real words from assistive technology.
//
// Each form is read once however many paths reach it: forms nest as a DAG, not
// a tree, and re-reading each subtree per path costs time exponential in the
// nesting depth. The answers come from the whole graph rather than from a
// depth-first walk, because resources can list a form that lists them back:
// ISO 32000-1, 8.10.1 forbids a form that invokes itself, but nothing stops a
// form's resources from listing a form that lists it. A walk that stops at a
// form already on its path and caches what it found below it caches a "no
// text" for a form that reaches text only through that ancestor, so which
// forms it judged decorative, and the page's /Artifact spans, would follow
// Go's map order (CONTRIBUTING "Deterministic output").
//
// A form further below the page than the document's form budget
// ([limits.Limits.MaxFormDepth]) is not read: text extraction reads the same
// budget, so a form deep enough to escape this scan carries no text the
// extractor would reach either. A page form paints text when a chain of at most
// that many forms, itself included, leads it to a form showing text. The
// shortest chain decides, not the path a form was first reached by.
//
// # Corpus evidence
//
//   - pdfjs issue15629: draws its field values through /X0, which does nothing
//     but invoke three /Annot_* forms; judging /X0 by its own bytes marked it
//     decoration and lost the name the form was filled in with.
//   - pdfjs issue6961.pdf: nests forms ten levels deep, and reading each form
//     once per path made `litho remediate -profile PDF/UA-1` on it run for over
//     50 minutes at 142% CPU, re-inflating the same streams, until the depth
//     cap finally exhausted the search. It never returned within any run of the
//     corpus harness.
//   - All corpora: one page lists forms whose resources list each other back,
//     page 1 of pdfjs issue19800.pdf.
//   - All corpora: every page names the same text-painting forms under the
//     graph as under the depth-first walk, and 1907 pages name at least one.
type formGraph struct {
	// entries maps each XObject name of the page's resources to its node.
	entries map[ast.PdfName]int
	nodes   []formNode
	// chain holds, per node, how many forms the shortest chain from it to a
	// form showing text holds, both ends included, or 0 when none reaches one.
	chain  []int
	budget int
}

// formNode is one XObject the scan reached.
type formNode struct {
	// showsText reports whether the form's own content shows text.
	showsText bool
	// children are the nodes of the XObjects the form's resources list.
	children []int
}

// scanForms reads the forms the page's XObjects xobjs reach, breadth first so
// each form is read at its shortest distance from the page, and follows a
// form's children only while that distance leaves them within the form budget.
// The page's XObjects sit at distance 1, and the budget is at least 1
// ([limits.Limits.WithDefaults]). The forms and their resources are read
// alloc-first (see [stagedValue]).
//
// A scan that ctx stops leaves the forms it did not read showing no text, and
// the caller's next check of ctx ends the tagging run.
func scanForms(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, xobjs ast.PdfDict) *formGraph {
	g := &formGraph{entries: make(map[ast.PdfName]int, len(xobjs)), budget: doc.Limits().MaxFormDepth}
	type pending struct {
		node  int
		entry ast.PdfObject
		// level counts the forms from the page down to this one, itself
		// included, along the shortest chain.
		level int
	}
	var queue []pending
	byObjNum := make(map[int]int)
	add := func(entry ast.PdfObject, level int) int {
		ref, isRef := entry.(ast.PdfIndirectRef)
		if isRef && ref.ObjectNum > 0 {
			if node, seen := byObjNum[ref.ObjectNum]; seen {
				return node
			}
			byObjNum[ref.ObjectNum] = len(g.nodes)
		}
		g.nodes = append(g.nodes, formNode{})
		queue = append(queue, pending{node: len(g.nodes) - 1, entry: entry, level: level})
		return len(g.nodes) - 1
	}
	for _, name := range slices.Sorted(maps.Keys(xobjs)) {
		g.entries[name] = add(xobjs[name], 1)
	}
	for head := 0; head < len(queue); head++ {
		if ctx.Err() != nil {
			break
		}
		p := queue[head]
		shows, inner := readForm(ctx, alloc, doc, p.entry)
		g.nodes[p.node].showsText = shows
		if shows || p.level == g.budget {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(inner)) {
			child := add(inner[name], p.level+1)
			g.nodes[p.node].children = append(g.nodes[p.node].children, child)
		}
	}
	g.chain = shortestTextChains(g.nodes)
	return g
}

// shortestTextChains returns, per node, how many forms the shortest chain from
// it to a form showing text holds, both ends included, or 0 when no chain
// reaches one. It walks the edges backwards from the forms showing text, so the
// answer is the graph's and not the order the edges are met in.
func shortestTextChains(nodes []formNode) []int {
	parents := make([][]int, len(nodes))
	for i, n := range nodes {
		for _, c := range n.children {
			parents[c] = append(parents[c], i)
		}
	}
	chain := make([]int, len(nodes))
	var frontier []int
	for i, n := range nodes {
		if n.showsText {
			chain[i] = 1
			frontier = append(frontier, i)
		}
	}
	for len(frontier) > 0 {
		var next []int
		for _, c := range frontier {
			for _, p := range parents[c] {
				if chain[p] == 0 {
					chain[p] = chain[c] + 1
					next = append(next, p)
				}
			}
		}
		frontier = next
	}
	return chain
}

// paintsText reports whether the form at node reaches a form showing text, its
// own content included, through a chain of forms the budget lets text
// extraction read.
func (g *formGraph) paintsText(node int) bool {
	return g.chain[node] > 0 && g.chain[node] <= g.budget
}

// readForm reports whether the Form XObject entry shows text of its own and,
// when it does not, returns the XObjects its resources list, which the scan
// follows next. Anything that is not a readable Form XObject, an image among
// them, shows no text and lists nothing.
func readForm(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, entry ast.PdfObject) (shows bool, inner ast.PdfDict) {
	stream, err := stagedStream(ctx, alloc, doc, entry)
	if err != nil || stream.Dict == nil {
		return false, nil
	}
	// An unreadable /Subtype names no form, so its error needs no other
	// handling: the entry shows no text either way.
	if subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); subtype != "Form" {
		return false, nil
	}
	body, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("decoding form xobject for text scan", slog.Any("err", err))
		return false, nil
	}
	if showsText(body) {
		return true, nil
	}
	nested, err := stagedDict(ctx, alloc, doc, stream.Dict["Resources"])
	if err != nil || nested == nil {
		return false, nil
	}
	inner, err = stagedDict(ctx, alloc, doc, nested["XObject"])
	if err != nil {
		return false, nil
	}
	return false, inner
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
