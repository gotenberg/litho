package office

import "errors"

// The errors the Office converters return beside limits.ErrInputTooLarge,
// limits.ErrTooManyPages and typeset.ErrInvalidOptions, which every
// converter shares.
var (
	// ErrMalformed reports a document the converter cannot read: not a
	// package of its format, a required part missing or unparsable.
	ErrMalformed = errors.New("malformed document")
	// ErrEncrypted reports a document that needs a password Options.Password
	// does not give, or one encrypted with a scheme the converter does not
	// read.
	ErrEncrypted = errors.New("encrypted document")
)
