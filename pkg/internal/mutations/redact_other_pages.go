package mutations

import (
	"context"
	"log/slog"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// OtherPageCopy is a surviving copy of a removed string that only pages a
// redaction did not cover draw.
type OtherPageCopy struct {
	Hit TokenHit
	// Pages are the 1-based pages whose content draws the object, ascending.
	Pages []int
}

// SplitOtherPageCopies separates the hits held only by what other pages draw
// from the rest.
//
// A region redaction removes what the region covers on its own page: ISO
// 32000-2, 12.5.6.23 applies a Redact annotation to the page it sits on, and
// the same text drawn on another page is content the caller did not select. A
// hit counts as such a copy when its object is a stream another page draws (a
// content stream of its /Contents, a form XObject or tiling pattern its
// resources name, a Type 3 glyph description, an annotation appearance stream,
// and the same reached from those streams' own resources) and no redacted page
// draws or names it. Every other hit is returned in rest: a copy in the
// redacted page's own resources or annotations, the structure tree, metadata,
// or any stream no page draws.
//
// redacted holds the 1-based numbers of the pages the redaction covered. Pages
// are read as alloc stages them, so a redacted page is judged by the content
// that replaced its own. When the walk of a redacted page stops at the form
// depth budget, it cannot prove that page does not draw a deeper stream, and
// every hit is returned in rest.
//
// # Corpus evidence
//
//   - govdocs, pdfcpu, pdfjs, redacting all of page 1: every surviving copy
//     is one other pages draw in 355 of 459 govdocs refusals with
//     ErrRedactionIncomplete, 4 of 10 pdfcpu and 19 of 60 pdfjs ones, mostly
//     running headers and footers (pdfjs issue11532.pdf, "Federal Reserve
//     Bank of Dallas" on pages 3 and 5).
//   - litho readcolumns_ua1.pdf: the one fixture that moves, its title drawn
//     again on page 2.
func SplitOtherPageCopies(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	redacted map[int]bool,
	hits []TokenHit,
) ([]OtherPageCopy, []TokenHit, error) {
	if len(hits) == 0 {
		return nil, hits, nil
	}
	redactedDraws := make(map[int]bool)
	otherDraws := make(map[int][]int)
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		drawn := newPageDrawWalk(alloc, doc)
		drawn.page(ctx, latestPage(ctx, alloc, doc, page))
		if redacted[i+1] {
			if drawn.truncated {
				return nil, hits, nil
			}
			for objNum := range drawn.streams {
				redactedDraws[objNum] = true
			}
			continue
		}
		for objNum := range drawn.streams {
			otherDraws[objNum] = append(otherDraws[objNum], i+1)
		}
	}

	var copies []OtherPageCopy
	var rest []TokenHit
	for _, hit := range hits {
		onPages := otherDraws[hit.ObjectNum]
		if hit.ObjectNum == 0 || len(onPages) == 0 || redactedDraws[hit.ObjectNum] {
			rest = append(rest, hit)
			continue
		}
		copies = append(copies, OtherPageCopy{Hit: hit, Pages: slices.Clone(onPages)})
	}
	return copies, rest, nil
}

// pageDrawWalk collects the streams one page draws.
type pageDrawWalk struct {
	alloc    *writer.ObjAllocator
	doc      *document.Document
	maxDepth int

	streams map[int]bool
	// seen marks every indirect dictionary and array the walk descends
	// through, so a resource dictionary or /Annots array a crafted file
	// names from many places is walked once.
	seen map[int]bool
	// truncated records that a form chain ran past the depth budget, so the
	// set is a lower bound.
	truncated bool
}

func newPageDrawWalk(alloc *writer.ObjAllocator, doc *document.Document) *pageDrawWalk {
	return &pageDrawWalk{
		alloc:    alloc,
		doc:      doc,
		maxDepth: doc.Limits().MaxFormDepth,
		streams:  make(map[int]bool),
		seen:     make(map[int]bool),
	}
}

// page walks /Contents, the resources and the annotation appearances.
func (w *pageDrawWalk) page(ctx context.Context, page *document.Page) {
	if contents, ok := page.Dict["Contents"]; ok {
		if ref, isRef := contents.(ast.PdfIndirectRef); isRef {
			if _, isStream := w.resolve(ctx, ref).(ast.PdfStream); isStream {
				w.streams[ref.ObjectNum] = true
			} else if !document.VisitedBefore(ref, w.seen) {
				w.contentArray(w.resolve(ctx, ref))
			}
		} else {
			w.contentArray(contents)
		}
	}
	w.resources(ctx, page.Resources, 0)
	w.annotations(ctx, page.Dict)
}

