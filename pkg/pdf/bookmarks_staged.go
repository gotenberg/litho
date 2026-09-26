package pdf

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/bookmarks"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// stagedHeadings detects headings in staged, the pages the Writer holds as the
// edits before [Writer.GenerateBookmarks] leave its page tree, and returns the
// bookmarks numbered as staged numbers them. readerPages are the Reader's
// pages.
//
// A page of the Reader is read as the Reader holds it, as
// [Reader.GenerateBookmarks] reads it. A page an insertion staged exists only
// in alloc, so it is written to a document of its own and read from there
// (see [Writer.insertedPageElements]). A heading the Reader's structure tree
// places on a page a deletion removed is dropped with that page: its title
// would quote the page, and its destination would bring it back.
//
// # Corpus evidence
//
//   - All corpora: of 116 files sampled across the seven, each given its first
//     page again as page 2 before bookmarks were generated, 36 now carry a
//     bookmark on the inserted page, and the outlines hold 285 items where
//     they held 231.
func (w *Writer) stagedHeadings(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	readerPages, staged []*document.Page,
	opts BookmarkGenerationOptions,
) ([]models.Bookmark, error) {
	readerNum := make(map[*document.Page]int, len(readerPages))
	for i, page := range readerPages {
		readerNum[page] = i + 1
	}
	var inserted []*document.Page
	for _, page := range staged {
		if _, held := readerNum[page]; !held {
			inserted = append(inserted, page)
		}
	}
	if len(inserted) == 0 && len(staged) == len(readerPages) {
		// Nothing staged the page tree: the Reader's pages are the Writer's.
		return w.reader.GenerateBookmarks(ctx, opts)
	}

	insertedElements, err := w.insertedPageElements(ctx, alloc, doc, inserted)
	if err != nil {
		return nil, err
	}

	input := bookmarks.Input{
		Doc:          w.reader.doc,
		PageRefToNum: make(map[ast.PdfIndirectRef]int, len(readerPages)),
		Pages:        make([]bookmarks.PageTextElements, 0, len(staged)),
	}
	kept := make(map[*document.Page]bool, len(staged))
	next := 0
	for i, page := range staged {
		num := i + 1
		var elements []models.TextElement
		if rn, held := readerNum[page]; held {
			kept[page] = true
			input.PageRefToNum[page.Ref] = num
			elements, err = w.reader.ExtractTextElements(ctx, rn)
		} else {
			elements, err = insertedElements[next], nil
			next++
		}
		if err != nil || elements == nil {
			// A page that fails to extract costs its headings, not the
			// outline, as it does in [Reader.GenerateBookmarks].
			continue
		}
		input.Pages = append(input.Pages, bookmarks.PageTextElements{PageNum: num, Elements: elements})
	}
	// A deleted page keeps a number past the last page, so the headings the
	// structure tree places on it can be told apart and dropped.
	removed := len(staged)
	for _, page := range readerPages {
		if !kept[page] {
			removed++
			input.PageRefToNum[page.Ref] = removed
		}
	}

	generated, err := bookmarks.Generate(ctx, input, generationToInternal(opts))
	if err != nil {
		return nil, err
	}
	return bookmarksWithin(generated, len(staged)), nil
}

// bookmarksWithin returns bms without the bookmarks naming a page past last,
// each dropped bookmark's children taking its place. The generator bounds the
// depth it recurses over.
func bookmarksWithin(bms []models.Bookmark, last int) []models.Bookmark {
	out := make([]models.Bookmark, 0, len(bms))
	for _, b := range bms {
		children := bookmarksWithin(b.Children, last)
		if b.PageNumber > last {
			out = append(out, children...)
			continue
		}
		b.Children = children
		out = append(out, b)
	}
	return out
}

// renumberBookmarks returns bms, whose page numbers name readerPages, with the
// numbers staged gives the same pages. A bookmark naming a page staged does not
// hold gets page 0 and so no destination, as [mutations.SetBookmarks] leaves
// one naming a deleted page. The outline reader bounds the depth it recurses
// over.
func renumberBookmarks(bms []models.Bookmark, readerPages, staged []*document.Page) []models.Bookmark {
	if len(bms) == 0 {
		return bms
	}
	at := make(map[*document.Page]int, len(staged))
	for i, page := range staged {
		at[page] = i + 1
	}
	var renumber func([]models.Bookmark) []models.Bookmark
	renumber = func(in []models.Bookmark) []models.Bookmark {
		out := make([]models.Bookmark, len(in))
		for i, b := range in {
			num := 0
			if b.PageNumber >= 1 && b.PageNumber <= len(readerPages) {
				num = at[readerPages[b.PageNumber-1]]
			}
			b.PageNumber = num
			b.Children = renumber(b.Children)
			out[i] = b
		}
		return out
	}
	return renumber(bms)
}

// insertedPageElements returns the text elements of each of pages, pages an
// insertion staged in alloc, in order, nil for a page that fails to extract.
//
// The objects such a page names are copies in alloc, at numbers doc does not
// define, so the Reader cannot read them. The pages are written to a document
// of their own in a temp file (see [mutations.WriteStagedPages]) and read back
// from it, with the reading configuration of the Reader.
func (w *Writer) insertedPageElements(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page) ([][]models.TextElement, error) {
	if len(pages) == 0 {
		return nil, nil
	}
	f, err := w.createTemp("litho-inserted-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("creating a temp file for the inserted pages: %w", err)
	}
	path := f.Name()
	logger := w.reader.doc.Logger
	defer removeTempFile(logger, path)
	buffered := bufio.NewWriterSize(f, writeBufferSize)
	writeErr := mutations.WriteStagedPages(ctx, buffered, alloc, doc, pages)
	if writeErr == nil {
		writeErr = buffered.Flush()
	}
	if closeErr := f.Close(); closeErr != nil && writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return nil, fmt.Errorf("writing the inserted pages to read them: %w", writeErr)
	}

	r, err := w.reader.reopenFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("reopening the inserted pages: %w", err)
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			logger.Debug("writer: closing the inserted pages failed", slog.Any("err", closeErr))
		}
	}()
	n, err := r.PageCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting the inserted pages: %w", err)
	}
	if n != len(pages) {
		return nil, fmt.Errorf("the %d inserted pages read back as %d", len(pages), n)
	}
	out := make([][]models.TextElement, len(pages))
	for i := range pages {
		elements, err := r.ExtractTextElements(ctx, i+1)
		if err != nil {
			logger.Debug("writer: extracting an inserted page's text failed",
				slog.Int("page", i+1), slog.Any("err", err))
			continue
		}
		out[i] = elements
	}
	return out, nil
}
