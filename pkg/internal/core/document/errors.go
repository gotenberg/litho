package document

import (
	"errors"
	"fmt"
)

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

	// ErrNoRevision indicates the bytes before an offset do not end in a
	// revision that can be read: no cross-reference section and trailer end
	// them (ISO 32000-1, 7.5.6).
	ErrNoRevision = errors.New("no revision ends at this offset")

	// ErrInternalPanic is the sentinel a recovered panic wraps. The public API
	// boundary returns it, and so does a worker goroutine that recovers its own
	// panic, since the boundary's recover runs on the calling goroutine and
	// never sees one raised on another. pkg/pdf re-exports it as
	// pdf.ErrInternalPanic.
	ErrInternalPanic = errors.New("internal panic recovered while processing the document")
)

// Sentinel classes of the raw-byte format violations the document's checks
// report. Each violation reads its own message and matches its class under
// [errors.Is].
var (
	// ErrObjectHeaderFormat marks a format violation in the white space and
	// EOL markers around an indirect object's header or its endobj keyword
	// (ISO 32000-1, 7.3.10; ISO 19005-2, 6.1.9 t1).
	ErrObjectHeaderFormat = errors.New("object header format violation")

	// ErrXRefFormat is the class of the cross-reference table format
	// violations: [ErrXRefEOLMarkers] and [ErrXRefSubsectionSpacing] each
	// match it under [errors.Is]. ISO 32000-1, 7.5.4 states the table syntax
	// they check.
	ErrXRefFormat = errors.New("cross-reference table format violation")

	// ErrXRefEOLMarkers marks a cross-reference table whose xref keyword is
	// not followed by a single EOL marker, the requirement ISO 19005-1, 6.1.4
	// t2 and ISO 19005-2, 6.1.4 t2 state.
	ErrXRefEOLMarkers = fmt.Errorf("%w: EOL markers after the xref keyword", ErrXRefFormat)

	// ErrXRefSubsectionSpacing marks a cross-reference table whose subsection
	// header is not the two numbers separated by a single space, or is set
	// apart from the xref keyword by extra white space. ISO 19005-1, 6.1.4 t1
	// states the requirement, and later parts dropped it.
	ErrXRefSubsectionSpacing = fmt.Errorf("%w: subsection header spacing", ErrXRefFormat)
)
