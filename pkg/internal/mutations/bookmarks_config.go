package mutations

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/bookmarks"
	"github.com/gotenberg/litho/pkg/models"
)

// TextElementProvider returns the text elements of a single page in a
// specific source. sourceIdx is 0 for [Split] and 0..N-1 for [Merge].
type TextElementProvider func(ctx context.Context, sourceIdx, pageNum int) ([]models.TextElement, error)

// BookmarkConfig is the internal config consumed by [Split] and [Merge].
type BookmarkConfig struct {
	Handling              models.BookmarkHandling
	OrphanPolicy          models.OrphanPolicy
	PromoteOrphanChildren bool
	WrapEachSource        bool
	SourceLabels          []string
	Generation            bookmarks.Options
	TextElements          TextElementProvider
}

// DefaultSplitBookmarkConfig returns the default config for [Split].
func DefaultSplitBookmarkConfig() BookmarkConfig {
	return BookmarkConfig{
		Handling:              models.BookmarkUpdate,
		OrphanPolicy:          models.OrphanDrop,
		PromoteOrphanChildren: true,
	}
}

// DefaultMergeBookmarkConfig returns the default config for [Merge].
func DefaultMergeBookmarkConfig() BookmarkConfig {
	return BookmarkConfig{
		Handling: models.BookmarkUpdate,
	}
}
