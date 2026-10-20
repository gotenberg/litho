package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// contentSharing tells [replaceContentStream] which pages name a content
// stream in their /Contents, so the rewrite of one page's direct /Contents
// array leaves a stream another page draws as that page reads it.
//
// ISO 32000-1, 7.7.3.3 lets /Contents be an array of streams, and nothing
// keeps two pages from naming the same stream, or one array from naming a
// stream twice. The rewrite of such an array in place writes the page's whole
// content into its first stream and empties the others, which takes a later
// shared stream off the other page and puts this page's content on it through
// a shared first stream.
//
// entries holds the page tree entries in page order, which the page number a
// rewrite is handed indexes. users is built on the first array rewrite from
// the pages' /Contents as they then stand, and is not rebuilt: a rewrite never
// makes two pages share a stream, so what it reads stale is a stream no longer
// shared, which only sends a page to the write that shares nothing. A
// contentSharing serves one fixer's pass over the pages, on one goroutine.
type contentSharing struct {
	entries []annotPageEntry
	// users maps the object number of each stream a page's /Contents names to
	// the indexes of the first two distinct pages naming it, or to the one
	// index twice when the page names it twice: two entries tell it shared.
	users map[int][]int
}

// newContentSharing returns the [contentSharing] of the pages entries lists in
// page order ([pageTreeEntries]).
func newContentSharing(entries []annotPageEntry) *contentSharing {
	return &contentSharing{entries: entries}
}

// pageContentSharing returns the [contentSharing] of pages, the document's
// pages in order.
func pageContentSharing(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page) *contentSharing {
	return newContentSharing(pageTreeEntries(ctx, alloc, doc, pages))
}

// page returns the page tree entry of the page numbered pageNum, from 1, and
// false when the entries hold no such page.
func (s *contentSharing) page(pageNum int) (annotPageEntry, bool) {
	if s == nil || pageNum < 1 || pageNum > len(s.entries) {
		return annotPageEntry{}, false
	}
	return s.entries[pageNum-1], true
}

// shared reports whether a stream arr names, the direct /Contents array of the
// page numbered pageNum, is named again in arr or by another page's /Contents.
func (s *contentSharing) shared(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, arr ast.PdfArray, pageNum int) bool {
	if s.users == nil {
		s.build(ctx, alloc, doc)
	}
	seen := make(map[int]bool, len(arr))
	for _, elem := range arr {
		ref, ok := elem.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		if seen[ref.ObjectNum] {
			return true
		}
		seen[ref.ObjectNum] = true
		for _, user := range s.users[ref.ObjectNum] {
			if user != pageNum-1 {
				return true
			}
		}
	}
	return false
}

// build records the users of every stream the pages' /Contents name, read
// through the allocator: the elements of an array, direct or at an object
// number, or the stream /Contents names alone.
func (s *contentSharing) build(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) {
	s.users = make(map[int][]int)
	for i, pe := range s.entries {
		if ctx.Err() != nil {
			return
		}
		contents, ok := liveTreeNode(ctx, alloc, doc, pe)["Contents"]
		if !ok {
			continue
		}
		var refs ast.PdfArray
		switch v := contents.(type) {
		case ast.PdfArray:
			refs = v
		case ast.PdfIndirectRef:
			if refs = liveArray(ctx, alloc, doc, v); refs == nil {
				refs = ast.PdfArray{v}
			}
		}
		for _, elem := range refs {
			if ref, ok := elem.(ast.PdfIndirectRef); ok && len(s.users[ref.ObjectNum]) < 2 {
				s.users[ref.ObjectNum] = append(s.users[ref.ObjectNum], i)
			}
		}
	}
}

// writeOwnContentStream queues newData as a new stream and points the page
// numbered pageNum at it, leaving the streams its /Contents named as they are
// for the other pages that draw them.
func (s *contentSharing) writeOwnContentStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, newData []byte, pageNum int) error {
	pe, ok := s.page(pageNum)
	if !ok || !treeNodeWritable(pe) {
		return fmt.Errorf("page %d shares a content stream and its dictionary has no place to be written", pageNum)
	}
	dict := maps.Clone(liveTreeNode(ctx, alloc, doc, pe))
	dict["Contents"] = alloc.Alloc(ast.NewStream(ast.PdfDict{}, newData))
	if !writeTreeNode(ctx, alloc, doc, pe, dict) {
		return fmt.Errorf("page %d shares a content stream and its dictionary cannot be written", pageNum)
	}
	return nil
}
