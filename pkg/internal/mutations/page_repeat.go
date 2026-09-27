package mutations

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// shareWalkCheckInterval is how many objects [shareRepeatedPages] walks
// between two checks of its context.
const shareWalkCheckInterval = 256

// pageRanks groups the indices of pages, a selection in output order that may
// name a page more than once, by the time each names its page: the first
// group holds every page's first naming, the second every second naming, and
// so on. Each group names a page once, in output order.
//
// A page object stands at one place in one page tree: it has one /Parent (ISO
// 32000-1, 7.7.3.3, Table 30), and the tree lists each page once (7.7.3.2). So
// each group after the first is copied as pages of their own (see
// [shareRepeatedPages]).
func pageRanks(pages []*document.Page) [][]int {
	seen := make(map[*document.Page]int, len(pages))
	var ranks [][]int
	for i, page := range pages {
		rank := seen[page]
		seen[page] = rank + 1
		if rank == len(ranks) {
			ranks = append(ranks, nil)
		}
		ranks[rank] = append(ranks[rank], i)
	}
	return ranks
}

// pagesAt returns the pages at indices, in order.
func pagesAt(pages []*document.Page, indices []int) []*document.Page {
	out := make([]*document.Page, len(indices))
	for i, index := range indices {
		out[i] = pages[index]
	}
	return out
}

// pageCopyPlan returns the copier overrides under which a copy of pages, some
// of doc's allPages each named once, carries those pages and nothing past
// them, and the structure it carries, nil unless marking is bakeTagged (see
// [newStructSource]).
//
// outside holds the object numbers of the pages of doc the output leaves out,
// whose destinations are cut (see [splitDestOverrides]). A form field keeps
// only the widgets of pages (see [fieldKidOverrides]), so the copy of a field
// that a round of pages named again carries lists the widgets of that round,
// and no widget another copy of its page holds. The signed signatures of
// pages are drawn into them (see [bakeSignedSignatures]).
func pageCopyPlan(ctx context.Context, doc *document.Document, allPages, pages []*document.Page, outside map[int]bool, marking bakeMarking) (map[int]ast.PdfObject, *structSource, error) {
	overrides := pageCopyOverrides(pages)
	if len(outside) > 0 {
		splitDestOverrides(ctx, doc, pages, outside, overrides)
	}
	if elsewhere := unselectedPages(allPages, pages); len(elsewhere) > 0 {
		if srcCatalog, err := document.ResolveCatalog(ctx, doc); err == nil && srcCatalog != nil {
			dropped := droppedPageAnnots(ctx, nil, doc, allPages, elsewhere)
			// The truncation flag is dropped here on purpose. A copy goes
			// outward from the pages it takes, so a page it leaves is not
			// reachable from what it copies however deep the field tree
			// runs; measured across chain depths well past the bound, split
			// and insert never carried the excluded page. Only delete, which
			// rewrites whatever the catalog reaches, has to refuse.
			fieldOverrides, _ := fieldKidOverrides(ctx, nil, doc, srcCatalog.Dict, dropped)
			maps.Copy(overrides, fieldOverrides)
		}
	}
	overrides, err := bakeSignedSignatures(ctx, doc, pages, overrides, marking)
	if err != nil {
		return nil, nil, err
	}
	if marking != bakeTagged {
		return overrides, nil, nil
	}
	structs, overrides, err := newStructSource(ctx, doc, overrides, pages)
	if err != nil {
		return nil, nil, err
	}
	return overrides, structs, nil
}

// shareNode is one object [shareRepeatedPages] walks.
type shareNode struct {
	// parents are the objects whose values name this one.
	parents []int
	// owned is set when the object, or one it names, belongs to one place
	// (see [ownedByOnePlace]), or reads otherwise in the two copies.
	owned bool
	// shareable is set when the object is the source's own, resolved alike
	// by both copiers.
	shareable bool
}

