package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

// timestampFlags holds the flags the timestamp command and the pipe's
// timestamp stage share.
type timestampFlags struct {
	tsaURL, field *string
}

// registerTimestampFlags declares the document timestamp flags on fs.
func registerTimestampFlags(fs *flag.FlagSet) timestampFlags {
	return timestampFlags{
		tsaURL: fs.String("tsa-url", "", "RFC 3161 timestamp authority URL (required)"),
		field:  fs.String("field", "", "Signature field the timestamp goes in: an unsigned one of that name, else a new field takes the name (default: Signature1, ...)"),
	}
}

// options returns the URL and the [pdf.TimestampOption] values the flags set.
func (f timestampFlags) options() (string, []pdf.TimestampOption, error) {
	if *f.tsaURL == "" {
		return "", nil, errors.New("--tsa-url is required")
	}
	var opts []pdf.TimestampOption
	if *f.field != "" {
		opts = append(opts, pdf.WithTimestampField(*f.field))
	}
	return *f.tsaURL, opts, nil
}

// encryptedTimestampRefusal explains why an encrypted document takes no
// document timestamp, for the reason [encryptedSignRefusal] gives.
const encryptedTimestampRefusal = "litho cannot timestamp an encrypted document: its timestamp dictionary would have to be encrypted under the document's key (ISO 32000-1, 7.6.1). Decrypt it first (litho decrypt), then timestamp the output"

func runTimestamp(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("timestamp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	flags := registerTimestampFlags(fs)

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() < 1 || *output == "" || *flags.tsaURL == "" {
		fmt.Fprintln(stderr, "Usage: litho timestamp <file> --tsa-url URL [--field F] -o out.pdf")
		return exitError
	}
	tsaURL, opts, err := flags.options()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()
	if r.IsEncrypted() {
		fmt.Fprintln(stderr, encryptedTimestampRefusal)
		return exitError
	}

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
	defer releaseWriter(w, stderr)
	w.Timestamp(tsaURL, opts...)

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	if err := w.WriteTo(ctx, dst); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "timestamping: %v\n", err)
		return exitError
	}
	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}
	return exitOK
}
