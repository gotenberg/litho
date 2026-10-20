package cff

import "errors"

// Sentinel errors for CFF font parsing.
var (
	// ErrInvalidCFF indicates structurally invalid CFF font data.
	ErrInvalidCFF = errors.New("cff: invalid font data")
	// ErrTruncated indicates the CFF data ended before a structure was
	// fully read.
	ErrTruncated = errors.New("cff: data truncated")
	// ErrSubsetUnsupported indicates a program [Subset] does not cut down:
	// a CID-keyed one, or one whose charstrings are not Type 2.
	ErrSubsetUnsupported = errors.New("cff: program cannot be subset")
	// ErrInvalidCharstring indicates a Type 2 charstring that cannot be
	// interpreted to an outline.
	ErrInvalidCharstring = errors.New("cff: invalid charstring")
)
