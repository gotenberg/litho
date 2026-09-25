package ccitt

import "errors"

// Sentinel errors for CCITT facsimile decoding failures.
var (
	// ErrMalformed indicates data that breaks the coding rules of ITU-T T.4
	// or T.6.
	ErrMalformed = errors.New("malformed CCITT facsimile data")

	// ErrUnsupported indicates a coding T.4 or T.6 defines that this decoder
	// does not implement: uncompressed mode.
	ErrUnsupported = errors.New("unsupported CCITT facsimile coding")

	// ErrPartial indicates rows that stop before a row in a coding this
	// decoder does not implement, which [Decode] returns alongside it: the
	// data goes on, and the rows after that one are unread.
	ErrPartial = errors.New("CCITT facsimile data holds rows the decoder cannot read")

	// ErrLimit indicates data that exceeds one of this package's anti-DoS
	// limits.
	ErrLimit = errors.New("CCITT facsimile data exceeds a decoding limit")
)
