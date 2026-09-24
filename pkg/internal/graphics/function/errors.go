package function

import "errors"

// Sentinel errors for PDF function evaluation.
//
// # Corpus evidence
//
//   - All corpora: no fixture uses a type 3 (stitching) function, which is
//     why [ErrUnsupportedType] refuses it rather than shipping it untested.
var (
	// ErrUnsupportedType indicates a /FunctionType this package does not
	// evaluate. Type 3 (stitching) is the only one defined by ISO 32000-1,
	// 7.10 that is absent: nothing exercises it, so it is refused rather than
	// shipped untested.
	ErrUnsupportedType = errors.New("function: unsupported /FunctionType")

	// ErrMalformed indicates a function object whose required entries are
	// missing, of the wrong type, or mutually inconsistent.
	ErrMalformed = errors.New("function: malformed function object")

	// ErrInputCount indicates an evaluation whose input count does not match
	// the function's /Domain.
	ErrInputCount = errors.New("function: wrong number of inputs")
)
