package plaintext

import "errors"

// Sentinel errors [Convert] wraps. Match them with errors.Is.
var (
	// ErrInvalidOptions is returned for [Options] out of range: a size that
	// is not finite and positive, a page past the PDF size limit, margins
	// that leave no room, or a cap below zero or above its ceiling.
	ErrInvalidOptions = errors.New("invalid plain text conversion options")
	// ErrInputTooLarge is returned for a source longer than
	// [Options.MaxInputBytes]. Nothing is laid out.
	ErrInputTooLarge = errors.New("plain text source exceeds the input limit")
	// ErrTooManyPages is returned when the layout needs more than
	// [Options.MaxPages] pages.
	ErrTooManyPages = errors.New("plain text layout exceeds the page limit")
)
