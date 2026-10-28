// Command extract prints the text of every page of a PDF and lists the
// images each page draws.
//
//	go run ./examples/extract document.pdf
package main

import (
	"context"
	"errors"
	"fmt"
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
	if len(args) != 1 {
		return errors.New("usage: extract document.pdf")
	}

	r, err := pdf.OpenFile(ctx, args[0])
	if err != nil {
		return err
	}
	defer r.Close()

	n, err := r.PageCount(ctx)
	if err != nil {
		return err
	}
	for page := 1; page <= n; page++ {
		text, err := r.ExtractText(ctx, page)
		if err != nil {
			return err
		}
		fmt.Printf("--- page %d ---\n%s\n", page, text)

		images, err := r.ExtractImages(ctx, page)
		if err != nil {
			return err
		}
		for _, img := range images {
			fmt.Printf("image %s: %dx%d %s\n", img.Filename, img.Width, img.Height, img.ColorSpace)
		}
	}
	return nil
}
