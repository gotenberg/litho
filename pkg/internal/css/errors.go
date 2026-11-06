package css

import "errors"

// ErrTooLarge reports a stylesheet past a size cap: its bytes, tokens,
// nesting depth or rule count.
var ErrTooLarge = errors.New("css: stylesheet too large")

// ErrTooComplex reports a document and stylesheets whose cascade asks for
// more work than one Compute call allows.
var ErrTooComplex = errors.New("css: cascade too complex")
