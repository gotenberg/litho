package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runSetMetadata(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("set-metadata", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	title := fs.String("title", "", "Document title")
	author := fs.String("author", "", "Document author")
	subject := fs.String("subject", "", "Document subject")
	keywords := fs.String("keywords", "", "Document keywords")
	creator := fs.String("creator", "", "Creator application")
	producer := fs.String("producer", "", "Producer application")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho set-metadata <file> --title X --author Y ... -o out.pdf")
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	// This command has no encryption flag, so it can never be encrypting.
	warnEncryptionDropped(stderr, false, r)

	w := pdf.NewWriter(r)
	defer releaseWriter(w, stderr)
	w.SetMetadata(pdf.Metadata{
		Title:    *title,
		Author:   *author,
		Subject:  *subject,
		Keywords: *keywords,
		Creator:  *creator,
		Producer: *producer,
	})

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
