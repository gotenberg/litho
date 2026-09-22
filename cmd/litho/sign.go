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
	// Registered so Parse ACCEPTS them, and discarded because this command
	// always refuses: there is nothing to read the values into. Dropping the
	// declarations would make `sign -o out.pdf in.pdf` fail with "flag provided
	// but not defined" and replace the explanation below with a parse error,
	// which is what TestSign_RefusesWithAnExplanation pins by passing -o.
	//
	// This is not the `_ = variable` CLAUDE.md forbids: that rule is about
	// discarding a value you should have used.
	_ = fs.String("o", "", "Output file")
	_ = fs.String("cert", "", "Certificate PEM file")
	_ = fs.String("key", "", "Private key PEM file")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	fmt.Fprintln(stderr, "signing requires a custom Signer implementation")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "The litho library's Sign method accepts a pdf.Signer interface that")
	fmt.Fprintln(stderr, "the CLI cannot provide without additional crypto dependencies.")
	fmt.Fprintln(stderr, "Use the Go API directly to implement signing with your own Signer.")

	return exitError
}
