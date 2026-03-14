package xref

import "errors"

// Sentinel errors for cross-reference parsing failure states.
var (
	// ErrMalformedXRef indicates the cross-reference table or stream
	// contains structurally invalid data.
	ErrMalformedXRef = errors.New("malformed cross-reference data")
)
