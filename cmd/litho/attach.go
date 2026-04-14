package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runAttach(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	name := fs.String("name", "", "Attachment name as stored in the PDF (required)")
	dataPath := fs.String("data", "", "Path to the file to attach (required)")
	mime := fs.String("mime", "", "MIME type (auto-detected from extension if omitted)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	if fs.NArg() < 1 || *output == "" || *name == "" || *dataPath == "" {
		fmt.Fprintln(stderr, "Usage: litho attach <file> --name \"data.csv\" --data data.csv [--mime text/csv] -o out.pdf")
		return exitError
	}

	data, err := os.ReadFile(*dataPath)
	if err != nil {
		fmt.Fprintf(stderr, "reading attachment file: %v\n", err)
		return exitError
	}

	mimeType := *mime
	if mimeType == "" {
		mimeType = mimeFromExt(*dataPath)
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	w := pdf.NewWriter(r)
	w.AddAttachment(*name, data, mimeType)

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
