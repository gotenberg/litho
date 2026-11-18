package markdown

import (
	"context"
	"slices"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/tagging"
)

// node is an element of the structure the writer reads: a structure element,
// with its standard role, or, with no role, a piece of content holding
// glyphs.
type node struct {
	role   string
	kids   []*node
	glyphs []glyphRef
	// alt is the element's /Alt text.
	alt string
	// numbering is the /ListNumbering attribute of a list (ISO 32000-1,
	// 14.8.5.5, Table 347), "" when it states none.
	numbering string
}

// glyphRef names a glyph: an index into the pages the writer reads, and one
// into that page's [typography.ShownText.Elements].
type glyphRef struct {
	page, index int
}

// roleArtifact marks page furniture [tagging.Classify] sets apart, a running
// header, footer or page number.
const roleArtifact = "Artifact"

// maxRoleMapChain caps how many /RoleMap entries a role is followed through.
// Anti-DoS guard, not spec-mandated: ISO 32000-1, 14.7.3 maps a role to a
// standard one, directly or through other roles, and a cycle maps it to none.
// Real role maps chain one or two names deep.
const maxRoleMapChain = 16

// structTree builds the nodes of the document's structure tree, nil when it
// has none. pageIndex maps a page reference to its index in the pages the
// writer reads; content on any other page is left out.
func structTree(ctx context.Context, doc *document.Document, pageIndex map[ast.PdfIndirectRef]int, glyphs *glyphIndex) (*node, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil || catalog == nil {
		return nil, nil //nolint:nilerr // An unresolvable catalog is null (ISO 32000-1, 7.3.10): no structure tree.
	}
	root, err := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
	if err != nil || root == nil {
		return nil, nil //nolint:nilerr // An unresolvable /StructTreeRoot is null (ISO 32000-1, 7.3.10): no structure tree.
	}
	w := structWalker{
		doc:       doc,
		pageIndex: pageIndex,
		glyphs:    glyphs,
		roleMap:   roleMap(ctx, doc, root["RoleMap"]),
		visited:   make(map[int]bool),
	}
	top := &node{role: "Document"}
	w.kids(ctx, top, root["K"], -1, 0)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return top, nil
}

// structWalker reads a structure tree into nodes.
type structWalker struct {
	doc       *document.Document
	pageIndex map[ast.PdfIndirectRef]int
	glyphs    *glyphIndex
	roleMap   map[string]string
	// visited holds the object numbers of the elements read: the structure
	// tree is a tree (ISO 32000-1, 14.7.2), so one reached twice is
	// malformed, and reading it again could recurse without end.
	visited map[int]bool
}

// kids appends to parent the nodes the /K value k names. page is the index
// of the page the enclosing element's /Pg names, -1 when none does.
func (w *structWalker) kids(ctx context.Context, parent *node, k ast.PdfObject, page, depth int) {
	if depth > compliance.MaxStructTreeWalkDepth || ctx.Err() != nil {
		return
	}
	if ref, ok := k.(ast.PdfIndirectRef); ok {
		if w.visited[ref.ObjectNum] {
			return
		}
		w.visited[ref.ObjectNum] = true
	}
	resolved, err := document.ResolveObject(ctx, w.doc, k)
	if err != nil {
		return
	}
	switch v := resolved.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			w.kids(ctx, parent, kid, page, depth)
		}
	case ast.PdfInt:
		w.content(parent, page, intconv.Clamp(int64(v)))
	case ast.PdfDict:
		if _, ok := v["S"]; ok {
			parent.kids = append(parent.kids, w.element(ctx, v, page, depth+1))
			return
		}
		// A marked-content reference (ISO 32000-1, 14.7.4.3, Table 324).
		// One with /Stm marks content inside a form XObject or an
		// annotation's appearance, whose glyphs carry no MCID of the
		// page's own sequences, and an object reference marks no text.
		mcid, ok := v["MCID"].(ast.PdfInt)
		if !ok {
			return
		}
		if _, inStream := v["Stm"]; inStream {
			return
		}
		w.content(parent, w.pageOf(v, page), intconv.Clamp(int64(mcid)))
	}
}

// element reads the structure element dict.
func (w *structWalker) element(ctx context.Context, dict ast.PdfDict, page, depth int) *node {
	n := &node{role: w.standardRole(dict["S"])}
	if alt, err := document.ResolveString(ctx, w.doc, dict["Alt"]); err == nil {
		n.alt = document.DecodePdfString(alt)
	}
	n.numbering = listNumbering(ctx, w.doc, dict["A"])
	w.kids(ctx, n, dict["K"], w.pageOf(dict, page), depth)
	return n
}

// content appends to parent the glyphs the marked-content sequence mcid holds
// on page.
func (w *structWalker) content(parent *node, page, mcid int) {
	if page < 0 {
		return
	}
	if refs := w.glyphs.mcid(page, mcid); len(refs) > 0 {
		parent.kids = append(parent.kids, &node{glyphs: refs})
	}
}

// pageOf returns the index of the page dict's /Pg names, or page when it
// names none the writer reads (ISO 32000-1, 14.7.2, Table 323: /Pg passes to
// the element's kids).
func (w *structWalker) pageOf(dict ast.PdfDict, page int) int {
	if ref, ok := dict["Pg"].(ast.PdfIndirectRef); ok {
		if i, ok := w.pageIndex[ref]; ok {
			return i
		}
		return -1
	}
	return page
}

