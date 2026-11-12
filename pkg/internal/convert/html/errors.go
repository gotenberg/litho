package html

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Sentinel errors [Convert] wraps beside limits.ErrInputTooLarge,
// limits.ErrTooManyPages and typeset.ErrInvalidOptions, which every
// converter shares. Match them with errors.Is.
var (
	// ErrTooManyElements is returned for a document of more than
	// [MaxElements] elements. It wraps limits.ErrInputTooLarge. Nothing is
	// laid out.
	ErrTooManyElements = fmt.Errorf("%w: too many elements", limits.ErrInputTooLarge)
	// ErrTooDeep is returned for a document that nests elements deeper
	// than the HTML parser follows, 512. It wraps limits.ErrInputTooLarge.
	// Nothing is laid out.
	ErrTooDeep = fmt.Errorf("%w: elements nested too deeply", limits.ErrInputTooLarge)
)
