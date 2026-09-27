package mutations

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

// OutlineItem is one item of an outline litho writes: its title, the 1-based
// output page it opens, its children, and the structure destination of the
// outline item it was read from.
type OutlineItem struct {
	Title      string
	PageNumber int
	Children   []OutlineItem
	// structDest is the structure destination the item read names (ISO
	// 32000-2, 12.3.2.3), an array in the objects of the document it was read
	// from, nil when it names none. source is that document's index among the
	// documents the output takes pages from.
	structDest ast.PdfArray
	source     int
}

// outlineItems returns bookmarks as outline items naming no structure
// destination.
func outlineItems(bookmarks []models.Bookmark) []OutlineItem {
	if len(bookmarks) == 0 {
		return nil
	}
	out := make([]OutlineItem, len(bookmarks))
	for i, bm := range bookmarks {
		out[i] = OutlineItem{Title: bm.Title, PageNumber: bm.PageNumber, Children: outlineItems(bm.Children)}
	}
	return out
}

// sourceOutline reads doc's outline as the items a split or a merge carries,
// each with the structure destination its outline item names. source is doc's
// index among the documents the output takes pages from, and pages are doc's
// pages.
//
// An item whose only destination is a structure destination names no page, so
// it opens the page its structure element's content begins on, as a reader
// following the structure destination shows (ISO 32000-2, 12.3.2.3).
func sourceOutline(ctx context.Context, doc *document.Document, pages []*document.Page, source int) ([]OutlineItem, error) {
	pageMap := document.BuildPageMap(pages)
	return semantics.ResolveOutlineItems(ctx, doc, func(b models.Bookmark, item ast.PdfDict, children []OutlineItem) OutlineItem {
		out := OutlineItem{Title: b.Title, PageNumber: b.PageNumber, Children: children, source: source}
		out.structDest = itemStructDest(ctx, doc, item)
		if out.PageNumber == 0 && out.structDest != nil {
			resolve := func(obj ast.PdfObject) ast.PdfObject {
				resolved, err := document.ResolveObject(ctx, doc, obj)
				if err != nil {
					return nil
				}
				return resolved
			}
			if elem, ok := resolve(out.structDest[0]).(ast.PdfDict); ok {
				if page, found := newElementPages(resolve).first(ctx, elem); found {
					out.PageNumber = pageMap[page]
				}
			}
		}
		return out
	})
}

// itemStructDest returns the structure destination an outline item names, or
// nil. A GoTo action's /SD is the one a reader supporting structure
// destinations follows in preference to its /D (ISO 32000-2, 12.6.4.2, Table
// 202). Otherwise the item's destination, its /Dest or its action's /D, is one
// when it is a structure destination itself (see [destStructDest]).
func itemStructDest(ctx context.Context, doc *document.Document, item ast.PdfDict) ast.PdfArray {
	dest, hasDest := item["Dest"]
	if !hasDest {
		action, err := document.ResolveDict(ctx, doc, item["A"])
		if err != nil || action == nil {
			return nil
		}
		if s, _ := document.ResolveName(ctx, doc, action["S"]); s != "GoTo" {
			return nil
		}
		if sd := structDestArray(ctx, doc, action["SD"]); sd != nil {
			return sd
		}
		dest = action["D"]
	}
	return destStructDest(ctx, doc, dest, 0)
}

// destStructDest returns the structure destination dest is, or nil: an array
// whose first element is a structure element (ISO 32000-2, 12.3.2.3), or a
// dictionary carrying one under /SD, a form a named destination's value takes
// (12.3.2). A name or a string stands for the value it names. A dictionary
// with no /SD is read through its /D, as a GoTo action some producers write
// in /Dest is, and as [document.ResolveDestinationPage] reads one.
func destStructDest(ctx context.Context, doc *document.Document, dest ast.PdfObject, depth int) ast.PdfArray {
	if depth > maxDestChainDepth || dest == nil {
		return nil
	}
	resolved, err := document.ResolveObject(ctx, doc, dest)
	if err != nil {
		return nil
	}
	switch v := resolved.(type) {
	case ast.PdfName, ast.PdfString:
		return destStructDest(ctx, doc, namedDestination(ctx, doc, v), depth+1)
	case ast.PdfDict:
		if sd := structDestArray(ctx, doc, v["SD"]); sd != nil {
			return sd
		}
		return destStructDest(ctx, doc, v["D"], depth+1)
	case ast.PdfArray:
		return structDestArray(ctx, doc, v)
	}
	return nil
}