// standardRole returns the standard structure type the /S value s maps to
// through the role map (ISO 32000-1, 14.8.4), s itself when it maps to none.
func (w *structWalker) standardRole(s ast.PdfObject) string {
	name, _ := s.(ast.PdfName)
	role := string(name)
	for range maxRoleMapChain {
		mapped, ok := w.roleMap[role]
		if !ok || mapped == role {
			break
		}
		role = mapped
	}
	return role
}

// roleMap reads a /RoleMap dictionary.
func roleMap(ctx context.Context, doc *document.Document, obj ast.PdfObject) map[string]string {
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return nil
	}
	out := make(map[string]string, len(d))
	for k, v := range d {
		if name, ok := v.(ast.PdfName); ok {
			out[string(k)] = string(name)
		}
	}
	return out
}

// listNumbering returns the /ListNumbering of the List attributes the /A
// value a holds: a dictionary, or an array of them, each possibly followed by
// a revision number (ISO 32000-1, 14.7.5.2).
func listNumbering(ctx context.Context, doc *document.Document, a ast.PdfObject) string {
	resolved, err := document.ResolveObject(ctx, doc, a)
	if err != nil {
		return ""
	}
	attrs := []ast.PdfObject{resolved}
	if arr, ok := resolved.(ast.PdfArray); ok {
		attrs = arr
	}
	for _, attr := range attrs {
		d, err := document.ResolveDict(ctx, doc, attr)
		if err != nil || d == nil {
			continue
		}
		if o, _ := d["O"].(ast.PdfName); o != "List" {
			continue
		}
		if numbering, ok := d["ListNumbering"].(ast.PdfName); ok {
			return string(numbering)
		}
	}
	return ""
}

// classifiedTree converts the logical structure [tagging.Classify] derives
// from the layout into nodes. docPage maps a 0-based page of the document to
// its index in the pages the writer reads, -1 when the writer does not read
// it.
func classifiedTree(root *tagging.Element, docPage func(int) int, glyphs *glyphIndex) *node {
	var convert func(e *tagging.Element) *node
	convert = func(e *tagging.Element) *node {
		// The /Alt of a figure the layout finds is a placeholder naming the
		// image resource, not text describing it: it is left out.
		n := &node{role: string(e.Role)}
		if e.Pagination != "" {
			n.role = roleArtifact
		}
		if e.Span != nil {
			page := docPage(e.Span.PageIndex)
			for _, span := range append([]tagging.ContentSpan{*e.Span}, e.Continued...) {
				if refs := glyphs.span(page, span.Start, span.End); len(refs) > 0 {
					n.kids = append(n.kids, &node{glyphs: refs})
				}
			}
		}
		for _, kid := range e.Kids {
			n.kids = append(n.kids, convert(kid))
		}
		return n
	}
	return convert(root)
}

// glyphIndex finds the glyphs of a page by marked-content identifier or by
// the content bytes that drew them.
type glyphIndex struct {
	byMCID []map[int][]int
	// byOffset holds, per page, the glyphs that sit at a byte offset of
	// the content, sorted by that offset and then by index.
	byOffset [][]offsetGlyph
}

type offsetGlyph struct {
	offset, index int
}

func newGlyphIndex(pages []Page) *glyphIndex {
	g := &glyphIndex{
		byMCID:   make([]map[int][]int, len(pages)),
		byOffset: make([][]offsetGlyph, len(pages)),
	}
	for p, page := range pages {
		mcids := make(map[int][]int)
		var offsets []offsetGlyph
		for i, el := range page.Shown.Elements {
			if el.MCID >= 0 {
				mcids[el.MCID] = append(mcids[el.MCID], i)
			}
			if op := page.Shown.PageOps[i]; op >= 0 && op < len(page.Ranges) {
				offsets = append(offsets, offsetGlyph{page.Ranges[op].Start, i})
			}
		}
		sort.SliceStable(offsets, func(a, b int) bool { return offsets[a].offset < offsets[b].offset })
		g.byMCID[p], g.byOffset[p] = mcids, offsets
	}
	return g
}

// mcid returns the glyphs of the marked-content sequence mcid on page.
func (g *glyphIndex) mcid(page, mcid int) []glyphRef {
	return refsOn(page, g.byMCID[page][mcid])
}

// span returns the glyphs drawn by the operations that start within [start,
// end) of page's content, in content order. page is -1 for a page the writer
// does not read.
func (g *glyphIndex) span(page, start, end int) []glyphRef {
	if page < 0 {
		return nil
	}
	offsets := g.byOffset[page]
	lo := sort.Search(len(offsets), func(i int) bool { return offsets[i].offset >= start })
	var indices []int
	for _, o := range offsets[lo:] {
		if o.offset >= end {
			break
		}
		indices = append(indices, o.index)
	}
	slices.Sort(indices)
	return refsOn(page, indices)
}

func refsOn(page int, indices []int) []glyphRef {
	refs := make([]glyphRef, len(indices))
	for i, k := range indices {
		refs[i] = glyphRef{page, k}
	}
	return refs
}
