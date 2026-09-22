package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runSplit(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("split", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	pages := fs.String("pages", "", "Page range (required)")
	userPwd := fs.String("user-password", "", "Encrypt the output with this user password")
	ownerPwd := fs.String("owner-password", "", "Owner password for the encrypted output")
	algorithm := fs.String("algorithm", "aes256", "Algorithm for the encrypted output: rc4-128, aes128, aes256")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || *pages == "" {
		fmt.Fprintln(stderr, "Usage: litho split <file> --pages RANGE [--user-password X] -o out.pdf")
		return exitError
	}

	// Resolved before the output file is created, so a bad algorithm fails
	// without leaving a partial file behind.
	writeOpts, ok := encryptWriteOptions(*userPwd, *ownerPwd, *algorithm, stderr)
	if !ok {
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	warnEncryptionDropped(stderr, len(writeOpts) > 0, r)

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

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := pdf.Split(ctx, r, pageList, dst, writeOpts...); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "splitting: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}
