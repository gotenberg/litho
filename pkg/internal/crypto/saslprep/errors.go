package saslprep

import "errors"

// Sentinel errors for the strings SASLprep rejects.
var (
	// ErrInvalidUTF8 indicates the input is not valid UTF-8, so it names no
	// Unicode string to prepare.
	ErrInvalidUTF8 = errors.New("not valid UTF-8")

	// ErrProhibited indicates the prepared string holds a character RFC 4013,
	// 2.3 prohibits.
	ErrProhibited = errors.New("prohibited character")

	// ErrBidi indicates the prepared string fails the bidirectional checks of
	// RFC 3454, 6.
	ErrBidi = errors.New("bidirectional check failed")

	// ErrUnassigned indicates a stored string holds a code point Unicode 3.2
	// leaves unassigned (RFC 3454, 7 and Table A.1).
	ErrUnassigned = errors.New("unassigned code point")
)
