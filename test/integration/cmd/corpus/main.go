package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	// Multi-call dispatch: when argv[0]'s basename matches a
	// subcommand, treat the program as that subcommand directly. The
	// provisioning Docker entrypoint cross-compiles this binary and
	// bind-mounts it at /work/bin/scan-claims, then invokes it as
	// `scan-claims --pdfs ...`. Without this dispatch the flag parser
	// would treat `--pdfs` as an unknown subcommand.
	if cmd := subcommandFromArgv0(); cmd != "" {
		os.Exit(dispatch(cmd, os.Args[1:]))
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	os.Exit(dispatch(os.Args[1], os.Args[2:]))
}

func subcommandFromArgv0() string {
	if filepath.Base(os.Args[0]) == "scan-claims" {
		return "scan-claims"
	}
	return ""
}

func dispatch(cmd string, args []string) int {
	switch cmd {
	case "init":
		return runInit(args)
	case "run":
		return runRun(args)
	case "scan-claims":
		return runScanClaims(args)
	case "-h", "--help", "help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", cmd)
		printUsage()
		return 2
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