func (w *pageDrawWalk) contentArray(obj ast.PdfObject) {
	arr, _ := obj.(ast.PdfArray)
	for _, item := range arr {
		if ref, isRef := item.(ast.PdfIndirectRef); isRef {
			w.streams[ref.ObjectNum] = true
		}
	}
}

// resources walks the streams a resource dictionary names that draw content
// when painted: form XObjects (ISO 32000-1, 8.10), tiling patterns (8.7.3.1)
// and Type 3 glyph descriptions (9.6.5). depth counts the forms above it.
func (w *pageDrawWalk) resources(ctx context.Context, obj ast.PdfObject, depth int) {
	if document.VisitedBefore(obj, w.seen) {
		return
	}
	res := w.dict(ctx, obj)
	if res == nil {
		return
	}
	for _, key := range []ast.PdfName{"XObject", "Pattern"} {
		for _, entry := range w.subDict(ctx, res[key]) {
			w.drawnStream(ctx, entry, depth, isContentStreamDict)
		}
	}
	for _, entry := range w.subDict(ctx, res["Font"]) {
		if document.VisitedBefore(entry, w.seen) {
			continue
		}
		font := w.dict(ctx, entry)
		if subtype, _ := font["Subtype"].(ast.PdfName); subtype != "Type3" {
			continue
		}
		for _, proc := range w.subDict(ctx, font["CharProcs"]) {
			if ref, isRef := proc.(ast.PdfIndirectRef); isRef {
				w.streams[ref.ObjectNum] = true
			}
		}
		w.resources(ctx, font["Resources"], depth+1)
	}
}

// drawnStream records a stream that draws content and walks its resources one
// form deeper.
func (w *pageDrawWalk) drawnStream(ctx context.Context, obj ast.PdfObject, depth int, draws func(ast.PdfDict) bool) {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if !isRef || w.streams[ref.ObjectNum] {
		return
	}
	stream, isStream := w.resolve(ctx, ref).(ast.PdfStream)
	if !isStream || !draws(stream.Dict) {
		return
	}
	if depth+1 > w.maxDepth {
		w.truncated = true
		return
	}
	w.streams[ref.ObjectNum] = true
	w.resources(ctx, stream.Dict["Resources"], depth+1)
}

// annotations walks the appearance streams of the page's annotations: each
// /N, /R and /D entry of /AP is a stream or a dictionary of streams (ISO
// 32000-1, 12.5.5, Table 168).
func (w *pageDrawWalk) annotations(ctx context.Context, pageDict ast.PdfDict) {
	if document.VisitedBefore(pageDict["Annots"], w.seen) {
		return
	}
	for _, entry := range latestAnnots(ctx, w.alloc, w.doc, pageDict) {
		if document.VisitedBefore(entry, w.seen) {
			continue
		}
		ap := w.dict(ctx, w.dict(ctx, entry)["AP"])
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			appearance := ap[key]
			if ref, isRef := appearance.(ast.PdfIndirectRef); isRef {
				if _, isStream := w.resolve(ctx, ref).(ast.PdfStream); isStream {
					w.drawnStream(ctx, ref, 0, func(ast.PdfDict) bool { return true })
					continue
				}
			}
			for _, state := range w.subDict(ctx, appearance) {
				w.drawnStream(ctx, state, 0, func(ast.PdfDict) bool { return true })
			}
		}
	}
}

// resolve reads obj as the output would hold it, nil when it does not resolve.
func (w *pageDrawWalk) resolve(ctx context.Context, obj ast.PdfObject) ast.PdfObject {
	resolved, err := resolveAllocFirst(ctx, w.alloc, w.doc, obj)
	if err != nil {
		w.doc.Logger.Debug("redaction copy walk: object does not resolve, so no page draws it",
			slog.Any("err", err))
		return nil
	}
	return resolved
}

// dict reads obj as a dictionary, nil when it is not one.
func (w *pageDrawWalk) dict(ctx context.Context, obj ast.PdfObject) ast.PdfDict {
	if obj == nil {
		return nil
	}
	d, _ := w.resolve(ctx, obj).(ast.PdfDict)
	return d
}

// subDict reads a resource category, marking it walked when it is an object
// of its own, so a category many dictionaries share is read once.
func (w *pageDrawWalk) subDict(ctx context.Context, obj ast.PdfObject) ast.PdfDict {
	if obj == nil || document.VisitedBefore(obj, w.seen) {
		return nil
	}
	return w.dict(ctx, obj)
}
