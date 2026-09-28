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

// DefaultMediaBox returns the media box a write gives a page that neither
// states nor inherits one a viewer uses (see [UsableBox]): [0 0 width height]
// for the width and height the most pages of docs have, each page's media box
// as a viewer reads it, the pages taken document by document and in page
// order, a tie going to the size the earliest of them has. When no page of
// docs has a media box a viewer uses, it returns fallback, or
// [LetterMediaBox] when fallback is nil. The array is a fresh one each call,
// so a caller may write it as an entry of its own.
//
// ISO 32000-1, 7.7.3.3, Table 30 makes /MediaBox required and gives it no
// default, so a page with none describes no size at all. A viewer draws it on
// US Letter (see [LetterMediaBox]), and [Page.ViewedMediaBox] keeps that
// reading, since it is what the source shows. A write that gives the page a
// media box of its own chooses one instead, and the size the rest of the
// document uses is the likeliest one the producer meant: a page missing its
// media box among A4 pages was an A4 page. The output then shows the page at a
// size a viewer did not show for the source. The replacement keeps its origin
// at 0 0, the one a producer that states a media box ordinarily writes, since
// the missing box states no origin to keep.
//
// A document whose pages cannot be read gives no size: its page tree is what
// the write repairs, and the other documents, or fallback, still size the
// page.
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
func DefaultMediaBox(ctx context.Context, docs []*Document, fallback ast.PdfArray) (ast.PdfArray, error) {
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
	if len(order) == 0 {
		if fallback == nil {
			return LetterMediaBox(), nil
		}
		return slices.Clone(fallback), nil
	}
	best := order[0]
	for _, size := range order[1:] {
		if counts[size] > counts[best] {
			best = size
		}
	}
	return ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), boxExtent(best.width), boxExtent(best.height)}, nil
}

// mediaBoxVoteCheckInterval is how many pages [DefaultMediaBox] reads between
// two checks of its context. Not spec-mandated: a page costs a map update, so
// the check is cheap next to the walk that produced the pages.
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

// MediaBoxDefault is the media box one write gives the pages that neither
// state nor inherit one a viewer uses: [DefaultMediaBox] of Docs and Fallback,
// computed when the first such page needs it and kept for the others, so a
// write that needs none reads no page tree for it. A nil MediaBoxDefault
// resolves to [LetterMediaBox].
//
// A MediaBoxDefault is not safe for concurrent use: one write resolves it, on
// the goroutine that writes.
type MediaBoxDefault struct {
	// Docs are the documents whose pages give the size: the one the write
	// reads, then those it takes pages from.
	Docs []*Document
	// Fallback is the media box when no page of Docs has one a viewer uses,
	// nil for [LetterMediaBox].
	Fallback ast.PdfArray

	box ast.PdfArray
}

// Resolve returns the media box, a fresh array each call.
func (m *MediaBoxDefault) Resolve(ctx context.Context) (ast.PdfArray, error) {
	if m == nil {
		return LetterMediaBox(), nil
	}
	if m.box == nil {
		box, err := DefaultMediaBox(ctx, m.Docs, m.Fallback)
		if err != nil {
			return nil, fmt.Errorf("deriving the default media box: %w", err)
		}
		m.box = box
	}
	return slices.Clone(m.box), nil
}
