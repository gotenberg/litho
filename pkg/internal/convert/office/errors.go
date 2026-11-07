package office

import "errors"

// The errors the Office converters return.
var (
	// ErrInvalidOptions reports an option out of its range.
	ErrInvalidOptions = errors.New("office: invalid options")
	// ErrInputTooLarge reports a document past a size cap: its bytes, its
	// package entries, its elements.
	ErrInputTooLarge = errors.New("office: input too large")
	// ErrTooManyPages reports a document that lays out on more pages than
	// Options.MaxPages allows.
	ErrTooManyPages = errors.New("office: too many pages")
	// ErrMalformed reports a document the converter cannot read: not a
	// package of its format, a required part missing or unparsable.
	ErrMalformed = errors.New("office: malformed document")
	// ErrEncrypted reports a document that needs a password Options.Password
	// does not give, or one encrypted with a scheme the converter does not
	// read.
	ErrEncrypted = errors.New("office: encrypted document")
	// ErrNotImplemented reports a converter that is not written yet.
	ErrNotImplemented = errors.New("office: converter not implemented")
)
