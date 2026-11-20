package models

// BookmarkHandling controls how bookmarks are produced for the output of
// [github.com/gotenberg/litho/pkg/pdf.Split] and
// [github.com/gotenberg/litho/pkg/pdf.Merge]. BookmarkHandling values are
// immutable and safe for concurrent use.
type BookmarkHandling int

const (
	// BookmarkUpdate keeps existing bookmarks and remaps page indexes.
	// Bookmarks pointing to dropped pages are handled per [OrphanPolicy].
	// This is the default.
	BookmarkUpdate BookmarkHandling = iota
	// BookmarkReconstruct ignores existing bookmarks and auto-generates a
	// new outline from the output content via the heading detector.
	BookmarkReconstruct
	// BookmarkMixed uses existing bookmarks when present, otherwise
	// auto-generates per source.
	BookmarkMixed
	// BookmarkNone produces no outline at all.
	BookmarkNone
)

// OrphanPolicy controls what happens to bookmarks whose target page is no
// longer in the output document (Split only).
// OrphanPolicy values are immutable and safe for concurrent use.
type OrphanPolicy int

const (
	// OrphanDrop removes the bookmark.
	OrphanDrop OrphanPolicy = iota
	// OrphanRemap retargets the bookmark to the nearest kept page.
	OrphanRemap
	// OrphanKeep emits the bookmark with no destination so the title
	// remains visible (no clickable target).
	OrphanKeep
)

// BookmarkMergeMode controls how auto-generated bookmarks are combined with
// any existing document outline.
// BookmarkMergeMode values are immutable and safe for concurrent use.
type BookmarkMergeMode int

const (
	// BookmarkReplace replaces the existing outline with the generated
	// bookmarks. This is the default.
	BookmarkReplace BookmarkMergeMode = iota
	// BookmarkPrepend inserts generated bookmarks before any existing entries.
	BookmarkPrepend
	// BookmarkAppend inserts generated bookmarks after any existing entries.
	BookmarkAppend
)
