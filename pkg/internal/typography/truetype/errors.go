package truetype

import "errors"

var (
	// ErrInvalidFont indicates the font data is structurally invalid.
	ErrInvalidFont = errors.New("invalid TrueType/OpenType font")

	// ErrTableNotFound indicates a required table was not found.
	ErrTableNotFound = errors.New("font table not found")
)
