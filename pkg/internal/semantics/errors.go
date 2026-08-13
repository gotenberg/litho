package semantics

import "errors"

// ErrNoMetadata indicates the document carries no document-level metadata: no
// /Info dictionary, no XMP packet, no /Info fields recoverable from the raw
// bytes, and no catalog /Lang.
//
// Absence is not a defect. Callers that must tell "absent" from "extraction
// failed" match it with [errors.Is].
var ErrNoMetadata = errors.New("no metadata found (neither /Info dictionary nor XMP stream)")
