package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runStampPDF(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stamp-pdf", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	stampPath := fs.String("stamp", "", "Path to stamp PDF file (required)")
	stampPage := fs.Int("stamp-page", 1, "1-based page number in the stamp PDF")
	x := fs.Float64("x", 0, "X position in points from bottom-left")
	y := fs.Float64("y", 0, "Y position in points from bottom-left")
	pages := fs.String("pages", "", "Page range (default: all)")

	if err := fs.Parse(args); err != nil {
		return exitError
	}

	if fs.NArg() < 1 || *output == "" || *stampPath == "" {
		fmt.Fprintln(stderr, "Usage: litho stamp-pdf <file> --stamp overlay.pdf [--stamp-page 1] [--x 0] [--y 0] [--pages RANGE] -o out.pdf")
		return exitError
	}

	stampReader, stampCloser, err := openPDF(ctx, *stampPath, "")
	if err != nil {
		fmt.Fprintf(stderr, "opening stamp PDF: %v\n", err)
		return exitError
	}
	defer stampCloser()

	stamp := pdf.PDFStamp{
		Source: stampReader,
		Page:   *stampPage,
		X:      *x,
		Y:      *y,
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	w := pdf.NewWriter(r)

	var pageList []int
	if *pages != "" {
		pageCount, err := r.PageCount(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "reading page count: %v\n", err)
			return exitError
		}
		pageList, err = parsePages(*pages, pageCount)
		if err != nil {
			fmt.Fprintf(stderr, "parsing pages: %v\n", err)
			return exitError
		}
	}

	if err := w.StampPDF(stamp, pageList...); err != nil {
		fmt.Fprintf(stderr, "stamping PDF: %v\n", err)
		return exitError
	}

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst); err != nil {
		closeDst() //nolint:errcheck
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}
