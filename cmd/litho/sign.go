package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

func runSign(_ context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	fs.SetOutput(stderr)
	_ = fs.String("o", "", "Output file")
	_ = fs.String("cert", "", "Certificate PEM file")
	_ = fs.String("key", "", "Private key PEM file")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	fmt.Fprintln(stderr, "signing requires a custom Signer implementation")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "The litho library's Sign method accepts a pdf.Signer interface that")
	fmt.Fprintln(stderr, "the CLI cannot provide without additional crypto dependencies.")
	fmt.Fprintln(stderr, "Use the Go API directly to implement signing with your own Signer.")

	return exitError
}
