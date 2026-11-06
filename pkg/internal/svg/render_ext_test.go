package svg_test

import (
	"bytes"
	"context"
	"fmt"
	"image"

	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/pdf"
)

// The draw tests rasterise through pkg/pdf, which imports the svg package
// through the HTML converter: only an external test package may import it.
func init() {
	svg.RenderPDF = func(data []byte) (image.Image, error) {
		r, err := pdf.Open(context.Background(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("opening: %w", err)
		}
		defer r.Close()
		img, err := r.RenderPage(context.Background(), 1, pdf.WithRenderDPI(96))
		if err != nil {
			return nil, fmt.Errorf("rendering: %w", err)
		}
		return img, nil
	}
}
