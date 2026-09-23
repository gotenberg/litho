package jbig2

import "errors"

// Sentinel errors for JBIG2 decoding failures.
var (
	// ErrMalformed indicates data that breaks the syntax of ITU-T T.88.
	ErrMalformed = errors.New("malformed JBIG2 data")

	// ErrUnsupported indicates a construct T.88 defines that this decoder
	// does not implement.
	ErrUnsupported = errors.New("unsupported JBIG2 construct")

	// ErrLimit indicates data that exceeds one of this package's anti-DoS
	// limits.
	ErrLimit = errors.New("JBIG2 data exceeds a decoding limit")
)