// namedDestination returns the value a named destination maps to, unresolved,
// or nil. A string names an entry of the /Dests name tree of the catalog's
// /Names, and a name an entry of the catalog's /Dests dictionary (ISO 32000-1,
// 12.3.2.3), each keyed by its bytes.
func namedDestination(ctx context.Context, doc *document.Document, name ast.PdfObject) ast.PdfObject {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil || catalog == nil {
		return nil
	}
	switch v := name.(type) {
	case ast.PdfName:
		dests, err := document.ResolveDict(ctx, doc, catalog.Dict["Dests"])
		if err != nil || dests == nil {
			return nil
		}
		return dests[v]
	case ast.PdfString:
		names, err := document.ResolveDict(ctx, doc, catalog.Dict["Names"])
		if err != nil || names == nil {
			return nil
		}
		value, err := document.LookupNameTree(ctx, doc, names["Dests"], string(v))
		if err != nil {
			doc.Logger.Debug("outline: looking up a named destination failed", "error", err)
			return nil
		}
		return value
	}
	return nil
}

// structDestArray returns obj as a structure destination: an array whose first
// element is a reference to a structure element, the dictionary carrying /S
// (ISO 32000-1, 14.7.2, Table 323), and whose other elements are the view
// parameters of an explicit destination (ISO 32000-1, 12.3.2.2, Table 151),
// read to the numbers, names and nulls they are. It returns nil for anything
// else, a page destination included.
func structDestArray(ctx context.Context, doc *document.Document, obj ast.PdfObject) ast.PdfArray {
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) == 0 {
		return nil
	}
	ref, isRef := arr[0].(ast.PdfIndirectRef)
	if !isRef {
		return nil
	}
	elem, err := document.ResolveDict(ctx, doc, ref)
	if err != nil || elem == nil {
		return nil
	}
	if s, err := document.ResolveName(ctx, doc, elem["S"]); err != nil || s == "" {
		return nil
	}
	out := ast.PdfArray{ref}
	for _, param := range arr[1:] {
		value, err := document.ResolveObject(ctx, doc, param)
		if err != nil {
			return nil
		}
		switch value.(type) {
		case ast.PdfName, ast.PdfInt, ast.PdfReal, ast.PdfNull:
			out = append(out, value)
		default:
			return nil
		}
	}
	return out
}

// outlineTargets decides the structure destination each item of an outline
// opens beside its page (see [buildOutlineItems]).
//
// ISO 14289-2, 8.8 asks every destination inside the document to be a
// structure destination, and ISO 32000-2, 12.6.4.2 has a GoTo action name one
// under /SD beside the page destination under /D, which a reader supporting
// structure destinations follows instead. An item carries the structure
// element its source item named when the output holds that element. Otherwise
// it opens the first element in the output's logical order whose content
// begins on its page (see [StructPageTargets]): the element a reader following
// the structure destination shows on the page the item opens. That is how an
// item whose element left with its pages keeps one, and how an item that named
// a page alone gains one. An item on a page no element begins on opens the
// page alone.
//
// # Corpus evidence
//
//   - All corpora: a split of page 1 of each of 1124 tagged files added ISO
//     14289-2, 8.8 test 1 to 120 outputs whose source passed it, and a merge of
//     each with the next to 153. 118 of the 120 rebuilt a GoTo action their
//     source already failed test 2 with. With structure destinations, 2 splits
//     and 8 merges add it, their items on pages no element begins on.
//   - pdfjs bug1997343.pdf, bug2009627.pdf: the GoTo actions of their items
//     name a structure destination under /SD, which a split and a merge
//     dropped.
type outlineTargets struct {
	// carry returns the output's copy of a structure element of the source
	// document at index source, and false when the output does not hold it.
	carry func(source int, elem ast.PdfIndirectRef) (ast.PdfIndirectRef, bool)
	// byPage maps an output page's object number to the element its
	// structure destination names.
	byPage map[int]ast.PdfIndirectRef
}