// shareRepeatedPages prepares copier, which copies pages of doc that a copy
// names again, to write each as a page object of its own. first is the copier
// that wrote their first copies, and firstCopies maps the object number of
// each page the output holds to its first copy.
//
// The page object has to be new: a page has one /Parent and one place in the
// page tree (ISO 32000-1, 7.7.3.3, Table 30), so a second reference to the
// first copy names the same page twice, and a reader holds it once. Every
// object that belongs to one place is new with it (see [ownedByOnePlace]):
// its annotations, as an annotation's /P names one page (Table 164), the
// fields their widgets present, as a widget is one kid of one field (12.5.6.19,
// Table 220), and the structure elements and structure keys describing it
// (14.7.4.4). Everything else the page reaches is shared with the first copy:
// its content streams, resources, fonts and images, and the appearance
// streams of its annotations. None of these names a page or a place, and an
// edit of a page's content writes new streams rather than changing shared
// ones.
//
// References to the other pages the output holds go to their first copies,
// so a link to another page lands on a page of the output rather than
// copying that page again.
//
// Each object is walked once, so the walk is bounded by the objects the file
// defines.
//
// # Corpus evidence
//
//   - All corpora: a split naming page 1 again ([1 2 1], [1 1] for a one-page
//     file) held fewer pages than it named on all 5211 files, and an insertion
//     of page 1 twice on all 5210, as litho and qpdf count them. Each holds
//     every page it names now. qpdf warns on the same 41 and 62 outputs, and
//     every warning added is one the source's page carries, now on the page
//     added too: a content stream that does not decode, an /Annots array
//     listing one annotation twice.
//   - pdfjs issue12963.pdf: its widgets name page tree nodes, fonts and images
//     as /Parent, so the widgets of the added page are not reachable from
//     /AcroForm, as those of the first copy are not.
//   - litho: veraPDF fails no rule at a fixture's home profile on the split or
//     the insertion that it does not fail on the fixture, the 17 PDF/UA-1 and
//     14 PDF/UA-2 fixtures included.
//   - pdfjs issue19319.pdf: page 1 named 50 times makes a 10.0 MB file, where
//     page 1 once makes a 9.96 MB one.
func shareRepeatedPages(ctx context.Context, doc *document.Document, first, copier *writer.Copier, pages []*document.Page, firstCopies map[int]ast.PdfIndirectRef) error {
	own := make(map[int]bool, len(pages))
	for _, page := range pages {
		if page.Ref.ObjectNum > 0 {
			own[page.Ref.ObjectNum] = true
		}
	}
	for _, num := range slices.Sorted(maps.Keys(firstCopies)) {
		if !own[num] {
			copier.MapTo(num, firstCopies[num])
		}
	}

	nodes := make(map[int]*shareNode)
	var queue []int
	visit := func(num, parent int) {
		node, seen := nodes[num]
		if !seen {
			node = &shareNode{}
			nodes[num] = node
			queue = append(queue, num)
		}
		if parent > 0 {
			node.parents = append(node.parents, parent)
		}
	}
	for _, page := range pages {
		source := page.Dict
		if override, ok := copier.Override(page.Ref.ObjectNum); ok && page.Ref.ObjectNum != 0 {
			if dict, isDict := override.(ast.PdfDict); isDict {
				source = dict
			}
		}
		// A page copy writes the resources the page inherits onto it (see
		// [copyPageForOutput]), so they are walked as the page's own.
		roots := []ast.PdfObject{source, pageResourcesEntry(source, page)}
		if page.Ref.ObjectNum > 0 {
			visit(page.Ref.ObjectNum, 0)
			nodes[page.Ref.ObjectNum].owned = true
			roots = roots[1:]
		}
		for _, root := range roots {
			for _, ref := range referencesIn(root) {
				visit(ref.ObjectNum, page.Ref.ObjectNum)
			}
		}
	}

	for walked := 0; len(queue) > 0; walked++ {
		if walked%shareWalkCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("sharing the objects of a repeated page: %w", err)
			}
		}
		num := queue[0]
		queue = queue[1:]
		node := nodes[num]
		if copier.StandsIn(num) {
			// Another page of the output, already its first copy.
			continue
		}
		_, firstOverride := first.Override(num)
		value, override := copier.Override(num)
		if firstOverride || override {
			// The two copies read different objects here, or may, so the
			// first copy's is not this one's.
			node.owned = true
		}
		if !override {
			resolved, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: num})
			if err != nil {
				if ctx.Err() != nil {
					return fmt.Errorf("sharing the objects of a repeated page: %w", err)
				}
				continue
			}
			value = resolved
			node.shareable = !firstOverride
		}
		dict := objectDict(value)
		if ownedByOnePlace(dict) {
			node.owned = true
		}
		if isPageTreeObject(dict) && !own[num] {
			// A page the output leaves out, or a page tree node: the copy
			// does not share it, and nothing past it is the page's.
			node.owned = true
			continue
		}
		for _, ref := range referencesIn(value) {
			visit(ref.ObjectNum, num)
		}
	}

	var owned []int
	for _, num := range slices.Sorted(maps.Keys(nodes)) {
		if nodes[num].owned {
			owned = append(owned, num)
		}
	}
	for len(owned) > 0 {
		num := owned[0]
		owned = owned[1:]
		for _, parent := range nodes[num].parents {
			if !nodes[parent].owned {
				nodes[parent].owned = true
				owned = append(owned, parent)
			}
		}
	}

	mapping := first.Mapping()
	for _, num := range slices.Sorted(maps.Keys(nodes)) {
		node := nodes[num]
		if node.owned || !node.shareable || copier.StandsIn(num) || first.StandsIn(num) {
			continue
		}
		if dst, copied := mapping[num]; copied {
			copier.MapTo(num, ast.PdfIndirectRef{ObjectNum: dst})
		}
	}
	return nil
}

