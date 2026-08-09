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
