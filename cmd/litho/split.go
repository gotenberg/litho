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
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	pages := fs.String("pages", "", "Page range (required)")
	userPwd := fs.String("user-password", "", "Encrypt the output with this user password")
	ownerPwd := fs.String("owner-password", "", "Owner password for the encrypted output")
	algorithm := fs.String("algorithm", "aes256", "Algorithm for the encrypted output: rc4-128, aes128, aes256")
	compact := registerCompactFlag(fs)

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || *pages == "" {
		fmt.Fprintln(stderr, "Usage: litho split <file> --pages RANGE [--user-password X] [--compact] -o out.pdf")
		return exitError
	}

	// Resolved before the output file is created, so a bad algorithm fails
	// without leaving a partial file behind.
	writeOpts, ok := encryptWriteOptions(*userPwd, *ownerPwd, *algorithm, stderr)
	if !ok {
		return exitError
	}
	// Captured BEFORE the compaction options join the slice. Both warnings
	// below ask whether the OUTPUT is encrypted, and once writeOpts also
	// carries compaction options its length stops answering that: --compact
	// alone would make it non-empty, which suppresses the encryption warning
	// and fires the compaction one on a file that was compacted fine.
	encrypting := len(writeOpts) > 0
	writeOpts = append(writeOpts, compactWriteOptions(*compact)...)
	writeOpts = append(writeOpts, defaultPageSize.writeOptions()...)

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	warnEncryptionDroppedOfferingPassword(stderr, encrypting, r)
	warnXFADropped(ctx, stderr, r)
	warnCompactSkipped(stderr, *compact, encrypting)

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

	var dropped []pdf.DroppedSignature
	writeOpts = append(writeOpts, pdf.WithDroppedSignatures(&dropped))
	if err := pdf.Split(ctx, r, pageList, dst, writeOpts...); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "splitting: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	warnSignaturesDropped(stderr, dropped)
	return exitOK
}
