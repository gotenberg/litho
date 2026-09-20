package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runMerge(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	userPwd := fs.String("user-password", "", "Encrypt the output with this user password")
	ownerPwd := fs.String("owner-password", "", "Owner password for the encrypted output")
	algorithm := fs.String("algorithm", "aes256", "Algorithm for the encrypted output: rc4-128, aes128, aes256")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) < 2 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho merge <file1> <file2> [file...] [--user-password X] -o out.pdf")
		return exitError
	}

	// Resolved before the output file is created, so a bad algorithm fails
	// without leaving a partial file behind.
	writeOpts, ok := encryptWriteOptions(*userPwd, *ownerPwd, *algorithm, stderr)
	if !ok {
		return exitError
	}

	var readers []*pdf.Reader
	var closers []func()

	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			for _, c := range closers {
				c()
			}
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}
		readers = append(readers, r)
		closers = append(closers, closer)
	}

	defer func() {
		for _, c := range closers {
			c()
		}
	}()

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := pdf.Merge(ctx, readers, dst, writeOpts...); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "merging: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}
