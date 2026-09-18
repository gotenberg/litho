package mutations

import "errors"

// ErrPageNotAddressable reports a page that cannot be rewritten in place
// because it has no indirect reference of its own.
//
// A page may be inlined directly in a /Kids array (ISO 32000-1, 7.7.3.2), in
// which case there is no object number for an incremental update to replace.
// Copying mutations handle this by allocating a new object for the page; the
// ones that edit a page where it stands cannot, and report this instead of
// leaving the caller believing the edit landed.
var ErrPageNotAddressable = errors.New("page has no indirect reference")

// ErrCatalogUnreadable reports a catalog that neither the trailer reference nor
// the reader's recovery could produce entries for.
//
// A catalog with no entries is not a catalog: ISO 32000-1, 7.7.2 requires
// /Type and /Pages. Editing one in place would write those zero entries back
// over the real catalog, so the mutation reports this instead.
var ErrCatalogUnreadable = errors.New("document catalog could not be read")

// ErrPageTreeNotReproduced reports a full rewrite whose source catalog named a
// page tree the output could not reproduce.
//
// The rewrite copies the object graph from the catalog down, so a page tree
// litho failed to read is simply absent from the copy. Substituting an empty
// one (ISO 32000-1, 7.7.3.2 requires the catalog to name a page tree) keeps the
// output structurally valid but silently discards every page: `litho attach` on
// an encrypted pdfjs file turned a 47 KB one-page document into a 1.9 KB
// zero-page one and exited 0, while `litho rotate` on the same file failed.
// Report the loss instead, and reserve the empty tree for a catalog that never
// named one.
var ErrPageTreeNotReproduced = errors.New("source page tree could not be reproduced in the rewrite")

// ErrSpanOutOfRange reports a byte span that does not lie within the content it
// addresses.
//
// A span names offsets in one parse of a content stream, so a span reaching
// past the bytes being rewritten means the two have diverged. Excising it would
// cut whatever occupies those offsets now, so the mutation reports the span
// rather than clamping it to fit.
var ErrSpanOutOfRange = errors.New("byte span lies outside the content")

// ErrPartialOperatorCoverage reports a region that covers some but not all of
// what one content-stream operator draws.
//
// An operator is removed whole or not at all, because excision deletes byte
// ranges and cannot rewrite one: dropping a show-text operator whose run only
// partly meets the region would take the rest of its text with it. Splitting
// the run at glyph boundaries is the alternative, and it has to answer the
// glyph-advance leak first (arXiv 2206.02285), where the advances of the
// surviving glyphs still spell the removed ones. Refusing states the problem
// instead of removing more than the caller asked for.
var ErrPartialOperatorCoverage = errors.New("region covers an operator only partly")

// ErrRegionInFormXObject reports a region that selects content a form XObject
// draws.
//
// The glyphs belong to the form's own content stream, so no byte range in the
// page's stream addresses them. Removing the Do that invoked the form would
// erase everything else the form draws, so the mutation reports the region
// rather than reaching for the only span it could name.
var ErrRegionInFormXObject = errors.New("region selects content drawn by a form XObject")

// ErrSpansOverlap reports two byte spans that cover some of the same bytes.
//
// Overlapping spans stop having a single meaning once one of them is removed,
// and merging them quietly would excise bytes no caller selected. Removal has
// to be provable, so the mutation reports the overlap instead.
var ErrSpansOverlap = errors.New("byte spans overlap")
