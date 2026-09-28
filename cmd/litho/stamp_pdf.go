package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runStampPDF(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stamp-pdf", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	stampPath := fs.String("stamp", "", "Path to stamp PDF file (required)")
	stampPage := fs.Int("stamp-page", 1, "1-based page number in the stamp PDF")
	x := fs.Float64("x", 0, "X position in points from bottom-left")
	y := fs.Float64("y", 0, "Y position in points from bottom-left")
	pages := fs.String("pages", "", "Page range (default: all)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || *stampPath == "" {
		fmt.Fprintln(stderr, "Usage: litho stamp-pdf <file> --stamp overlay.pdf [--stamp-page 1] [--x 0] [--y 0] [--pages RANGE] -o out.pdf")
		return exitError
	}

	// Opened with the same -p as the target, as insert and merge open their
	// own second documents. The stamp used to be opened with no password at
	// all, which made an encrypted overlay unusable: there is no
	// --stamp-password, so `stamp-pdf -p pw -stamp enc.pdf` answered "invalid
	// PDF password" for a caller who had supplied the right one. A password is
	// a no-op on an unencrypted file, so the ordinary plaintext stamp is
	// unaffected.
	stampReader, stampCloser, err := openPDF(ctx, *stampPath, *password)
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

	// Both inputs, because either can now carry protection the output will not:
	// the stamp is opened with the same -p as the target. Variadic for exactly
	// this reason, as insert passes its target and source. This command has no
	// encryption flag, so it is never encrypting.
	warnEncryptionDropped(stderr, false, r, stampReader)

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
	defer releaseWriter(w, stderr)

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
