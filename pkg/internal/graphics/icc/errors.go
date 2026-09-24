package icc

import "errors"

// Sentinel errors for ICC profile failures.
var (
	// ErrMalformed indicates a profile that breaks the syntax of ICC.1.
	ErrMalformed = errors.New("malformed ICC profile")

	// ErrUnsupported indicates a profile construct this package does not
	// transform.
	ErrUnsupported = errors.New("unsupported ICC profile construct")

	// ErrLimit indicates a profile that exceeds one of this package's
	// anti-DoS limits.
	ErrLimit = errors.New("ICC profile exceeds a decoding limit")
)