// structDest returns the structure destination item opens on page, or nil.
func (o *outlineTargets) structDest(item OutlineItem, page ast.PdfIndirectRef) ast.PdfArray {
	if o == nil {
		return nil
	}
	if len(item.structDest) > 0 && o.carry != nil {
		if elem, isRef := item.structDest[0].(ast.PdfIndirectRef); isRef {
			if dst, held := o.carry(item.source, elem); held {
				out := make(ast.PdfArray, 0, len(item.structDest))
				out = append(out, dst)
				return append(out, item.structDest[1:]...)
			}
		}
	}
	if elem, known := o.byPage[page.ObjectNum]; known {
		return ast.PdfArray{elem, ast.PdfName("Fit")}
	}
	return nil
}

// outputOutlineTargets returns the [outlineTargets] of a split's or a merge's
// output, whose structure tree root alloc holds at root, or nil when the
// output has no tree. sources are the structure sources the output carries,
// indexed as the documents it takes pages from, nil for one whose structure it
// does not carry.
func outputOutlineTargets(ctx context.Context, alloc *writer.ObjAllocator, root ast.PdfIndirectRef, sources []*structSource) *outlineTargets {
	if root.ObjectNum == 0 {
		return nil
	}
	rootDict, isDict := alloc.Pending()[root.ObjectNum].(ast.PdfDict)
	if !isDict {
		return nil
	}
	return &outlineTargets{
		carry:  carriedElement(sources),
		byPage: StructPageTargets(ctx, AllocResolver(ctx, alloc, nil), rootDict["K"]),
	}
}

// carriedElement returns the carry function of [outlineTargets] for the
// structure sources of a split or a merge, indexed as the output's sources: a
// structure element whose source's prune kept it has the copy its source's
// copier wrote. A source whose structure the output does not carry is nil.
func carriedElement(sources []*structSource) func(int, ast.PdfIndirectRef) (ast.PdfIndirectRef, bool) {
	return func(source int, elem ast.PdfIndirectRef) (ast.PdfIndirectRef, bool) {
		if source < 0 || source >= len(sources) {
			return ast.PdfIndirectRef{}, false
		}
		s := sources[source]
		if s == nil || s.copier == nil || !s.prune.kept(elem.ObjectNum) {
			return ast.PdfIndirectRef{}, false
		}
		dst, copied := s.copier.Mapping()[elem.ObjectNum]
		if !copied {
			return ast.PdfIndirectRef{}, false
		}
		return ast.PdfIndirectRef{ObjectNum: dst}, true
	}
}

// StructPageTargets maps each page's object number to the first structure
// element, in the logical order of the tree under rootK (ISO 32000-1,
// 14.7.2), whose content begins on that page (see [elementPages.first]), read
// through resolve. An element is visited before its kids, so a page names the
// outermost element beginning on it. Only an element written as an object of
// its own can be a destination's target.
func StructPageTargets(ctx context.Context, resolve func(ast.PdfObject) ast.PdfObject, rootK ast.PdfObject) map[int]ast.PdfIndirectRef {
	targets := make(map[int]ast.PdfIndirectRef)
	pages := newElementPages(resolve)
	visited := make(map[int]bool)
	var walk func(kObj ast.PdfObject, depth int)
	walk = func(kObj ast.PdfObject, depth int) {
		if depth > maxStructTreeDepth || ctx.Err() != nil {
			return
		}
		for _, kid := range pages.kids(kObj) {
			ref, isRef := kid.(ast.PdfIndirectRef)
			if isRef {
				if visited[ref.ObjectNum] {
					continue
				}
				visited[ref.ObjectNum] = true
			}
			elem, isDict := resolve(kid).(ast.PdfDict)
			if !isDict {
				continue
			}
			if !pages.isElement(elem) {
				continue
			}
			if isRef {
				if page, found := pages.first(ctx, elem); found {
					if _, taken := targets[page.ObjectNum]; !taken {
						targets[page.ObjectNum] = ref
					}
				}
			}
			walk(elem["K"], depth+1)
		}
	}
	walk(rootK, 0)
	return targets
}

// elementPages finds the page a structure element's content begins on,
// reading objects through resolve.
type elementPages struct {
	resolve func(ast.PdfObject) ast.PdfObject
	memo    map[int]elementStart
	onPath  map[int]bool
}

// elementStart is where an element's content begins: found with the page it
// begins on, or not found, empty telling an element holding no kid from one
// whose first content item names no page.
type elementStart struct {
	page  ast.PdfIndirectRef
	found bool
	empty bool
}

