package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "init":
		os.Exit(runInit(os.Args[2:]))
	case "run":
		os.Exit(runRun(os.Args[2:]))
	case "scan-claims":
		os.Exit(runScanClaims(os.Args[2:]))
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: corpus <init|run> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Subcommands:")
	fmt.Fprintln(os.Stderr, "  init          Provision corpora via Docker (downloads PDFs and goldens)")
	fmt.Fprintln(os.Stderr, "  run           Compare litho against golden references across the matrix")
	fmt.Fprintln(os.Stderr, "  scan-claims   Emit a claims.csv mapping each PDF to its claimed profile")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Common run flags:")
	fmt.Fprintln(os.Stderr, "  --corpus=pdfjs,verapdf        Restrict to named corpora")
	fmt.Fprintln(os.Stderr, "  --feature=text,compliance     Restrict to named features")
	fmt.Fprintln(os.Stderr, "  --profile=PDF_A-2b            Restrict to fixtures under one profile")
	fmt.Fprintln(os.Stderr, "  --format=text|json            Output format")
	fmt.Fprintln(os.Stderr, "  --workers=N                   Parallel workers (default: GOMAXPROCS)")
}
