package shade

import "errors"

// Sentinel errors for shading compilation and painting.
var (
	// ErrMalformed indicates a shading whose required entries are missing,
	// of the wrong type, or mutually inconsistent (ISO 32000-1, 8.7.4.5).
	ErrMalformed = errors.New("shade: malformed shading")

	// ErrUnsupportedType indicates a /ShadingType outside 1 to 7, or a
	// /PatternType other than 2.
	ErrUnsupportedType = errors.New("shade: unsupported shading type")

	// ErrTooLarge indicates a mesh whose values or patches exceed the
	// anti-DoS bounds of [New].
	ErrTooLarge = errors.New("shade: shading too large")

	// ErrTarget indicates a paint target the caller built inconsistently: no
	// canvas, no converter, or a mask whose size differs from the canvas.
	ErrTarget = errors.New("shade: invalid paint target")
)
