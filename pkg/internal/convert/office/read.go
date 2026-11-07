package office

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// DefaultMaxInputBytes is the source cap when Options.MaxInputBytes is
// zero.
const DefaultMaxInputBytes = limits.DefaultMaxConvertInputBytes

// ReadAll reads src to its end, at most maxBytes bytes
// ([DefaultMaxInputBytes] when maxBytes is zero), and fails with
// [ErrInputTooLarge] past that. A package is a zip archive, whose central
// directory sits at its end, so a converter reads the whole source before
// it opens it. It returns ctx's error when ctx ends first.
func ReadAll(ctx context.Context, src io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxInputBytes
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(src, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the source: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: source exceeds %d bytes", ErrInputTooLarge, maxBytes)
	}
	return data, nil
}
