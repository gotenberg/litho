package mutations

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/bookmarks"
	"github.com/gotenberg/litho/pkg/models"
)

// BookmarkHandling controls how bookmarks are produced for the output of
// Split or Merge.
type BookmarkHandling int

const (
	// BookmarkUpdate keeps existing bookmarks and remaps page indexes.
	// This is the default.
	BookmarkUpdate BookmarkHandling = iota
	// BookmarkReconstruct ignores existing bookmarks and auto-generates
	// from the output content.
	BookmarkReconstruct
	// BookmarkMixed auto-generates bookmarks for sources that have none,
	// and updates existing bookmarks for the others.
	BookmarkMixed
	// BookmarkNone produces no outline.
	BookmarkNone
)

// TextElementProvider returns the text elements of a single page in a
// specific source. sourceIdx is 0 for Split and 0..N-1 for Merge.
type TextElementProvider func(ctx context.Context, sourceIdx, pageNum int) ([]models.TextElement, error)

// BookmarkConfig is the internal config consumed by Split and Merge.
type BookmarkConfig struct {
	Handling              BookmarkHandling
	OrphanPolicy          OrphanHandling
	PromoteOrphanChildren bool
	WrapEachSource        bool
	SourceLabels          []string
	Generation            bookmarks.Options
	TextElements          TextElementProvider
}

// DefaultSplitBookmarkConfig returns the default config for Split.
func DefaultSplitBookmarkConfig() BookmarkConfig {
	return BookmarkConfig{
		Handling:              BookmarkUpdate,
		OrphanPolicy:          OrphanDrop,
		PromoteOrphanChildren: true,
	}
}

// DefaultMergeBookmarkConfig returns the default config for Merge.
func DefaultMergeBookmarkConfig() BookmarkConfig {
	return BookmarkConfig{
		Handling: BookmarkUpdate,
	}
}
