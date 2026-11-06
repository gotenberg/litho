package text

import "errors"

// Sentinel errors returned by [Fonts.Layout].
var (
	// ErrInvalidWidth reports a line width that is not finite and positive.
	ErrInvalidWidth = errors.New("line width is not finite and positive")
	// ErrInvalidStyle reports a [Style] whose size or spacing is not finite
	// or lies outside its documented range.
	ErrInvalidStyle = errors.New("invalid text style")
	// ErrTooManyRuns reports a Layout call with more runs than
	// [MaxLayoutRuns].
	ErrTooManyRuns = errors.New("too many runs in one layout")
	// ErrTextTooLong reports a Layout call whose runs hold more characters
	// than [MaxLayoutRunes].
	ErrTextTooLong = errors.New("too much text in one layout")
	// ErrNoFonts reports a Layout call on a [Fonts] that indexed no face.
	ErrNoFonts = errors.New("no font face indexed")
)
