package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runDelete(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	pages := fs.String("pages", "", "Page range (required)")
	compactFlags := registerCompactFlags(fs)

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || *pages == "" {
		fmt.Fprintln(stderr, "Usage: litho delete <file> --pages RANGE [--compact] [--recompress-images] [--subset-fonts] -o out.pdf")
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

	pageCount, err := r.PageCount(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "reading page count: %v\n", err)
		return exitError
	}

	pageList, err := parsePages(*pages, pageCount)
	if err != nil {
		fmt.Fprintf(stderr, "parsing pages: %v\n", err)
		return exitError
	}

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
	defer releaseWriter(w, stderr)

	if err := w.DeletePages(pageList...); err != nil {
		fmt.Fprintf(stderr, "deleting pages: %v\n", err)
		return exitError
	}

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst, compactWriteOptions(compactFlags.get())...); err != nil {
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
