// Command compose merges PDFs, then rotates the first page of the result and
// stamps every page with a DRAFT watermark, in one pipeline.
//
//	go run ./examples/compose out.pdf a.pdf b.pdf
package main

import (
	"context"
	"errors"
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
	if len(args) < 3 {
		return errors.New("usage: compose out.pdf a.pdf b.pdf [more.pdf]")
	}

	sources := make([]*pdf.Reader, 0, len(args)-1)
	for _, path := range args[1:] {
		r, err := pdf.OpenFile(ctx, path)
		if err != nil {
			return err
		}
		defer r.Close()
		sources = append(sources, r)
	}

	p := pdf.NewPipeline().
		Open(sources[0]).
		Merge(sources[1:]).
		Apply(func(_ context.Context, w *pdf.Writer) error {
			if err := w.RotatePages(90, 1); err != nil {
				return err
			}
			// No page numbers: every page.
			return w.StampText(pdf.TextStamp{
				Text: "DRAFT", X: 200, Y: 400, FontSize: 72,
				Opacity: 0.3, Rotation: 45, Color: [3]float64{1, 0, 0},
			})
		})

	out, err := os.Create(args[0]) //nolint:gosec // The output path is the example's own argument.
	if err != nil {
		return err
	}
	_, err = p.WriteTo(ctx, out)
	return errors.Join(err, out.Close())
}
