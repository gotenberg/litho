package fontsubset

import "errors"

// ErrSubsetDiverged is returned, wrapped, when a program [Run] subsets lazily
// no longer subsets to the bytes it measured, which means its source changed
// between the pass and the write.
var ErrSubsetDiverged = errors.New("a subset font program no longer subsets as measured")

// Reasons a Type 1 program does not read for the pass, which then leaves it
// whole.
var (
	errNoCharStrings      = errors.New("no /CharStrings entry")
	errTooManyGlyphs      = errors.New("more glyphs than the pass gives IDs")
	errPredefinedEncoding = errors.New("a built-in encoding naming a predefined encoding other than the Standard encoding")
)
