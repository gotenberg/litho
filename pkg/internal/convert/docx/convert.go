package docx

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Convert reads the DOCX package src holds and lays it out as a paged
// document, with the fonts of the index fonts. It returns ctx's error when
// ctx ends first, and the errors office.Options documents. The output
// depends on src, fonts' index and opts alone.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*compose.Document, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := office.ReadAll(ctx, src, opts.MaxInputBytes); err != nil {
		return nil, fmt.Errorf("reading DOCX: %w", err)
	}
	return nil, fmt.Errorf("converting DOCX: %w", office.ErrNotImplemented)
}
