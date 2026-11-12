package layout

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// ErrInvalidOptions reports a nil root or font index, or a page box whose
// size or margins are not finite, or leave no positive page area.
var ErrInvalidOptions = errors.New("invalid layout options")

// ErrTooManyBoxes reports a content tree that generates more boxes than
// [MaxBoxes]. It wraps limits.ErrInputTooLarge.
var ErrTooManyBoxes = fmt.Errorf("%w: too many boxes", limits.ErrInputTooLarge)
