package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// fieldFlags collects repeated --field name=value pairs.
type fieldFlags []string

func (f *fieldFlags) String() string { return strings.Join(*f, ", ") }
func (f *fieldFlags) Set(val string) error {
	*f = append(*f, val)
	return nil
}

func runSetForm(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("set-form", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")

	var fields fieldFlags
	fs.Var(&fields, "field", "Field assignment name=value (repeatable)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	if fs.NArg() < 1 || *output == "" || len(fields) == 0 {
		fmt.Fprintln(stderr, "Usage: litho set-form <file> --field name=value [--field name2=value2] -o out.pdf")
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	w := pdf.NewWriter(r)

	for _, f := range fields {
		name, value, ok := strings.Cut(f, "=")
		if !ok {
			fmt.Fprintf(stderr, "invalid field format %q: expected name=value\n", f)
			return exitError
		}
		w.SetFormField(name, value)
	}

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
