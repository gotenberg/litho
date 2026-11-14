package colorfont

import "errors"

var (
	// ErrInvalidFont reports a colour table whose structure is broken: an
	// offset or a count that runs past its table.
	ErrInvalidFont = errors.New("invalid colour font table")

	// ErrNoColor reports a program that holds no colour table this
	// package reads.
	ErrNoColor = errors.New("no colour glyph table")
)
