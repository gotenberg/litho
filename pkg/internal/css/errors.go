package css

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// ErrTooLarge reports a stylesheet past a size cap: its bytes, tokens,
// nesting depth or rule count.
var ErrTooLarge = errors.New("css: stylesheet too large")

// ErrTooComplex reports a document and stylesheets whose cascade asks for
// more work than one Compute call allows. It wraps limits.ErrInputTooLarge.
var ErrTooComplex = fmt.Errorf("%w: cascade too complex", limits.ErrInputTooLarge)
