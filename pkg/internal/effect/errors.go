package effect

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Sentinel errors of the effect engine.
var (
	// ErrTooLarge is returned, wrapped in an error naming the size, for a
	// surface whose side is not positive or whose pixels pass [MaxPixels].
	// It wraps [limits.ErrInputTooLarge]: a document asks for the size.
	ErrTooLarge = fmt.Errorf("%w: effect surface past its pixel cap", limits.ErrInputTooLarge)

	// ErrFormat is returned, wrapped in an error naming the operation, when
	// the surfaces an operation combines differ in format or size, or when
	// an operation receives a format it does not take. It is a mistake of
	// the caller's code, not of a document.
	ErrFormat = errors.New("effect surface format or size mismatch")

	// ErrGrid is returned, wrapped in an error naming the grid, for a
	// [Grid] whose scale is not positive and finite or whose origin is not
	// finite. It is a mistake of the caller's code, not of a document.
	ErrGrid = errors.New("effect grid not positive and finite")
)
