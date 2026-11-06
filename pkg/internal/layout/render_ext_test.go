package layout_test

import (
	"bytes"
	"context"
	"fmt"
	"image"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/pdf"
)

// The draw tests rasterise through pkg/pdf, which imports the layout
// package: only an external test package may import it.
func init() {
	layout.RenderPDF = func(data []byte, n int) (image.Image, error) {
		r, err := pdf.Open(context.Background(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("opening: %w", err)
		}
		defer r.Close()
		img, err := r.RenderPage(context.Background(), n, pdf.WithRenderDPI(96))
		if err != nil {
			return nil, fmt.Errorf("rendering: %w", err)
		}
		return img, nil
	}
}
