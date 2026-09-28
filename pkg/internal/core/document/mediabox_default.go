package document

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// maxExactIntegerBox bounds the page extent written back as an integer: a
// float64 holds every integer up to 2^53 exactly, so a width or height past it
// is written as a real. Anti-overflow guard for the conversion, not
// spec-mandated: ISO 32000-1, Annex C.2 caps a real page at 14,400 units.
const maxExactIntegerBox = 1 << 53

// pageSize is the width and height of a media box, in default user space
// units.
type pageSize struct {
	width, height float64
}

// MediaBoxCandidates returns the media boxes a write may give a page that
// neither states nor inherits one a viewer uses (see [UsableBox]), in the
// order it tries them: [0 0 width height] for each width and height the pages
// of docs have, each page's media box as a viewer reads it, the size the most
// pages have first, a tie going to the size the earliest page has, the pages
// taken document by document and in page order. Each size comes in its own
// orientation, then turned a quarter. It returns none when no page of docs has
// a media box a viewer uses.
//
// ISO 32000-1, 7.7.3.3, Table 30 makes /MediaBox required and gives it no
// default, so a page with none describes no size at all. A viewer draws it on
// US Letter (see [LetterMediaBox]), and [Page.ViewedMediaBox] keeps that
// reading, since it is what the source shows. A write that gives the page a
// media box of its own chooses one instead, and the sizes the rest of the
// document uses are the likeliest ones the producer meant: a page missing its
// media box among A4 pages was an A4 page. The output then shows the page at a
// size a viewer did not show for the source, which [MediaBoxDefault] allows
// only when what the page paints still shows whole. A candidate keeps its
// origin at 0 0, the one a producer that states a media box ordinarily writes,
// since the missing box states no origin to keep.
//
// A document whose pages cannot be read gives no size: its page tree is what
// the write repairs, and the other documents still size the page.
//
// # Corpus evidence
//
//   - All corpora: 9 of 5240 files hold a page with no media box a viewer
//     uses. The size most of their pages have is US Letter in 6, 200 by 200
//     in litho baretree_2b.pdf, 600 by 800 in litho viewerboxes_2b.pdf, and
//     800 by 600 in pdfjs boundingBox_invalid.pdf, a tie with 600 by 800 the
//     earlier page wins.
//   - litho defaultbox_2b.pdf: pages missing their media box among A4 ones,
//     the case no other corpus file reaches.
func MediaBoxCandidates(ctx context.Context, docs []*Document) ([]ast.PdfArray, error) {
	counts := make(map[pageSize]int)
	var order []pageSize
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		pages, err := defaultBoxPages(ctx, doc)
		if err != nil {
			return nil, err
		}
		for i, page := range pages {
			if i%mediaBoxVoteCheckInterval == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			if !UsableBox(page.MediaBox) {
				continue
			}
			x0, y0, x1, y1 := boxCorners(page.MediaBox)
			size := pageSize{width: x1 - x0, height: y1 - y0}
			if counts[size] == 0 {
				order = append(order, size)
			}
			counts[size]++
		}
	}
	// A stable sort keeps a tie in first-page order.
	slices.SortStableFunc(order, func(a, b pageSize) int { return counts[b] - counts[a] })
	seen := make(map[pageSize]bool, 2*len(order))
	candidates := make([]ast.PdfArray, 0, 2*len(order))
	for _, size := range order {
		for _, s := range []pageSize{size, {width: size.height, height: size.width}} {
			if seen[s] {
				continue
			}
			seen[s] = true
			candidates = append(candidates, ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), boxExtent(s.width), boxExtent(s.height)})
		}
	}
	return candidates, nil
}

// mediaBoxVoteCheckInterval is how many pages [MediaBoxCandidates] reads
// between two checks of its context. Not spec-mandated: a page costs a map
// update, so the check is cheap next to the walk that produced the pages.
const mediaBoxVoteCheckInterval = 100

