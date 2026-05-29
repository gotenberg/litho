package cff

import "errors"

// Sentinel errors for CFF font parsing.
var (
	// ErrInvalidCFF indicates structurally invalid CFF font data.
	ErrInvalidCFF = errors.New("cff: invalid font data")
	// ErrTruncated indicates the CFF data ended before a structure was
	// fully read.
	ErrTruncated = errors.New("cff: data truncated")
)
