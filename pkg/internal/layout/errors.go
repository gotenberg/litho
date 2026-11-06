package layout

import "errors"

// ErrTooManyPages reports a layout that needs more pages than
// Options.MaxPages allows.
var ErrTooManyPages = errors.New("layout needs more pages than allowed")

// ErrInvalidOptions reports a nil root or font index, or a page box whose
// size or margins are not finite, or leave no positive page area.
var ErrInvalidOptions = errors.New("invalid layout options")

// ErrTooManyBoxes reports a content tree that generates more boxes than
// [MaxBoxes].
var ErrTooManyBoxes = errors.New("layout generates too many boxes")
