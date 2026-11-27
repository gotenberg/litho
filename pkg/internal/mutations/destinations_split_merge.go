package mutations

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// destSource is one source whose named destinations a split or a merge
// carries: its document, the copier that carried its pages, and the object
// numbers of those pages.
type destSource struct {
	doc    *document.Document
	copier *writer.Copier
	pages  map[int]bool
}

// pageObjectNums returns the object numbers of pages.
func pageObjectNums(pages []*document.Page) map[int]bool {
	nums := make(map[int]bool, len(pages))
	for _, page := range pages {
		nums[page.Ref.ObjectNum] = true
	}
	return nums
}

// rebuildDestinations writes the named destinations of every source onto dst,
// the output's catalog, or leaves dst untouched when no source names one on a
// page the output holds.
//
// A document names its destinations in the catalog's /Dests dictionary (ISO
// 32000-1, Table 28) or in the /Dests tree of its /Names dictionary (Table 31),
// and a link, an outline item or a view another document opens can name one
// (12.3.2.3). Split and merge build a fresh catalog and copied neither, so a
// table of contents, whose links a browser writes by name, opened nothing.
// The links of the copied pages already name their pages directly (see
// [pageDestOverrides]); the tables keep each name addressable from outside the
// file.
//
// Each table keeps the form its source wrote. A name two sources define keeps
// the first source's entry, as a name tree holds each key once (7.9.6): the
// later source's links still reach their own pages. An entry is carried only
// when its destination names a page the output holds, which also keeps the
// copy from following it into a page a split leaves out. It is written as the
// explicit destination array: the structure destination a dictionary value can
// add (ISO 32000-2, 12.3.2.3) names an element the output's structure tree
// rebuilt, and a destination naming its page by number (12.3.2.2) names a page
// whose number the copy changes.
//
// The tables are read best effort. A tree that cannot be walked whole loses
// the names past the break, never a page or a link.
func rebuildDestinations(ctx context.Context, alloc *writer.ObjAllocator, sources []destSource, dst ast.PdfDict) {
	dests := make(ast.PdfDict)
	var tree []destEntry
	inTree := make(map[string]bool)

	for _, src := range sources {
		if ctx.Err() != nil {
			return
		}
		if src.copier == nil {
			continue
		}
		catalog, err := document.ResolveCatalog(ctx, src.doc)
		if err != nil || catalog == nil {
			continue
		}

		if dict, err := document.ResolveDict(ctx, src.doc, catalog.Dict["Dests"]); err == nil && dict != nil {
			for _, name := range slices.Sorted(maps.Keys(dict)) {
				if _, taken := dests[name]; taken {
					continue
				}
				if dest, ok := carriedDest(ctx, src, dict[name]); ok {
					dests[name] = dest
				}
			}
		}

		names, err := document.ResolveDict(ctx, src.doc, catalog.Dict["Names"])
		if err != nil || names == nil {
			continue
		}
		root, ok := names["Dests"]
		if !ok {
			continue
		}
		walkErr := document.WalkNameTree(ctx, src.doc, root, func(key string, value ast.PdfObject) {
			if key == "" || inTree[key] {
				return
			}
			if dest, ok := carriedDest(ctx, src, value); ok {
				inTree[key] = true
				tree = append(tree, destEntry{key: key, dest: dest})
			}
		})
		if walkErr != nil {
			src.doc.Logger.Debug("reading the named destinations of a source failed", slog.Any("err", walkErr))
		}
	}

	if len(dests) > 0 {
		dst["Dests"] = alloc.Alloc(dests)
	}
	if len(tree) > 0 {
		// Keys are byte strings, ordered by byte value (ISO 32000-1, 7.9.6). A
		// root holding only /Names needs no /Limits or /Kids.
		sort.Slice(tree, func(i, j int) bool { return tree[i].key < tree[j].key })
		pairs := make(ast.PdfArray, 0, 2*len(tree))
		for _, e := range tree {
			pairs = append(pairs, ast.PdfString(e.key), e.dest)
		}
		addNameTree(alloc, dst, "Dests", alloc.Alloc(ast.PdfDict{"Names": pairs}))
	}
}

// destEntry is one key of a /Dests name tree and the destination it maps to.
type destEntry struct {
	key  string
	dest ast.PdfObject
}

// carriedDest returns the copy of the explicit destination a named destination
// value maps to, and false when it names no page of src the output holds or
// cannot be copied.
func carriedDest(ctx context.Context, src destSource, value ast.PdfObject) (ast.PdfObject, bool) {
	arr, err := document.ResolveDestValue(ctx, src.doc, value)
	if err != nil || len(arr) == 0 {
		return nil, false
	}
	page, ok := arr[0].(ast.PdfIndirectRef)
	if !ok || !src.pages[page.ObjectNum] {
		return nil, false
	}
	copied, err := src.copier.CopyObject(ctx, arr)
	if err != nil {
		src.doc.Logger.Debug("copying a named destination failed", slog.Any("err", err))
		return nil, false
	}
	return copied, true
}

// addNameTree names root as the key tree of the /Names dictionary of catalog,
// a catalog a rewrite builds, adding to the dictionary alloc holds for it or
// allocating one.
func addNameTree(alloc *writer.ObjAllocator, catalog ast.PdfDict, key ast.PdfName, root ast.PdfObject) {
	if ref, ok := catalog["Names"].(ast.PdfIndirectRef); ok {
		if names, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict); ok {
			names[key] = root
			return
		}
	}
	catalog["Names"] = alloc.Alloc(ast.PdfDict{key: root})
}
