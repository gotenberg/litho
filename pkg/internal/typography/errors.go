package typography

import "errors"

// Sentinel errors for model-level failure states.
var (
	// ErrFontNotFound indicates a font resource referenced by a content
	// stream could not be located in the page's resource dictionary.
	ErrFontNotFound = errors.New("font resource not found in page dictionary")
)
