package layout

import (
	"context"
	"errors"

	"github.com/gotenberg/litho/pkg/internal/text"
)

// ErrTooManyPages reports a layout that needs more pages than
// Options.MaxPages allows.
var ErrTooManyPages = errors.New("layout needs more pages than allowed")

// ErrNotImplemented is a placeholder until the engine lands.
var ErrNotImplemented = errors.New("layout: not implemented")

func layout(context.Context, *Node, *text.Fonts, Options) (*Result, error) {
	return nil, ErrNotImplemented
}
