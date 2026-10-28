// Command render draws the first page of a PDF to a PNG image at 150 dpi.
//
//	go run ./examples/render document.pdf page.png
package main

import (
	"context"
	"errors"
	"image/png"
	"log"
	"os"

	"github.com/gotenberg/litho/pkg/pdf"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: render document.pdf page.png")
	}

	r, err := pdf.OpenFile(ctx, args[0])
	if err != nil {
		return err
	}
	defer r.Close()

	img, err := r.RenderPage(ctx, 1, pdf.WithRenderDPI(150))
	if err != nil {
		return err
	}

	out, err := os.Create(args[1]) //nolint:gosec // The output path is the example's own argument.
	if err != nil {
		return err
	}
	err = png.Encode(out, img)
	return errors.Join(err, out.Close())
}
