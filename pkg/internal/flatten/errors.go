package flatten

import "errors"

// ErrTooComplex reports a page whose regions do not settle within the
// passes [Analyze] allows them.
var ErrTooComplex = errors.New("flatten: page too complex to flatten")
