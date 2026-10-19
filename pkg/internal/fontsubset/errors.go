package fontsubset

import "errors"

// ErrSubsetDiverged is returned, wrapped, when a program [Run] subsets lazily
// no longer subsets to the bytes it measured, which means its source changed
// between the pass and the write.
var ErrSubsetDiverged = errors.New("a subset font program no longer subsets as measured")
