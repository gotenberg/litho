package text

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Sentinel errors returned by [Fonts.Layout].
var (
	// ErrInvalidWidth reports a line width that is not finite and positive.
	ErrInvalidWidth = errors.New("line width is not finite and positive")
	// ErrInvalidStyle reports a [Style] whose size or spacing is not finite
	// or lies outside its documented range.
	ErrInvalidStyle = errors.New("invalid text style")
	// ErrTooManyRuns reports a Layout call with more runs than
	// [MaxLayoutRuns]. It wraps limits.ErrInputTooLarge: a paragraph past
	// the cap is a document past it.
	ErrTooManyRuns = fmt.Errorf("%w: too many runs in one layout", limits.ErrInputTooLarge)
	// ErrTextTooLong reports a Layout call whose runs hold more characters
	// than [MaxLayoutRunes]. It wraps limits.ErrInputTooLarge.
	ErrTextTooLong = fmt.Errorf("%w: too much text in one layout", limits.ErrInputTooLarge)
	// ErrNoFonts reports a Layout call on a [Fonts] that indexed no face.
	ErrNoFonts = errors.New("no font face indexed")
)
