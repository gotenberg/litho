package function

import "errors"

// Sentinel errors for PDF function evaluation.
var (
	// ErrUnsupportedType indicates a /FunctionType that ISO 32000-1, 7.10
	// does not define. Types 0, 2, 3 and 4 are all evaluated.
	ErrUnsupportedType = errors.New("function: unsupported /FunctionType")

	// ErrMalformed indicates a function object whose required entries are
	// missing, of the wrong type, or mutually inconsistent.
	ErrMalformed = errors.New("function: malformed function object")

	// ErrInputCount indicates an evaluation whose input count does not match
	// the function's /Domain.
	ErrInputCount = errors.New("function: wrong number of inputs")

	// ErrStitchTooLarge indicates a type 3 function whose subfunctions nest
	// deeper, or number more, than the anti-DoS bounds of [New] allow.
	ErrStitchTooLarge = errors.New("function: stitching function too large")

	// ErrStitchCycle indicates a type 3 function that names itself, directly
	// or through its subfunctions, so it describes no finite function.
	ErrStitchCycle = errors.New("function: stitching function names itself")
)
