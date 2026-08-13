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
