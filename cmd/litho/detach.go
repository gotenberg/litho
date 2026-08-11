package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runDetach(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("detach", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	name := fs.String("name", "", "Attachment name to remove (required)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	if fs.NArg() < 1 || *output == "" || *name == "" {
		fmt.Fprintln(stderr, "Usage: litho detach <file> --name \"data.csv\" -o out.pdf")
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	w := pdf.NewWriter(r)
	w.RemoveAttachment(*name)

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst); err != nil {
		closeDst() //nolint:errcheck // already returning an error; a close failure would mask its cause
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}
