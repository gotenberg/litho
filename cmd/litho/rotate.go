package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runRotate(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rotate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	angle := fs.Int("angle", 0, "Rotation to set, in degrees (multiple of 90, required). Replaces any rotation the page already has")
	pages := fs.String("pages", "", "Page range (default: all)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	// -angle 0 is a real request: it clears /Rotate on an already-rotated
	// document, and 0 is one of the four values RotatePages accepts. Testing
	// the value cannot tell that apart from an absent flag, so ask the flag set
	// which flags were actually seen.
	angleSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "angle" {
			angleSet = true
		}
	})

	if fs.NArg() < 1 || *output == "" || !angleSet {
		fmt.Fprintln(stderr, "Usage: litho rotate <file> --angle ANGLE [--pages RANGE] -o out.pdf")
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	// This command has no encryption flag, so it can never be encrypting.
	warnEncryptionDropped(stderr, r)

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
	defer releaseWriter(w, stderr)

	var pageList []int
	if *pages != "" {
		pageCount, err := r.PageCount(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "reading page count: %v\n", err)
			return exitError
		}
		pageList, err = pdf.ParsePageRanges(*pages, pageCount)
		if err != nil {
			fmt.Fprintf(stderr, "parsing pages: %v\n", err)
			return exitError
		}
	}

	if err := w.RotatePages(*angle, pageList...); err != nil {
		fmt.Fprintf(stderr, "rotating pages: %v\n", err)
		return exitError
	}

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}
