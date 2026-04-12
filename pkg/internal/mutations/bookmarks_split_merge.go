package mutations

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/bookmarks"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

// resolveSplitBookmarks computes the final bookmark tree to write for a Split
// operation. The keptPages slice contains the source page numbers in the
// order they were kept.
func resolveSplitBookmarks(
	ctx context.Context,
	doc *document.Document,
	allPages []*document.Page,
	keptPages []int,
	cfg BookmarkConfig,
) ([]models.Bookmark, error) {
	if cfg.Handling == BookmarkNone {
		return nil, nil
	}

	// Build oldPage → newPage mapping (1-based positions in keptPages).
	oldToNew := make(map[int]int, len(keptPages))
	for i, p := range keptPages {
		// First occurrence wins (duplicates rare in split).
		if _, exists := oldToNew[p]; !exists {
			oldToNew[p] = i + 1
		}
	}
	newPageNum := func(old int) int { return oldToNew[old] }

	switch cfg.Handling {
	case BookmarkUpdate:
		existing, err := semantics.ResolveOutline(ctx, doc)
		if err != nil {
			doc.Logger.Debug("reading source outline for split failed",
				slog.Any("err", err),
			)
		}
		return FilterAndRemap(existing, newPageNum, cfg.OrphanPolicy, cfg.PromoteOrphanChildren, keptPages), nil

	case BookmarkReconstruct:
		return reconstructFromKeptPages(ctx, doc, allPages, keptPages, cfg)

	case BookmarkMixed:
		existing, err := semantics.ResolveOutline(ctx, doc)
		if err != nil {
			doc.Logger.Debug("reading source outline for mixed split failed",
				slog.Any("err", err),
			)
		}
		if len(existing) > 0 {
			return FilterAndRemap(existing, newPageNum, cfg.OrphanPolicy, cfg.PromoteOrphanChildren, keptPages), nil
		}
		return reconstructFromKeptPages(ctx, doc, allPages, keptPages, cfg)
	case BookmarkNone:
		return nil, nil
	}
	return nil, nil
}

// reconstructFromKeptPages auto-generates bookmarks from the kept pages of a
// single source.
func reconstructFromKeptPages(
	ctx context.Context,
	doc *document.Document,
	allPages []*document.Page,
	keptPages []int,
	cfg BookmarkConfig,
) ([]models.Bookmark, error) {
	if cfg.TextElements == nil {
		return nil, nil
	}

	pageRefToNum := make(map[ast.PdfIndirectRef]int, len(keptPages))
	pageTexts := make([]bookmarks.PageTextElements, 0, len(keptPages))

	for newIdx, oldPage := range keptPages {
		newPageNum := newIdx + 1
		if oldPage < 1 || oldPage > len(allPages) {
			doc.Logger.Debug("skipping out-of-range kept page during reconstruct",
				slog.Int("page", oldPage),
				slog.Int("total", len(allPages)),
			)
			continue
		}
		srcPage := allPages[oldPage-1]
		pageRefToNum[srcPage.Ref] = newPageNum

		elements, err := cfg.TextElements(ctx, 0, oldPage)
		if err != nil {
			doc.Logger.Debug("extracting text elements for reconstruct failed",
				slog.Int("page", oldPage),
				slog.Any("err", err),
			)
			continue
		}
		pageTexts = append(pageTexts, bookmarks.PageTextElements{
			PageNum:  newPageNum,
			Elements: elements,
		})
	}

	return bookmarks.Generate(ctx, bookmarks.Input{
		Doc:          doc,
		PageRefToNum: pageRefToNum,
		Pages:        pageTexts,
	}, cfg.Generation)
}

// resolveMergeBookmarks computes the final bookmark tree for a Merge
// operation. cumulativeOffsets[i] is the page count BEFORE source i.
func resolveMergeBookmarks(
	ctx context.Context,
	sources []MergeSource,
	cumulativeOffsets []int,
	cfg BookmarkConfig,
) ([]models.Bookmark, error) {
	if cfg.Handling == BookmarkNone {
		return nil, nil
	}

	perSource := make([][]models.Bookmark, len(sources))
	firstPageOfSource := make([]int, len(sources))

	for i, src := range sources {
		offset := cumulativeOffsets[i]
		firstPageOfSource[i] = offset + 1

		switch cfg.Handling {
		case BookmarkUpdate:
			existing, err := semantics.ResolveOutline(ctx, src.Doc)
			if err != nil {
				src.Doc.Logger.Debug("reading source outline for merge failed",
					slog.Int("source", i),
					slog.Any("err", err),
				)
			}
			perSource[i] = OffsetPages(existing, offset)

		case BookmarkReconstruct:
			generated, err := generateForMergeSource(ctx, src, i, offset, cfg)
			if err != nil {
				src.Doc.Logger.Debug("generating bookmarks for merge source failed",
					slog.Int("source", i),
					slog.Any("err", err),
				)
			}
			perSource[i] = generated

		case BookmarkMixed:
			existing, err := semantics.ResolveOutline(ctx, src.Doc)
			if err != nil {
				src.Doc.Logger.Debug("reading source outline for mixed merge failed",
					slog.Int("source", i),
					slog.Any("err", err),
				)
			}
			if len(existing) > 0 {
				perSource[i] = OffsetPages(existing, offset)
			} else {
				generated, genErr := generateForMergeSource(ctx, src, i, offset, cfg)
				if genErr != nil {
					src.Doc.Logger.Debug("mixed-fallback reconstruct failed",
						slog.Int("source", i),
						slog.Any("err", genErr),
					)
				}
				perSource[i] = generated
			}
		case BookmarkNone:
			// nothing
		}
	}

	// Apply per-source wrapping if requested.
	if cfg.WrapEachSource {
		var wrapped []models.Bookmark
		for i, bms := range perSource {
			label := DefaultSourceLabel(i)
			if i < len(cfg.SourceLabels) && cfg.SourceLabels[i] != "" {
				label = cfg.SourceLabels[i]
			}
			wrapped = append(wrapped, WrapInRoot(bms, label, firstPageOfSource[i]))
		}
		return wrapped, nil
	}

	// Otherwise concatenate.
	var all []models.Bookmark
	for _, bms := range perSource {
		all = append(all, bms...)
	}
	return all, nil
}

// generateForMergeSource auto-generates bookmarks for one source in a Merge
// operation, with page numbers offset by the cumulative count.
func generateForMergeSource(
	ctx context.Context,
	src MergeSource,
	sourceIdx int,
	offset int,
	cfg BookmarkConfig,
) ([]models.Bookmark, error) {
	if cfg.TextElements == nil {
		return nil, nil
	}

	pageRefToNum := make(map[ast.PdfIndirectRef]int, len(src.Pages))
	pageTexts := make([]bookmarks.PageTextElements, 0, len(src.Pages))

	for i, p := range src.Pages {
		newPageNum := offset + i + 1
		pageRefToNum[p.Ref] = newPageNum

		elements, err := cfg.TextElements(ctx, sourceIdx, i+1)
		if err != nil {
			src.Doc.Logger.Debug("extracting text elements for merge source failed",
				slog.Int("source", sourceIdx),
				slog.Int("page", i+1),
				slog.Any("err", err),
			)
			continue
		}
		pageTexts = append(pageTexts, bookmarks.PageTextElements{
			PageNum:  newPageNum,
			Elements: elements,
		})
	}

	return bookmarks.Generate(ctx, bookmarks.Input{
		Doc:          src.Doc,
		PageRefToNum: pageRefToNum,
		Pages:        pageTexts,
	}, cfg.Generation)
}
