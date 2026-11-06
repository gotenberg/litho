package html

import "errors"

// Sentinel errors [Convert] wraps. Match them with errors.Is.
var (
	// ErrInvalidOptions is returned for [Options] out of range: a page size
	// that is not finite and positive or past the PDF size limit, margins
	// that leave no room, an unknown media type, a cap below zero or above
	// its ceiling, or a nil font index.
	ErrInvalidOptions = errors.New("invalid HTML conversion options")
	// ErrInputTooLarge is returned for a source longer than
	// [Options.MaxInputBytes]. Nothing is parsed.
	ErrInputTooLarge = errors.New("HTML source exceeds the input limit")
	// ErrTooManyElements is returned for a document of more than
	// [MaxElements] elements. Nothing is laid out.
	ErrTooManyElements = errors.New("HTML document exceeds the element limit")
	// ErrTooDeep is returned for a document that nests elements deeper
	// than the HTML parser follows, 512. Nothing is laid out.
	ErrTooDeep = errors.New("HTML document nests elements too deeply")
	// ErrTooManyPages is returned when the layout needs more than
	// [Options.MaxPages] pages.
	ErrTooManyPages = errors.New("HTML layout exceeds the page limit")
)
