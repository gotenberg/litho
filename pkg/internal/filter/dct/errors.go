package dct

import "errors"

// Sentinel errors for JPEG decoding failures.
var (
	// ErrMalformed indicates data that breaks the syntax of ITU-T T.81.
	ErrMalformed = errors.New("malformed JPEG data")

	// ErrUnsupported indicates a coding T.81 defines that this decoder does
	// not implement: lossless, arithmetic or hierarchical coding, or samples
	// other than 8 bits.
	ErrUnsupported = errors.New("unsupported JPEG coding")

	// ErrLimit indicates data that exceeds one of this package's anti-DoS
	// limits.
	ErrLimit = errors.New("JPEG data exceeds a decoding limit")
)
