package document

import "errors"

// Sentinel errors for distinct document-level failure states.
var (
	// ErrMissingHeader indicates the file does not begin with a %PDF- header.
	ErrMissingHeader = errors.New("not a valid PDF document (missing %PDF- header)")

	// ErrDamagedXRef indicates the cross-reference table could not be located or parsed.
	ErrDamagedXRef = errors.New("damaged or missing cross-reference table")

	// ErrCircularRef indicates an indirect reference chain loops back on itself.
	ErrCircularRef = errors.New("circular reference detected")

	// ErrObjectNotFound indicates an object number referenced in the xref has no entry.
	ErrObjectNotFound = errors.New("object not found in xref")

	// ErrInternalPanic is the sentinel a recovered panic wraps. The public API
	// boundary returns it, and so does a worker goroutine that recovers its own
	// panic, since the boundary's recover runs on the calling goroutine and
	// never sees one raised on another. pkg/pdf re-exports it as
	// pdf.ErrInternalPanic.
	ErrInternalPanic = errors.New("internal panic recovered while processing the document")
)
