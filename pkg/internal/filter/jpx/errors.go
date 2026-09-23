package jpx

import "errors"

// Sentinel errors for JPEG 2000 decoding failures.
var (
	// ErrMalformed indicates data that breaks the syntax of ITU-T T.800.
	ErrMalformed = errors.New("malformed JPEG 2000 data")

	// ErrUnsupported indicates a construct T.800 defines that this decoder
	// does not implement.
	ErrUnsupported = errors.New("unsupported JPEG 2000 construct")

	// ErrLimit indicates data that exceeds one of this package's anti-DoS
	// limits.
	ErrLimit = errors.New("JPEG 2000 data exceeds a decoding limit")
)