// defaultBoxPages returns the pages of doc as the reader reads them, none when
// its catalog or page tree cannot be read. Only a cancelled ctx is an error.
func defaultBoxPages(ctx context.Context, doc *Document) ([]*Page, error) {
	catalog, err := ResolveCatalog(ctx, doc)
	if err == nil {
		var pages []*Page
		pages, err = ResolvePages(ctx, doc, catalog)
		if err == nil {
			return pages, nil
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	doc.Logger.Debug("no page sizes to derive a default media box from", "error", err)
	return nil, nil
}

// boxExtent returns v, a page extent, as the number a media box writes: an
// integer when it is a whole number that converts exactly, a real otherwise.
func boxExtent(v float64) ast.PdfObject {
	if v == math.Trunc(v) && v < maxExactIntegerBox {
		return ast.PdfInt(int64(v))
	}
	return ast.PdfReal(v)
}

// ContentExtent is the region a page paints within the region a viewer shows
// of it, in default user space: the rectangle a media box has to hold for the
// page to show everything it showed.
type ContentExtent struct {
	// Known reports whether the page was measured. A page whose content
	// could not be read whole, an XObject that does not resolve, text in a
	// font that does not, a shading painting the clip, is not, and no size
	// but the one a viewer showed holds it for sure.
	Known bool
	// Painted reports whether the page paints anything a viewer shows. A
	// known extent that paints nothing fits every size.
	Painted bool
	// Box is the painted region, lower-left then upper-right corner, when
	// Painted.
	Box [4]float64
}

// extentTolerance is how far, in default user space units, a painted region
// may pass a candidate media box and still count as inside it: the rounding
// of an A4 size written to two decimals. Not spec-mandated.
const extentTolerance = 0.01

// FitsIn reports whether box, a rectangle of four numbers, holds the painted
// region whole. An unknown extent fits nothing.
func (e ContentExtent) FitsIn(box ast.PdfArray) bool {
	if !e.Known {
		return false
	}
	if !e.Painted {
		return true
	}
	if !UsableBox(box) {
		return false
	}
	x0, y0, x1, y1 := boxCorners(box)
	return e.Box[0] >= x0-extentTolerance && e.Box[1] >= y0-extentTolerance &&
		e.Box[2] <= x1+extentTolerance && e.Box[3] <= y1+extentTolerance
}

// MediaBoxDefault chooses, for one write, the media box of each page that
// neither states nor inherits one a viewer uses: the first of
// [MediaBoxCandidates] of Docs, then Fallback, that holds what the page paints
// whole (see [ContentExtent]), and US Letter, the page a viewer drew, when
// none does. A write never shrinks what a viewer showed of a page. The
// candidates are read when the first such page needs them and kept for the
// others, so a write that needs none reads no page tree for them. A nil
// MediaBoxDefault gives US Letter.
//
// A MediaBoxDefault is not safe for concurrent use: one write resolves it, on
// the goroutine that writes.
type MediaBoxDefault struct {
	// Docs are the documents whose pages give the sizes: the one the write
	// reads, then those it takes pages from.
	Docs []*Document
	// Fallback is the size tried after those of Docs, the caller's, nil for
	// none.
	Fallback ast.PdfArray
	// DerivedOnly makes Resolve return nil rather than US Letter when no size
	// of Docs holds the page, for a write that leaves the choice to a later
	// one that knows the caller's fallback. Fallback is not tried.
	DerivedOnly bool

	candidates []ast.PdfArray
	read       bool
}

// Resolve returns the media box a page painting extent gets, a fresh array
// each call.
func (m *MediaBoxDefault) Resolve(ctx context.Context, extent ContentExtent) (ast.PdfArray, error) {
	if m == nil {
		return LetterMediaBox(), nil
	}
	if !m.read {
		candidates, err := MediaBoxCandidates(ctx, m.Docs)
		if err != nil {
			return nil, fmt.Errorf("reading the page sizes of the document: %w", err)
		}
		if m.Fallback != nil && !m.DerivedOnly {
			candidates = append(candidates, m.Fallback)
		}
		m.candidates, m.read = candidates, true
	}
	for _, box := range m.candidates {
		if extent.FitsIn(box) {
			return slices.Clone(box), nil
		}
	}
	if m.DerivedOnly {
		return nil, nil
	}
	return LetterMediaBox(), nil
}
