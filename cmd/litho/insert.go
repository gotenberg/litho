package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runInsert(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("insert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	source := fs.String("source", "", "Source PDF file (required)")
	pos := fs.Int("pos", -1, "0-based insertion position (required)")
	sourcePages := fs.String("source-pages", "", "Source page range (default: all)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	if fs.NArg() < 1 || *output == "" || *source == "" || *pos < 0 {
		fmt.Fprintln(stderr, "Usage: litho insert <target> --source <file> --pos N [--source-pages RANGE] -o out.pdf")
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	srcReader, srcCloser, err := openPDF(ctx, *source, *password)
	if err != nil {
		fmt.Fprintf(stderr, "opening source: %v\n", err)
		return exitError
	}
	defer srcCloser()

	var srcPageList []int
	if *sourcePages != "" {
		srcPageCount, err := srcReader.PageCount(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "reading source page count: %v\n", err)
			return exitError
		}
		srcPageList, err = parsePages(*sourcePages, srcPageCount)
		if err != nil {
			fmt.Fprintf(stderr, "parsing source pages: %v\n", err)
			return exitError
		}
	}

	w := pdf.NewWriter(r)

	if err := w.InsertPages(*pos, srcReader, srcPageList...); err != nil {
		fmt.Fprintf(stderr, "inserting pages: %v\n", err)
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