// objectDict returns the dictionary of obj, a dictionary's own or a stream's,
// nil for anything else.
func objectDict(obj ast.PdfObject) ast.PdfDict {
	switch v := obj.(type) {
	case ast.PdfDict:
		return v
	case ast.PdfStream:
		return v.Dict
	}
	return nil
}

// referencesIn returns the indirect references obj holds, in its direct
// arrays and dictionaries, a stream's dictionary included.
func referencesIn(obj ast.PdfObject) []ast.PdfIndirectRef {
	var refs []ast.PdfIndirectRef
	stack := []ast.PdfObject{obj}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch v := top.(type) {
		case ast.PdfIndirectRef:
			if v.ObjectNum > 0 {
				refs = append(refs, v)
			}
		case ast.PdfArray:
			stack = append(stack, v...)
		case ast.PdfDict:
			for _, key := range slices.Sorted(maps.Keys(v)) {
				stack = append(stack, v[key])
			}
		case ast.PdfStream:
			stack = append(stack, v.Dict)
		}
	}
	return refs
}

// isPageTreeObject reports whether dict is a page or a page tree node by its
// /Type (ISO 32000-1, Tables 29 and 30).
func isPageTreeObject(dict ast.PdfDict) bool {
	typ, _ := dict["Type"].(ast.PdfName)
	return typ == "Page" || typ == "Pages"
}

// onePlaceKeys are the entries that tie an object to one place in a document:
// the structure keys that index one /ParentTree entry (ISO 32000-1, 14.7.4.4),
// the parent and page an annotation, a field or a structure element names
// (Tables 164, 220 and 323), and the entries naming a field (Table 220).
var onePlaceKeys = []ast.PdfName{"StructParents", "StructParent", "P", "Pg", "Parent", "Kids", "FT", "T"}

// onePlaceTypes are the object types that stand at one place: a page or a
// page tree node (Tables 29 and 30), an annotation (Table 164), and the
// structure tree and its elements and references (Tables 322 to 325).
var onePlaceTypes = []ast.PdfName{"Page", "Pages", "Annot", "StructTreeRoot", "StructElem", "MCR", "OBJR"}

// ownedByOnePlace reports whether dict, an object a repeated page reaches,
// belongs to one place in the document, so a second copy of the page cannot
// share it with the first: a page, an annotation, a form field, a structure
// element, or an object whose structure key names one /ParentTree entry. An
// annotation is recognised by the /Subtype and /Rect ISO 32000-1, Table 164
// requires even where it states no /Type. The test errs towards owning: an
// object it owns wrongly is copied again, which costs bytes and nothing else.
func ownedByOnePlace(dict ast.PdfDict) bool {
	if dict == nil {
		return false
	}
	if typ, _ := dict["Type"].(ast.PdfName); slices.Contains(onePlaceTypes, typ) {
		return true
	}
	for _, key := range onePlaceKeys {
		if _, has := dict[key]; has {
			return true
		}
	}
	_, hasRect := dict["Rect"]
	_, hasSubtype := dict["Subtype"]
	return hasRect && hasSubtype
}
