package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gotenberg/litho/pkg/pdf"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point for the litho CLI. Every exit path
// returns a numeric status code rather than calling [os.Exit], and
// every output goes to the stdout/stderr writers passed by the caller
// instead of the process-wide [os.Stdout] / [os.Stderr]. The
// corresponding `main_test.go` file captures both writers and asserts
// the output.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "Usage: litho <file.pdf>")
		return 1
	}

	reader, err := pdf.OpenFile(ctx, args[0])
	if err != nil {
		fmt.Fprintf(stderr, "Failed to open PDF: %v\n", err)
		return 1
	}
	defer func() {
		_ = reader.Close() //nolint:errcheck
	}()

	metadata, err := reader.GetMetadata(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Warning: failed to extract metadata: %v\n", err)
	}
	fmt.Fprintln(stdout, "=== Metadata ===")
	for k, v := range metadata {
		fmt.Fprintf(stdout, "%s: %s\n", k, v)
	}

	numPages, err := reader.NumPages(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Warning: failed to get page count: %v\n", err)
	}
	fmt.Fprintf(stdout, "\n=== Pages: %d ===\n", numPages)

	if numPages > 0 {
		text, err := reader.ExtractText(ctx, 1)
		if err != nil {
			fmt.Fprintf(stderr, "Warning: failed to extract text from page 1: %v\n", err)
		}
		fmt.Fprintln(stdout, "\n=== Page 1 Text ===")
		fmt.Fprintln(stdout, text)
	}

	formData, err := reader.GetFormData(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Warning: failed to extract form data: %v\n", err)
	}
	if len(formData) > 0 {
		fmt.Fprintln(stdout, "\n=== Form Data ===")
		for k, v := range formData {
			fmt.Fprintf(stdout, "%s: %s\n", k, v)
		}
	}

	return 0
}
