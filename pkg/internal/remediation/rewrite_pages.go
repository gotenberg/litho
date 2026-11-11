package remediation

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// queueMalformedPages queues, unchanged, every page an incremental update
// would carry over malformed, and the indirect resource dictionary of one
// whose resources are, so the write that follows repairs them, and returns
// how many objects it queued: a page holding a stream inside it, or inside
// the resource dictionary it names, which ISO 32000-1, 7.3.8.1 requires to be
// an indirect object, and a page with no media box of its own or inherited,
// which Table 30 requires.
//
// An incremental update writes the objects queued with every stream they
// hold made indirect and every page with the entries 7.7.3 requires, and
// leaves the others as the source has them. A page that already holds what
// it should is left alone, so the output grows by the pages it repairs. A
// page an earlier fixer queued is read through the allocator and queued as
// that fixer left it.
//
// # Corpus evidence
//
//   - litho/pdf directstream_2b.pdf: its page holds a form and a content
//     stream inline. A PDF/A-2b remediation wrote them indirect only while
//     the DefaultCMYK staged on every page rewrote the page.
func queueMalformedPages(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) int {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return 0
	}
	pagesObj, ok := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)["Pages"]
	if !ok {
		return 0
	}
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool))
	queued := 0
	for _, pe := range pages {
		if ctx.Err() != nil {
			return queued
		}
		page := liveTreeNode(ctx, alloc, doc, pe)
		if ref, isRef := page["Resources"].(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
			if res := resolveFromAllocOrDoc(ctx, alloc, doc, ref); res != nil && holdsInlineStream(res, true) && alloc.AllocAt(ref.ObjectNum, maps.Clone(res)) {
				queued++
			}
		}
		if (holdsInlineStream(page, true) || !inheritsMediaBox(ctx, alloc, doc, pe)) && writeTreeNode(ctx, alloc, doc, pe, maps.Clone(page)) {
			queued++
		}
	}
	return queued
}

// holdsInlineStream reports whether obj holds a stream below its top level,
// a stream at the top of an object being where it belongs.
func holdsInlineStream(obj ast.PdfObject, top bool) bool {
	switch v := obj.(type) {
	case ast.PdfStream:
		return !top || holdsInlineStream(v.Dict, true)
	case ast.PdfDict:
		for _, e := range v {
			if holdsInlineStream(e, false) {
				return true
			}
		}
	case ast.PdfArray:
		for _, e := range v {
			if holdsInlineStream(e, false) {
				return true
			}
		}
	}
	return false
}

// inheritsMediaBox reports whether the page pe, or a node of the page tree
// the walk reached it through, carries a /MediaBox (ISO 32000-1, 7.7.3.4).
func inheritsMediaBox(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry) bool {
	for node := &pe; node != nil; node = node.parent {
		if _, ok := liveTreeNode(ctx, alloc, doc, *node)["MediaBox"]; ok {
			return true
		}
	}
	return false
}
