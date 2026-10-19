package type1

import "errors"

// Sentinel errors for Type 1 font programs.
var (
	// ErrSubsetUnsupported indicates a program [SubsetInPlace] does not cut
	// down: one it cannot split into its clear-text, encrypted and trailer
	// parts, whose charstrings it cannot read, or one a seac it cannot
	// follow draws from.
	ErrSubsetUnsupported = errors.New("type1: program cannot be subset")
)
