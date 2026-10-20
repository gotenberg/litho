package glyph

import "errors"

// ErrNotAFont reports an object that is no font dictionary litho can read:
// not a dictionary, or one without a /Subtype (ISO 32000-1, 9.5).
var ErrNotAFont = errors.New("not a font dictionary")

// ErrFontNotFound reports a font resource name the resource dictionary does
// not define.
var ErrFontNotFound = errors.New("font resource not found")

// ErrBadProgram reports font program bytes no parser of
// pkg/internal/typography reads. A font whose embedded program is bad draws
// through a substitute or a placeholder; the sentinel only reaches the
// debug log.
var ErrBadProgram = errors.New("unreadable font program")
