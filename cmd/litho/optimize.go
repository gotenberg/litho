package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

// optimizeUsage is the usage line of the optimize command.
const optimizeUsage = "Usage: litho optimize <file> [-p PASSWORD] [--no-compact] [--no-recompress-images] [--no-subset-fonts] [--allow-signature-loss] -o out.pdf"

// runOptimize implements `litho optimize`: a full rewrite of the input with
// every compaction pass on, object-stream packing with stream dedup, lossless
// image recompression and font subsetting, each of which a --no- flag turns
// off (see [registerOptimizeFlags]).
//
// The other commands offer the same passes as opt-in flags on a rewrite they
// make for their own edit. optimize makes the rewrite for the passes alone,
// with [pdf.WithFullRewrite], so it never falls back to an incremental update
// that would carry none of them.
//
// A signed input is refused unless --allow-signature-loss (see
// [refuseSignatureLoss]). An input that declares PDF/A-1 keeps dedup and is
// not packed (see [declaresPDFA1]), so optimizing it does not cost it the
// conformance it claims. An encrypted input is written decrypted, with the
// warning every other command prints, since no compaction applies to an
// encrypted output.
func runOptimize(ctx context.Context, args []string, _ io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("optimize", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	optimizing := registerOptimizeFlags(fs)

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() < 1 || *output == "" {
		fmt.Fprintln(stderr, optimizeUsage)
		return exitError
	}
	c, err := optimizing.get()
	if err != nil {
		fmt.Fprintf(stderr, "optimize: %v\n", err)
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	if !*optimizing.allowSignatureLoss {
		if err := refuseSignatureLoss(ctx, r); err != nil {
			fmt.Fprintf(stderr, "optimize: %v\n", err)
			return exitError
		}
	}
	warnEncryptionDropped(stderr, r)

	writeOpts := compactWriteOptions(c)
	if c.compact && declaresPDFA1(ctx, r, stderr) {
		writeOpts = unpackedCompactWriteOptions(c)
		warnPackingWithheldForDeclaredPDFA1(stderr)
	}
	writeOpts = append(writeOpts, pdf.WithFullRewrite())

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
	defer releaseWriter(w, stderr)

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	if err := w.WriteTo(ctx, dst, writeOpts...); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}
	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}
	return exitOK
}
