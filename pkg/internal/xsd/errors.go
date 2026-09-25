package xsd

import "errors"

// Sentinel errors returned by the loader. Wrap via [fmt.Errorf]
// with %w when annotating with the offending file or symbol so
// callers can keep using [errors.Is].
var (
	// ErrSchemaParse signals that an XSD document is not well-formed
	// XML or violates the structural shape the loader expects (missing
	// xs:schema root, etc.).
	ErrSchemaParse = errors.New("xsd: schema parse failed")

	// ErrUnresolvedRef signals that an element or type reference
	// could not be resolved against the loaded schema set after the
	// link pass. The wrapped error carries the missing QName.
	ErrUnresolvedRef = errors.New("xsd: unresolved reference")

	// ErrUnsupportedConstruct signals that the loader encountered an
	// XSD construct outside the documented CII subset (e.g.
	// xs:substitutionGroup). Returned eagerly so future CII schema
	// updates that lean on a new construct surface at load time
	// rather than as silent validation gaps later.
	ErrUnsupportedConstruct = errors.New("xsd: unsupported construct")

	// ErrEncoding signals that an instance document names or uses a
	// character encoding the decoder [NewDecoder] builds cannot read.
	// [Validate] returns it wrapped as its parse error.
	ErrEncoding = errors.New("xsd: unreadable character encoding")
)
