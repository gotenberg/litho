package typography

import "errors"

// Sentinel errors for model-level failure states.
var (
	// ErrFontNotFound indicates a font resource referenced by a content
	// stream could not be located in the page's resource dictionary.
	ErrFontNotFound = errors.New("font resource not found in page dictionary")

	// ErrNotStandard14 indicates a name that is not one of the fourteen
	// standard fonts of ISO 32000-1, 9.6.2.2 where one is required.
	ErrNotStandard14 = errors.New("not a standard 14 font name")
)
