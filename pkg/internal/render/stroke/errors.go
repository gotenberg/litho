package stroke

import "errors"

// ErrInvalidStyle reports a stroke parameter outside its domain, such as a
// negative or non-finite line width.
var ErrInvalidStyle = errors.New("invalid stroke style")

// ErrNonFinite reports a point or matrix entry that is NaN or infinite, or an
// outline whose coordinates overflowed.
var ErrNonFinite = errors.New("non-finite stroke geometry")

// ErrTooComplex reports a stroke whose flattened path, dash count or outline
// exceeds an anti-DoS cap.
var ErrTooComplex = errors.New("stroke exceeds the complexity cap")

// ErrMalformedPath reports a path whose steps consume more points than it
// holds.
var ErrMalformedPath = errors.New("malformed path")
