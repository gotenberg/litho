// Command convert lays a plain text file out as a tagged PDF, in the fonts of
// the operating system.
//
//	go run ./examples/convert out.pdf input.txt
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
	if len(args) != 2 {
		return errors.New("usage: convert out.pdf input.txt")
	}

	in, err := os.Open(args[1]) //nolint:gosec // The input path is the example's own argument.
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(args[0]) //nolint:gosec // The output path is the example's own argument.
	if err != nil {
		return err
	}
	report, err := pdf.Convert(ctx, in, pdf.FormatText, out, pdf.WithPaperSize(612, 792), pdf.WithFontSize(11))
	if err = errors.Join(err, out.Close()); err != nil {
		return err
	}

	fmt.Printf("%d page(s)\n", report.Pages)
	for _, w := range report.Warnings {
		fmt.Printf("page %d: %s: %s\n", w.Page, w.Code, w.Message)
	}
	return nil
}
