// Command convert lays a plain text or HTML file out as a tagged PDF, in the
// fonts of the operating system. An HTML file reads its stylesheets and
// images from its own directory.
//
//	go run ./examples/convert out.pdf input.txt
//	go run ./examples/convert out.pdf page.html
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: convert out.pdf input.txt|input.html")
	}

	format := pdf.FormatText
	opts := []pdf.ConvertOption{pdf.WithPaperSize(612, 792)}
	switch strings.ToLower(filepath.Ext(args[1])) {
	case ".html", ".htm":
		root, err := os.OpenRoot(filepath.Dir(args[1]))
		if err != nil {
			return err
		}
		defer root.Close()
		format = pdf.FormatHTML
		opts = append(opts, pdf.WithResources(root.FS()), pdf.WithMediaType("print"))
	default:
		opts = append(opts, pdf.WithFontSize(11))
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
	report, err := pdf.Convert(ctx, in, format, out, opts...)
	if err = errors.Join(err, out.Close()); err != nil {
		return err
	}

	fmt.Printf("%d page(s)\n", report.Pages)
	for _, w := range report.Warnings {
		fmt.Printf("page %d: %s: %s\n", w.Page, w.Code, w.Message)
	}
	return nil
}
