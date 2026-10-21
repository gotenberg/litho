package flatten

import "errors"

var (
	// ErrTooComplex reports a page flattening cannot handle within its
	// budgets: regions that do not settle within the passes [Analyze]
	// allows them, or transparency the render pixel budget cannot
	// composite.
	ErrTooComplex = errors.New("flatten: page too complex to flatten")
	// ErrInvalidOptions reports [Options] flattening cannot run with.
	ErrInvalidOptions = errors.New("flatten: invalid options")
)

// errNotForm reports a Do replaced by its text that names no form XObject.
var errNotForm = errors.New("flatten: not a form XObject")