// newElementPages returns an [elementPages] reading objects through resolve.
func newElementPages(resolve func(ast.PdfObject) ast.PdfObject) *elementPages {
	return &elementPages{resolve: resolve, memo: make(map[int]elementStart), onPath: make(map[int]bool)}
}

// isElement reports whether d is a structure element: a dictionary whose /S
// names its structure type (ISO 32000-1, 14.7.2, Table 323), directly or
// through an indirect reference, which some producers write.
//
// # Corpus evidence
//
//   - govdocs: 5 tagged files name the type of some of their elements through
//     an indirect reference to a name object, thread_000_000761.pdf 42 of its
//     69, which left 2 pages its outline opens with no element beginning on
//     them.
func (e *elementPages) isElement(d ast.PdfDict) bool {
	_, isName := e.resolve(d["S"]).(ast.PdfName)
	return isName
}

// kids returns the entries of a /K value: the array's, or the one entry it is.
func (e *elementPages) kids(kObj ast.PdfObject) ast.PdfArray {
	switch v := e.resolve(kObj).(type) {
	case nil, ast.PdfNull:
		return nil
	case ast.PdfArray:
		return v
	}
	return ast.PdfArray{kObj}
}

// first returns the page an element's first content item lies on, in the
// order of its /K (ISO 32000-1, 14.7.2): a marked-content identifier lies on
// the element's /Pg, and a marked-content reference or an object reference on
// its own /Pg, else the element's (Tables 323 to 325). A kid element's first
// content item is its own, and a kid element holding no kid is passed over.
// found is false when the first content item names no page, so the page the
// element begins on is not known, and when the element holds none.
func (e *elementPages) first(ctx context.Context, elem ast.PdfDict) (ast.PdfIndirectRef, bool) {
	start := e.start(ctx, elem, 0)
	return start.page, start.found
}

// start is [elementPages.first] telling an empty element apart.
func (e *elementPages) start(ctx context.Context, elem ast.PdfDict, depth int) elementStart {
	if depth > maxStructTreeDepth || ctx.Err() != nil {
		return elementStart{}
	}
	pg, hasPg := elem["Pg"].(ast.PdfIndirectRef)
	onElementPage := elementStart{page: pg, found: hasPg}
	kids := e.kids(elem["K"])
	for _, kid := range kids {
		if _, isMCID := kid.(ast.PdfInt); isMCID {
			return onElementPage
		}
		ref, isRef := kid.(ast.PdfIndirectRef)
		if isRef && e.onPath[ref.ObjectNum] {
			return elementStart{}
		}
		d, isDict := e.resolve(kid).(ast.PdfDict)
		if !isDict {
			continue
		}
		switch kind, _ := e.resolve(d["Type"]).(ast.PdfName); kind {
		case "MCR", "OBJR":
			if own, has := d["Pg"].(ast.PdfIndirectRef); has {
				return elementStart{page: own, found: true}
			}
			return onElementPage
		}
		if !e.isElement(d) {
			continue
		}
		kidStart, memoised := elementStart{}, false
		if isRef {
			kidStart, memoised = e.memo[ref.ObjectNum]
		}
		if !memoised {
			if isRef {
				e.onPath[ref.ObjectNum] = true
			}
			kidStart = e.start(ctx, d, depth+1)
			if isRef {
				delete(e.onPath, ref.ObjectNum)
				e.memo[ref.ObjectNum] = kidStart
			}
		}
		if !kidStart.empty {
			return kidStart
		}
	}
	return elementStart{empty: len(kids) == 0}
}

// AllocResolver returns a resolve function reading an object from alloc first,
// and from doc when alloc holds none at its number and doc is not nil.
func AllocResolver(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) func(ast.PdfObject) ast.PdfObject {
	return func(obj ast.PdfObject) ast.PdfObject {
		ref, isRef := obj.(ast.PdfIndirectRef)
		if !isRef {
			return obj
		}
		if pending, queued := alloc.Pending()[ref.ObjectNum]; queued {
			return pending
		}
		if doc == nil {
			return nil
		}
		resolved, err := document.ResolveObject(ctx, doc, ref)
		if err != nil {
			return nil
		}
		return resolved
	}
}
